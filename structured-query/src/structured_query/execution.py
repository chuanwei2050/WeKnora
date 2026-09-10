from dataclasses import dataclass
from typing import Any

from sqlalchemy import MetaData, String, Table, cast, select, text
from sqlalchemy.exc import DBAPIError

from .config import get_settings
from .database import engine


@dataclass(frozen=True)
class ExecutionResult:
    columns: list[str]
    rows: list[list[Any]]


class QueryExecutionError(ValueError):
    def __init__(self, code: str, *, retryable: bool) -> None:
        super().__init__(code)
        self.code = code
        self.retryable = retryable


def bounded_result(columns: list[str], raw_rows: list[Any]) -> ExecutionResult:
    """Reject unexpectedly large result payloads before JSON serialization."""
    settings = get_settings()
    total_bytes = sum(len(str(column).encode("utf-8")) for column in columns)
    rows: list[list[Any]] = []
    for raw_row in raw_rows:
        row = list(raw_row)
        for value in row:
            size = len(str(value).encode("utf-8")) if value is not None else 0
            if size > settings.max_result_cell_bytes:
                raise QueryExecutionError("result_cell_too_large", retryable=False)
            total_bytes += size
            if total_bytes > settings.max_result_bytes:
                raise QueryExecutionError("result_too_large", retryable=False)
        rows.append(row)
    return ExecutionResult(columns=columns, rows=rows)


def _plan_root(raw: Any) -> dict[str, Any]:
    if isinstance(raw, list) and raw and isinstance(raw[0], dict):
        return raw[0].get("Plan", {})
    if isinstance(raw, dict):
        return raw.get("Plan", {})
    return {}


def classify_database_error(error: DBAPIError) -> QueryExecutionError:
    sqlstate = getattr(error.orig, "sqlstate", None)
    mysql_code = error.orig.args[0] if getattr(error.orig, "args", ()) else None
    if sqlstate in {"42703", "42P01", "42883"}:
        return QueryExecutionError("invalid_reference", retryable=True)
    if mysql_code in {1054, 1146, 1305}:
        return QueryExecutionError("invalid_reference", retryable=True)
    if sqlstate in {"57014", "55P03"}:
        return QueryExecutionError("query_timeout", retryable=False)
    if mysql_code in {1205, 1317, 3024}:
        return QueryExecutionError("query_timeout", retryable=False)
    return QueryExecutionError("query_execution_failed", retryable=False)


def explain_and_execute(sql: str) -> ExecutionResult:
    settings = get_settings()
    with engine().connect() as connection:
        transaction = connection.begin()
        try:
            connection.execute(text("SET TRANSACTION READ ONLY"))
            connection.execute(text(f"SET LOCAL statement_timeout = {settings.statement_timeout_ms}"))
            connection.execute(text(f"SET LOCAL lock_timeout = {settings.lock_timeout_ms}"))
            connection.execute(text("SET LOCAL search_path = sq_data, pg_catalog"))
            plan = _plan_root(connection.execute(text(f"EXPLAIN (FORMAT JSON) {sql}")).scalar_one())
            if float(plan.get("Total Cost", 0)) > settings.max_plan_cost:
                raise QueryExecutionError("plan_cost_exceeded", retryable=False)
            if int(plan.get("Plan Rows", 0)) > settings.max_plan_rows:
                raise QueryExecutionError("plan_rows_exceeded", retryable=False)
            result = connection.execute(
                text(f"SELECT * FROM ({sql}) AS sq_bounded_result LIMIT {settings.max_result_rows}")
            )
            return bounded_result(list(result.keys()), result.fetchall())
        except DBAPIError as error:
            raise classify_database_error(error) from error
        finally:
            transaction.rollback()


def supports_managed_literal(schema: str, table: str, column: str, fragment: str) -> bool:
    """Check existence in one trusted managed column without exposing matching rows."""
    reflected = Table(table, MetaData(), schema=schema, autoload_with=engine())
    if column not in reflected.c:
        raise QueryExecutionError("invalid_reference", retryable=False)
    escaped = fragment.replace("\\", "\\\\").replace("%", "\\%").replace("_", "\\_")
    with engine().connect() as connection:
        transaction = connection.begin()
        try:
            connection.execute(text("SET TRANSACTION READ ONLY"))
            connection.execute(text(f"SET LOCAL statement_timeout = {get_settings().statement_timeout_ms}"))
            return connection.execute(
                select(1)
                .where(cast(reflected.c[column], String).ilike(f"%{escaped}%", escape="\\"))
                .limit(1)
            ).first() is not None
        except DBAPIError as error:
            raise classify_database_error(error) from error
        finally:
            transaction.rollback()
