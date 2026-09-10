from collections.abc import Callable
from dataclasses import dataclass
from difflib import SequenceMatcher
import re
import unicodedata

from sqlglot import exp, parse, parse_one
from sqlglot.errors import ParseError


class UnsafeSQL(ValueError):
    def __init__(self, code: str, repair_hint: str = "") -> None:
        super().__init__(code)
        self.code = code
        self.repair_hint = repair_hint


@dataclass(frozen=True)
class ValidatedSQL:
    sql: str
    tables: frozenset[str]


_FORBIDDEN = (
    exp.Insert,
    exp.Update,
    exp.Delete,
    exp.Create,
    exp.Drop,
    exp.Alter,
    exp.Command,
    exp.Merge,
    exp.Copy,
    exp.Transaction,
)

_FORBIDDEN_FUNCTIONS = {
    "benchmark", "dblink", "dblink_connect", "dblink_connect_u", "dblink_exec",
    "load_file", "lo_export", "lo_import", "pg_ls_dir", "pg_read_binary_file",
    "pg_read_file", "pg_stat_file", "sleep", "sys_eval", "sys_exec",
    "cursor_to_xml", "cursor_to_xmlschema", "database_to_xml",
    "database_to_xml_and_xmlschema", "database_to_xmlschema", "query_to_xml",
    "query_to_xml_and_xmlschema", "query_to_xmlschema", "schema_to_xml",
    "schema_to_xml_and_xmlschema", "schema_to_xmlschema", "table_to_xml",
    "table_to_xml_and_xmlschema", "table_to_xmlschema",
}


def _qualified_table_name(table: exp.Table) -> str:
    return ".".join(part for part in (table.catalog, table.db, table.name) if part)


def normalize_unambiguous_column_names(
    sql: str, semantic_to_physical: dict[str, str], dialect: str = "postgres"
) -> str:
    """Resolve model-emitted semantic column labels through the selected Profile."""
    try:
        statement = parse_one(sql, read=dialect)
    except ParseError:
        return sql
    normalized = {name.strip().casefold(): physical for name, physical in semantic_to_physical.items()}
    for column in statement.find_all(exp.Column):
        physical = normalized.get(column.name.strip().casefold())
        if physical:
            column.set("this", exp.to_identifier(physical))
    return statement.sql(dialect=dialect)


def remove_impossible_complete_profile_or_branches(
    sql: str,
    supported_values_by_column: dict[str, str],
    complete_value_columns: set[str],
    dialect: str = "postgres",
) -> str:
    """Remove OR branches proven false by a complete low-cardinality Profile.

    This is an AST identity simplification (`FALSE OR x == x`), not a semantic
    guess. Sampled/high-cardinality columns are intentionally excluded.
    """
    try:
        statement = parse_one(sql, read=dialect)
    except ParseError:
        return sql
    complete = {column.casefold() for column in complete_value_columns}
    support_by_column = {
        name.casefold(): _normalize_value_text(value)
        for name, value in supported_values_by_column.items()
        if name.casefold() in complete
    }
    aliases = _table_aliases(statement)
    for disjunction in list(statement.find_all(exp.Or)):
        left_impossible = _predicate_is_impossible(disjunction.this, support_by_column, aliases)
        right_impossible = _predicate_is_impossible(disjunction.expression, support_by_column, aliases)
        if left_impossible is True and right_impossible is False:
            disjunction.replace(disjunction.expression.copy())
        elif right_impossible is True and left_impossible is False:
            disjunction.replace(disjunction.this.copy())
    return statement.sql(dialect=dialect)


def validate_read_only_sql(
    sql: str, allowed_tables: set[str], max_tables: int = 3, dialect: str = "postgres",
    question_text: str = "",
    supported_values_by_column: dict[str, str] | None = None,
    probeable_columns: set[str] | None = None,
    verify_columns: set[str] | None = None,
    literal_support_probe: Callable[[str, str], bool] | None = None,
) -> ValidatedSQL:
    if dialect not in {"postgres", "mysql"}:
        raise UnsafeSQL("unsupported_dialect")
    try:
        statements = parse(sql, read=dialect)
    except ParseError as error:
        raise UnsafeSQL("invalid_sql") from error
    if len(statements) != 1:
        raise UnsafeSQL("multiple_statements")
    statement = statements[0]
    if not isinstance(statement, exp.Query) or any(statement.find(kind) for kind in _FORBIDDEN):
        raise UnsafeSQL("read_only_required")
    for function in statement.find_all(exp.Func):
        function_name = (getattr(function, "name", "") or function.sql_name()).casefold()
        if function_name in _FORBIDDEN_FUNCTIONS or function_name.startswith("dblink_"):
            raise UnsafeSQL("dangerous_function")
    cte_names = {cte.alias_or_name for cte in statement.find_all(exp.CTE)}
    tables = {
        _qualified_table_name(table)
        for table in statement.find_all(exp.Table)
        if table.name not in cte_names
    }
    if not tables:
        raise UnsafeSQL("table_required")
    if len(tables) > max_tables:
        raise UnsafeSQL("too_many_tables")
    if not tables.issubset(allowed_tables):
        raise UnsafeSQL("unauthorized_table")
    if supported_values_by_column is not None:
        _validate_filter_literals(
            statement,
            supported_values_by_column,
            probeable_columns=probeable_columns or set(),
            verify_columns=verify_columns or set(),
            literal_support_probe=literal_support_probe,
        )
    _reject_unrequested_near_duplicate_or(statement, question_text)
    return ValidatedSQL(sql=statement.sql(dialect=dialect), tables=frozenset(tables))


def _reject_unrequested_near_duplicate_or(
    statement: exp.Expression, question_text: str
) -> None:
    """Prevent typo expansion from silently broadening one requested category.

    Two almost-identical alternatives often denote distinct business categories.
    They are allowed only when the user explicitly named both; otherwise the model
    must choose one using the question qualifier and Profile evidence.
    """
    if not question_text:
        return
    normalized_question = _normalize_value_text(question_text)
    for disjunction in statement.find_all(exp.Or):
        alternatives: list[str] = []
        for branch in (disjunction.this, disjunction.expression):
            if not isinstance(branch, (exp.Like, exp.ILike, exp.EQ)):
                continue
            literals = [item for item in branch.find_all(exp.Literal) if item.is_string]
            fragments = [
                part
                for literal in literals
                for part in re.split(r"[%_]", _normalize_value_text(literal.this))
                if len(part) >= 3
            ]
            if len(fragments) == 1:
                alternatives.append(fragments[0])
        if len(alternatives) != 2:
            continue
        left, right = alternatives
        similarity = SequenceMatcher(None, left, right).ratio()
        if similarity >= 0.8 and not (
            left in normalized_question and right in normalized_question
        ):
            raise UnsafeSQL(
                "ambiguous_synonym_expansion",
                "两个近似名称可能属于不同业务类别；结合限定词和 Profile 只保留一个标准值，不得用 OR 扩大口径",
            )


def _normalize_value_text(value: str) -> str:
    return re.sub(r"\s+", "", unicodedata.normalize("NFKC", value).casefold())


_FILTER_PREDICATES = (
    exp.EQ, exp.NEQ, exp.GT, exp.GTE, exp.LT, exp.LTE,
    exp.Like, exp.ILike, exp.In, exp.Between,
)


def _validate_filter_literals(
    statement: exp.Expression,
    supported_values_by_column: dict[str, str],
    *,
    probeable_columns: set[str],
    verify_columns: set[str],
    literal_support_probe: Callable[[str, str], bool] | None,
) -> None:
    """Reject invented filter text where SQL crosses into the database executor."""
    support_by_column = {
        name.casefold(): _normalize_value_text(value)
        for name, value in supported_values_by_column.items()
    }
    probeable = {name.casefold() for name in probeable_columns}
    verify = {name.casefold() for name in verify_columns}
    aliases = _table_aliases(statement)
    failures: list[str] = []
    # Filtering predicates also occur inside conditional aggregates (CASE WHEN),
    # not only WHERE/HAVING clauses.
    for predicate in statement.walk():
        if not isinstance(predicate, _FILTER_PREDICATES):
            continue
        columns = list(predicate.find_all(exp.Column))
        if len(columns) != 1:
            continue
        support_key = _resolve_column_key(columns[0], support_by_column, aliases)
        if support_key is None:
            continue
        support = support_by_column[support_key]
        for literal in predicate.find_all(exp.Literal):
            if not literal.is_string:
                continue
            value = _normalize_value_text(literal.this)
            # LIKE wildcards separate independently supported fragments. A
            # single-character fragment is too weak to validate meaningfully.
            fragments = [part for part in re.split(r"[%_]", value) if len(part) >= 2]
            if support_key in verify and literal_support_probe is not None:
                unsupported = [
                    fragment for fragment in fragments
                    if not literal_support_probe(support_key, fragment)
                ]
            else:
                unsupported = [fragment for fragment in fragments if fragment not in support]
            if (
                unsupported
                and support_key in probeable
                and literal_support_probe is not None
                and all(literal_support_probe(support_key, fragment) for fragment in unsupported)
            ):
                unsupported = []
            if unsupported:
                overlap = _longest_supported_overlap(unsupported[0], support)
                excerpt = _nearest_supported_excerpt(unsupported[0], support)
                hint = (
                    f"column={support_key}; "
                    f"unsupported_fragment={unsupported[0][:48]}"
                )
                if overlap:
                    hint += f"; supported_overlap={overlap}"
                if excerpt:
                    hint += f"; nearest_profile_evidence={excerpt}"
                if hint not in failures:
                    failures.append(hint)
    if failures:
        # Preserve evidence for multiple invalid alternatives in the same SQL so
        # the one allowed repair call is not biased by whichever OR branch the
        # model happened to emit first. Each excerpt is independently bounded.
        raise UnsafeSQL("unsupported_value_literal", " | ".join(failures[:3]))


def _predicate_is_impossible(
    predicate: exp.Expression,
    support_by_column: dict[str, str],
    aliases: dict[str, str],
) -> bool | None:
    """Return True/False only for a directly provable single-column predicate."""
    if not isinstance(predicate, _FILTER_PREDICATES):
        return None
    columns = list(predicate.find_all(exp.Column))
    if len(columns) != 1:
        return None
    support_key = _resolve_column_key(columns[0], support_by_column, aliases)
    if support_key is None:
        return None
    support = support_by_column[support_key]
    literals = [literal for literal in predicate.find_all(exp.Literal) if literal.is_string]
    if not literals:
        return None
    for literal in literals:
        value = _normalize_value_text(literal.this)
        fragments = [part for part in re.split(r"[%_]", value) if len(part) >= 2]
        if any(fragment not in support for fragment in fragments):
            return True
    return False


def _table_aliases(statement: exp.Expression) -> dict[str, str]:
    aliases: dict[str, str] = {}
    for table in statement.find_all(exp.Table):
        qualified = _qualified_table_name(table).casefold()
        aliases[table.name.casefold()] = qualified
        if table.alias:
            aliases[table.alias.casefold()] = qualified
    return aliases


def _resolve_column_key(
    column: exp.Column,
    support_by_column: dict[str, str],
    aliases: dict[str, str],
) -> str | None:
    name = column.name.casefold()
    if column.table:
        table = aliases.get(column.table.casefold(), column.table.casefold())
        qualified = f"{table}.{name}"
        if qualified in support_by_column:
            return qualified
        short = f"{table.rsplit('.', 1)[-1]}.{name}"
        if short in support_by_column:
            return short
        return None
    matches = [key for key in support_by_column if key == name or key.endswith(f".{name}")]
    return matches[0] if len(matches) == 1 else None


def _nearest_supported_excerpt(value: str, support: str, context_chars: int = 48) -> str:
    """Return bounded source evidence around the longest literal overlap."""
    for size in range(len(value), 1, -1):
        for start in range(len(value) - size + 1):
            fragment = value[start : start + size]
            position = support.find(fragment)
            if position >= 0:
                left = max(0, position - context_chars)
                right = min(len(support), position + size + context_chars)
                return support[left:right]
    return ""


def _longest_supported_overlap(value: str, support: str) -> str:
    for size in range(len(value), 1, -1):
        for start in range(len(value) - size + 1):
            fragment = value[start : start + size]
            if fragment in support:
                return fragment
    return ""
