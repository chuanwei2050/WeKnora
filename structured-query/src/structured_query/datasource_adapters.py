from dataclasses import dataclass
from contextlib import contextmanager
from functools import lru_cache
from typing import Any, Protocol

from sqlalchemy import Boolean, Date, DateTime, Enum, Float, Integer, MetaData, Numeric, String, Table, Time, Uuid, cast, create_engine, func, inspect, select, text
from sqlalchemy.exc import DBAPIError

from .config import Settings, get_settings
from .execution import ExecutionResult, QueryExecutionError, _plan_root, bounded_result, classify_database_error


@dataclass(frozen=True, order=True)
class TableRef:
    schema: str
    name: str


@dataclass(frozen=True)
class TableProfile:
    table: TableRef
    columns: tuple[dict[str, Any], ...]
    primary_key: tuple[str, ...]
    foreign_keys: tuple[dict[str, Any], ...]
    comment: str | None


class DataSourceAdapter(Protocol):
    dialect: str

    def inspect_tables(self) -> list[TableProfile]: ...

    def execute(self, sql: str) -> ExecutionResult: ...

    def profile_column(self, table: TableRef, column: str, persist_values: bool) -> dict[str, Any]: ...

    def supports_literal(self, table: TableRef, column: str, fragment: str) -> bool: ...


def resolve_datasource_secret(secret_ref: str, settings: Settings | None = None) -> str:
    configured = (settings or get_settings()).datasource_secrets.get(secret_ref)
    if configured is None:
        raise QueryExecutionError("datasource_unavailable", retryable=False)
    return configured.get_secret_value()


class _SQLAlchemyAdapter:
    dialect = ""

    def __init__(self, dsn: str, allowed_tables: set[TableRef], settings: Settings | None = None) -> None:
        if not allowed_tables:
            raise ValueError("allowed_tables_required")
        self.settings = settings or get_settings()
        self.allowed_tables = frozenset(allowed_tables)
        self.engine = _datasource_engine(self.dialect, dsn)

    def inspect_tables(self) -> list[TableProfile]:
        inspector = inspect(self.engine)
        profiles: list[TableProfile] = []
        for table in sorted(self.allowed_tables):
            if not inspector.has_table(table.name, schema=table.schema):
                raise QueryExecutionError("authorized_table_not_found", retryable=False)
            comment = inspector.get_table_comment(table.name, schema=table.schema).get("text")
            profiles.append(
                TableProfile(
                    table=table,
                    columns=tuple(inspector.get_columns(table.name, schema=table.schema)),
                    primary_key=tuple(
                        inspector.get_pk_constraint(table.name, schema=table.schema).get("constrained_columns", [])
                    ),
                    foreign_keys=tuple(inspector.get_foreign_keys(table.name, schema=table.schema)),
                    comment=comment,
                )
            )
        return profiles

    def _bounded_query(self, sql: str) -> str:
        return f"SELECT * FROM ({sql}) AS sq_bounded_result LIMIT {self.settings.max_result_rows}"

    @staticmethod
    def _rows(result: Any) -> ExecutionResult:
        return bounded_result(list(result.keys()), result.fetchall())

    def profile_column(self, table: TableRef, column: str, persist_values: bool) -> dict[str, Any]:
        if table not in self.allowed_tables:
            raise QueryExecutionError("unauthorized_table", retryable=False)
        reflected = Table(table.name, MetaData(), schema=table.schema, autoload_with=self.engine)
        if column not in reflected.c:
            raise QueryExecutionError("invalid_reference", retryable=False)
        field = reflected.c[column]
        value_statistics_allowed = persist_values and isinstance(
            field.type, (String, Boolean, Date, DateTime, Time, Integer, Float, Numeric, Enum, Uuid)
        )
        try:
            with self._profile_connection() as connection:
                expressions = [
                    func.count().label("row_count"),
                    func.count(field).label("non_null_count"),
                ]
                if value_statistics_allowed:
                    expressions.extend([
                        func.count(func.distinct(field)).label("distinct_count"),
                        func.min(field).label("minimum"),
                        func.max(field).label("maximum"),
                    ])
                summary = connection.execute(select(*expressions)).one()
                values: list[dict[str, Any]] = []
                distinct_count = (
                    int(summary.distinct_count or 0) if value_statistics_allowed else None
                )
                if value_statistics_allowed:
                    assert distinct_count is not None
                    limit = self.settings.low_cardinality_limit if distinct_count <= self.settings.low_cardinality_limit else self.settings.high_cardinality_sample
                    rows = connection.execute(
                        select(field, func.count().label("frequency"))
                        .where(field.is_not(None))
                        .group_by(field)
                        .order_by(func.count().desc())
                        .limit(limit)
                    )
                    values = [{"value": str(row[0]), "frequency": int(row[1])} for row in rows]
        except DBAPIError as error:
            raise classify_database_error(error) from error
        row_count = int(summary.row_count or 0)
        return {
            "row_count": row_count,
            "null_count": row_count - int(summary.non_null_count or 0),
            "distinct_count": distinct_count,
            "minimum": None if not value_statistics_allowed or summary.minimum is None else str(summary.minimum),
            "maximum": None if not value_statistics_allowed or summary.maximum is None else str(summary.maximum),
            "values": values,
            "values_persisted": value_statistics_allowed,
        }

    def supports_literal(self, table: TableRef, column: str, fragment: str) -> bool:
        """Probe one selected, authorized field without persisting or returning its value."""
        if table not in self.allowed_tables:
            raise QueryExecutionError("unauthorized_table", retryable=False)
        reflected = Table(table.name, MetaData(), schema=table.schema, autoload_with=self.engine)
        if column not in reflected.c:
            raise QueryExecutionError("invalid_reference", retryable=False)
        pattern = f"%{_escape_like(fragment)}%"
        statement = select(1).where(
            cast(reflected.c[column], String).ilike(pattern, escape="\\")
        ).limit(1)
        return self._probe_exists(statement)

    def _probe_exists(self, statement: Any) -> bool:
        raise NotImplementedError

    def _profile_connection(self):
        raise NotImplementedError


class PostgreSQLAdapter(_SQLAlchemyAdapter):
    dialect = "postgres"

    def execute(self, sql: str) -> ExecutionResult:
        try:
            with self.engine.connect() as connection:
                transaction = connection.begin()
                try:
                    connection.execute(text("SET TRANSACTION READ ONLY"))
                    connection.execute(text(f"SET LOCAL statement_timeout = {self.settings.statement_timeout_ms}"))
                    connection.execute(text(f"SET LOCAL lock_timeout = {self.settings.lock_timeout_ms}"))
                    plan = _plan_root(connection.execute(text(f"EXPLAIN (FORMAT JSON) {sql}")).scalar_one())
                    self._check_plan(plan)
                    return self._rows(connection.execute(text(self._bounded_query(sql))))
                finally:
                    transaction.rollback()
        except QueryExecutionError:
            raise
        except DBAPIError as error:
            raise classify_database_error(error) from error

    def _check_plan(self, plan: dict[str, Any]) -> None:
        if float(plan.get("Total Cost", 0)) > self.settings.max_plan_cost:
            raise QueryExecutionError("plan_cost_exceeded", retryable=False)
        if int(plan.get("Plan Rows", 0)) > self.settings.max_plan_rows:
            raise QueryExecutionError("plan_rows_exceeded", retryable=False)

    def _probe_exists(self, statement: Any) -> bool:
        try:
            with self.engine.connect() as connection:
                transaction = connection.begin()
                try:
                    connection.execute(text("SET TRANSACTION READ ONLY"))
                    connection.execute(text(f"SET LOCAL statement_timeout = {self.settings.statement_timeout_ms}"))
                    connection.execute(text(f"SET LOCAL lock_timeout = {self.settings.lock_timeout_ms}"))
                    return connection.execute(statement).first() is not None
                finally:
                    transaction.rollback()
        except DBAPIError as error:
            raise classify_database_error(error) from error

    @contextmanager
    def _profile_connection(self):
        with self.engine.connect() as connection:
            transaction = connection.begin()
            try:
                connection.execute(text("SET TRANSACTION READ ONLY"))
                connection.execute(text(f"SET LOCAL statement_timeout = {self.settings.statement_timeout_ms}"))
                connection.execute(text(f"SET LOCAL lock_timeout = {self.settings.lock_timeout_ms}"))
                yield connection
            finally:
                transaction.rollback()


class MySQLAdapter(_SQLAlchemyAdapter):
    dialect = "mysql"

    def execute(self, sql: str) -> ExecutionResult:
        try:
            with self.engine.connect() as connection:
                connection.execute(text("SET SESSION TRANSACTION READ ONLY"))
                connection.execute(text(f"SET SESSION MAX_EXECUTION_TIME = {self.settings.statement_timeout_ms}"))
                connection.execute(text(f"SET SESSION innodb_lock_wait_timeout = {max(1, self.settings.lock_timeout_ms // 1000)}"))
                connection.commit()
                transaction = connection.begin()
                try:
                    raw_plan = connection.execute(text(f"EXPLAIN FORMAT=JSON {sql}")).scalar_one()
                    self._check_plan(raw_plan)
                    return self._rows(connection.execute(text(self._bounded_query(sql))))
                finally:
                    transaction.rollback()
                    connection.execute(text("SET SESSION TRANSACTION READ WRITE"))
                    connection.commit()
        except QueryExecutionError:
            raise
        except DBAPIError as error:
            raise classify_database_error(error) from error

    def _check_plan(self, raw_plan: Any) -> None:
        import json

        plan = json.loads(raw_plan) if isinstance(raw_plan, str) else raw_plan
        cost = float(plan.get("query_block", {}).get("cost_info", {}).get("query_cost", 0))
        if cost > self.settings.max_plan_cost:
            raise QueryExecutionError("plan_cost_exceeded", retryable=False)

    def _probe_exists(self, statement: Any) -> bool:
        try:
            with self.engine.connect() as connection:
                connection.execute(text("SET SESSION TRANSACTION READ ONLY"))
                connection.execute(text(f"SET SESSION MAX_EXECUTION_TIME = {self.settings.statement_timeout_ms}"))
                connection.commit()
                transaction = connection.begin()
                try:
                    return connection.execute(statement).first() is not None
                finally:
                    transaction.rollback()
                    connection.execute(text("SET SESSION TRANSACTION READ WRITE"))
                    connection.commit()
        except DBAPIError as error:
            raise classify_database_error(error) from error

    @contextmanager
    def _profile_connection(self):
        with self.engine.connect() as connection:
            connection.execute(text("SET SESSION TRANSACTION READ ONLY"))
            connection.execute(text(f"SET SESSION MAX_EXECUTION_TIME = {self.settings.statement_timeout_ms}"))
            connection.execute(text(f"SET SESSION innodb_lock_wait_timeout = {max(1, self.settings.lock_timeout_ms // 1000)}"))
            connection.commit()
            transaction = connection.begin()
            try:
                yield connection
            finally:
                transaction.rollback()
                connection.execute(text("SET SESSION TRANSACTION READ WRITE"))
                connection.commit()


def create_adapter(
    dialect: str, secret_ref: str, allowed_tables: set[TableRef], settings: Settings | None = None
) -> DataSourceAdapter:
    resolved_settings = settings or get_settings()
    dsn = resolve_datasource_secret(secret_ref, resolved_settings)
    if dialect == "postgresql":
        return PostgreSQLAdapter(dsn, allowed_tables, resolved_settings)
    if dialect == "mysql":
        return MySQLAdapter(dsn, allowed_tables, resolved_settings)
    raise ValueError("unsupported_datasource_dialect")


def _escape_like(value: str) -> str:
    return value.replace("\\", "\\\\").replace("%", "\\%").replace("_", "\\_")


@lru_cache(maxsize=64)
def _datasource_engine(dialect: str, dsn: str):
    """Reuse bounded connection pools; a rotated DSN naturally creates a new pool key."""
    return create_engine(
        dsn,
        pool_pre_ping=True,
        pool_size=5,
        max_overflow=5,
        pool_recycle=1_800,
        pool_timeout=5,
        hide_parameters=True,
    )
