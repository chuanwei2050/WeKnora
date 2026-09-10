from time import perf_counter
from datetime import datetime, timezone
from dataclasses import dataclass
from collections.abc import Callable
from difflib import SequenceMatcher
import re
import unicodedata
from uuid import UUID

from sqlalchemy import select
from sqlalchemy.orm import selectinload
from sqlglot import exp, parse_one

from .contracts import Evidence, QueryRequest, QueryResponse, QueryTimings, SourceRef
from .config import get_settings
from .database import DataColumn, DataSource, DataTable, Dataset, DatasetSourceType, DatasetVersion, QueryTrace, session_factory
from .candidate_search import (
    diversify_value_hits,
    lexical_terms,
    lexical_profile_search,
    refine_value_hits_for_tables,
    retrieve_profile_candidates,
)
from .datasource_adapters import TableRef, create_adapter
from .execution import explain_and_execute, supports_managed_literal
from .execution import QueryExecutionError
from .model_gateway import ModelGenerationError, generate_sql
from .sql_safety import (
    normalize_unambiguous_column_names,
    remove_impossible_complete_profile_or_branches,
    validate_read_only_sql,
)
from .table_selector import confirmed_join_limit, select_schema_candidates
from .sql_safety import UnsafeSQL


@dataclass(frozen=True)
class QueryAttemptOutcome:
    route: str
    sql: str
    sql_attempts: list[str]
    error_codes: list[str]
    model_calls: int
    result: object
    model_ms: int
    validation_ms: int
    execution_ms: int


class QueryAttemptsFailed(ValueError):
    def __init__(self, code: str, attempts: list[str], error_codes: list[str], model_calls: int, model_ms: int, validation_ms: int, execution_ms: int):
        super().__init__(code)
        self.attempts = attempts
        self.error_codes = error_codes
        self.model_calls = model_calls
        self.model_ms = model_ms
        self.validation_ms = validation_ms
        self.execution_ms = execution_ms


def trace_sql_attempts(attempts: list[str]) -> list[str]:
    """Keep auditable SQL structure while removing all literal source values."""
    redacted: list[str] = []
    for sql in attempts:
        try:
            tree = parse_one(sql)
            tree = tree.transform(
                lambda node: exp.Literal.string("?") if isinstance(node, exp.Literal) else node
            )
            redacted.append(tree.sql())
        except Exception:
            redacted.append("<unparseable-sql-redacted>")
    return redacted


def execute_with_one_repair(
    tenant_id: str,
    question: str,
    schema_context: str,
    evidence_values: list[str],
    allowed: set[str],
    dialect: str,
    execute: Callable[[str], object],
    semantic_to_physical: dict[str, str] | None = None,
    dataset_scope: list[str] | None = None,
    supported_values_by_column: dict[str, str] | None = None,
    complete_value_columns: set[str] | None = None,
    max_sql_tables: int = 3,
    probeable_columns: set[str] | None = None,
    verify_columns: set[str] | None = None,
    literal_support_probe: Callable[[str, str], bool] | None = None,
    repair_schema_context: str | None = None,
) -> QueryAttemptOutcome:
    attempts: list[str] = []
    error_codes: list[str] = []
    repair: str | None = None
    model_seconds = validation_seconds = execution_seconds = 0.0
    for attempt_number in range(2):
        stage_started = perf_counter()
        try:
            generation = generate_sql(
                tenant_id, question,
                repair_schema_context if repair and repair_schema_context else schema_context,
                evidence_values,
                repair=repair, dialect=dialect, dataset_scope=dataset_scope,
            )
        except ModelGenerationError as error:
            model_seconds += perf_counter() - stage_started
            error_codes.append(error.code)
            if attempt_number == 1 or not error.retryable:
                raise QueryAttemptsFailed(
                    error.code, attempts, error_codes, attempt_number + 1,
                    int(model_seconds * 1000), int(validation_seconds * 1000), int(execution_seconds * 1000),
                ) from error
            repair = f"error_code={error.code}"
            continue
        model_seconds += perf_counter() - stage_started
        if generation.route == "none":
            return QueryAttemptOutcome(
                "none", "", attempts, error_codes, attempt_number + 1, None,
                int(model_seconds * 1000), int(validation_seconds * 1000), int(execution_seconds * 1000),
            )
        attempts.append(generation.sql)
        try:
            stage_started = perf_counter()
            normalized_sql = normalize_unambiguous_column_names(
                generation.sql, semantic_to_physical or {}, dialect=dialect
            )
            normalized_sql = remove_impossible_complete_profile_or_branches(
                normalized_sql,
                supported_values_by_column or {},
                complete_value_columns or set(),
                dialect=dialect,
            )
            validated = validate_read_only_sql(
                normalized_sql,
                allowed,
                max_tables=max_sql_tables,
                dialect=dialect,
                question_text=question,
                supported_values_by_column=supported_values_by_column,
                probeable_columns=probeable_columns,
                verify_columns=verify_columns,
                literal_support_probe=literal_support_probe,
            )
            validation_seconds += perf_counter() - stage_started
            stage_started = perf_counter()
            result = execute(validated.sql)
            execution_seconds += perf_counter() - stage_started
            return QueryAttemptOutcome(
                "sql", validated.sql, attempts, error_codes, attempt_number + 1, result,
                int(model_seconds * 1000), int(validation_seconds * 1000), int(execution_seconds * 1000),
            )
        except (UnsafeSQL, QueryExecutionError) as error:
            if isinstance(error, QueryExecutionError):
                execution_seconds += perf_counter() - stage_started
            else:
                validation_seconds += perf_counter() - stage_started
            code = error.code
            error_codes.append(code)
            if attempt_number == 1 or (isinstance(error, QueryExecutionError) and not error.retryable):
                raise QueryAttemptsFailed(
                    code, attempts, error_codes, attempt_number + 1,
                    int(model_seconds * 1000), int(validation_seconds * 1000), int(execution_seconds * 1000),
                ) from error
            repair = f"previous_attempt={generation.sql}\nerror_code={code}"
            if isinstance(error, UnsafeSQL) and error.repair_hint:
                repair += f"\nrepair_hint={error.repair_hint}"
    raise AssertionError("unreachable")


def _normalize_scope_text(value: str) -> str:
    normalized = unicodedata.normalize("NFKC", value).casefold()
    normalized = re.sub(r"\.[^.]{1,10}$", "", normalized)
    return re.sub(r"[^\w\u4e00-\u9fff]", "", normalized)


def _longest_shared_segment(left: str, right: str) -> str:
    """Return the longest contiguous metadata segment present in the question."""
    if not left or not right:
        return ""
    previous = [0] * (len(right) + 1)
    best_length = best_end = 0
    for left_index, left_char in enumerate(left, 1):
        current = [0] * (len(right) + 1)
        for right_index, right_char in enumerate(right, 1):
            if left_char != right_char:
                continue
            current[right_index] = previous[right_index - 1] + 1
            if current[right_index] > best_length:
                best_length = current[right_index]
                best_end = left_index
        previous = current
    return left[best_end - best_length : best_end]


def _match_profile_metadata_scope(question: str, datasets: list[Dataset]) -> tuple[list[Dataset], list[str]]:
    """Resolve explicit dataset/sheet scope from persisted metadata, without business phrases."""
    normalized_question = _normalize_scope_text(question)
    labels_by_dataset: dict[str, list[tuple[str, str]]] = {}
    for dataset in datasets:
        labels = [dataset.original_file_name]
        active_version = next(
            (version for version in dataset.versions if version.id == dataset.active_version_id), None
        )
        if active_version is not None:
            labels.extend(table.sheet_name for table in active_version.tables)
        labels_by_dataset[str(dataset.id)] = [
            (label, _normalize_scope_text(label)) for label in labels if _normalize_scope_text(label)
        ]

    candidates: list[tuple[Dataset, str, str]] = []
    for dataset in datasets:
        matches = [
            (_longest_shared_segment(normalized_label, normalized_question), original_label)
            for original_label, normalized_label in labels_by_dataset[str(dataset.id)]
        ]
        segment, label = max(matches, key=lambda item: len(item[0]), default=("", ""))
        if len(segment) < 4:
            continue
        owners = {
            dataset_id
            for dataset_id, labels in labels_by_dataset.items()
            if any(segment in normalized_label for _, normalized_label in labels)
        }
        if owners == {str(dataset.id)}:
            candidates.append((dataset, segment, label))

    if not candidates:
        return [], []
    return [item[0] for item in candidates], [item[2] for item in candidates]


def run_query(tenant_id: str, request: QueryRequest) -> QueryResponse:
    started = perf_counter()
    with session_factory()() as session:
        statement = (
            select(Dataset)
            .options(selectinload(Dataset.versions).selectinload(DatasetVersion.tables))
            .where(Dataset.tenant_id == tenant_id, Dataset.namespace == request.namespace)
        )
        if request.dataset_ids:
            statement = statement.where(Dataset.id.in_(request.dataset_ids))
        datasets = list(session.scalars(statement).unique())
        scoped_datasets, scope_labels = _match_profile_metadata_scope(request.question, datasets)
        if scoped_datasets:
            datasets = scoped_datasets
        datasets_by_version = {dataset.active_version_id: dataset for dataset in datasets}
        active_versions = [dataset.active_version_id for dataset in datasets if dataset.active_version_id]
        if not active_versions:
            raise ValueError("no_active_dataset")
        metadata_scope_done = perf_counter()
        candidates = retrieve_profile_candidates(
            session,
            tenant_id=tenant_id,
            namespace=request.namespace,
            version_ids=active_versions,
            question=request.question,
        )
        table_hits = candidates.table_hits
        column_hits = candidates.column_hits
        value_hits = candidates.value_hits
        if not table_hits:
            raise ValueError("no_relevant_table")
        table_selection_started = perf_counter()
        candidate_ids = {UUID(hit["table_id"]) for hit in table_hits}
        candidate_tables = list(session.scalars(
            select(DataTable)
            .options(selectinload(DataTable.columns))
            .where(DataTable.id.in_(candidate_ids), DataTable.version_id.in_(active_versions))
        ))
        tables_by_id = {str(table.id): table for table in candidate_tables}
        deduplicated_hits = []
        seen_content: set[tuple[str, str]] = set()
        for hit in table_hits:
            table = tables_by_id.get(str(hit["table_id"]))
            dataset = datasets_by_version.get(table.version_id) if table is not None else None
            if table is None or dataset is None:
                continue
            identity = (dataset.content_sha256, table.sheet_name)
            if identity in seen_content:
                continue
            seen_content.add(identity)
            deduplicated_hits.append(hit)
        # Default to the strongest table. Expand only through persisted,
        # trustworthy relationships; do not make the model inspect three
        # unrelated/repeated schemas merely because three candidates exist.
        shortlisted_tables = select_schema_candidates(
            deduplicated_hits, tables_by_id, max_tables=3
        )
        if not shortlisted_tables:
            raise ValueError("profile_table_mismatch")
        selected_tables = shortlisted_tables
        if not scope_labels:
            # Equivalent schemas represent overlapping snapshots or subsets. For an
            # unscoped aggregate/list question, use the broadest candidate rather
            # than silently answering from a smaller departmental subset.
            scope_tables = list(session.scalars(
                select(DataTable)
                .options(selectinload(DataTable.columns))
                .where(DataTable.version_id.in_(active_versions))
            ))
            broadest_by_signature = {}
            for table in scope_tables:
                signature = tuple(
                    column.original_name for column in sorted(table.columns, key=lambda item: item.ordinal)
                )
                current = broadest_by_signature.get(signature)
                if current is None or table.row_count > current.row_count:
                    broadest_by_signature[signature] = table
            selected_tables = [
                broadest_by_signature.get(
                    tuple(column.original_name for column in sorted(table.columns, key=lambda item: item.ordinal)),
                    table,
                )
                for table in selected_tables
            ]
            selected_tables = list({str(table.id): table for table in selected_tables}.values())
        selected_ids = {str(table.id) for table in selected_tables}
        column_hits = [hit for hit in column_hits if hit["table_id"] in selected_ids]
        # The global value shortlist helps rank tables, but may be dominated by
        # values from other datasets. Once tables are selected, perform a cheap,
        # model-free lexical refinement inside that executable scope so SQL sees
        # relevant stored spellings and fields instead of a truncated residue.
        table_selection_done = perf_counter()
        targeted_value_hits = lexical_profile_search(
            session,
            tenant_id=tenant_id,
            namespace=request.namespace,
            version_ids=active_versions,
            question=request.question,
            kind="value",
            limit=200,
            table_ids=selected_ids,
        )
        value_hits = refine_value_hits_for_tables(
            value_hits, targeted_value_hits, selected_ids
        )
        retrieval_done = perf_counter()
        timing_parts = {
            "retrieval_ms": int((retrieval_done - started) * 1000),
            "metadata_scope_ms": int((metadata_scope_done - started) * 1000),
            "lexical_ms": candidates.lexical_ms,
            "embedding_ms": candidates.embedding_ms,
            "vector_ms": candidates.vector_ms,
            "table_selection_ms": int((table_selection_done - table_selection_started) * 1000),
            "target_value_refine_ms": int((retrieval_done - table_selection_done) * 1000),
        }
        full_schema_context = "\n\n".join(
            f"【数据集文件】{datasets_by_version[table.version_id].original_file_name}\n"
            f"【工作表】{table.sheet_name}\n【行数】{table.row_count}\n{table.profile['mschema']}"
            for table in selected_tables
        )
        schema_context = _compact_schema_context(
            selected_tables,
            datasets_by_version,
            {str(hit["column_id"]) for hit in column_hits if hit.get("column_id")},
            request.question,
            full_schema_context,
        )
        evidence_values = _compact_evidence_values(value_hits, request.question)
        selected_datasets = [datasets_by_version[table.version_id] for table in selected_tables]
        datasource_ids = {dataset.data_source_id for dataset in selected_datasets}
        source_types = {dataset.source_type for dataset in selected_datasets}
        if len(datasource_ids) != 1 or len(source_types) != 1:
            raise ValueError("cross_datasource_join_forbidden")
        dataset = selected_datasets[0]
        source = session.get(DataSource, dataset.data_source_id) if dataset.data_source_id else None
        if dataset.source_type != DatasetSourceType.MANAGED_FILE and source is None:
            raise ValueError("datasource_metadata_incomplete")
        dialect = "postgres" if source is None or source.dialect == "postgresql" else "mysql"
        allowed = {
            table.physical_name if dataset.source_type == DatasetSourceType.MANAGED_FILE
            else f"{table.physical_schema}.{table.physical_name}"
            for table in selected_tables
        }
        refs = {TableRef(table.physical_schema, table.physical_name) for table in selected_tables}
        adapter = None if source is None else create_adapter(source.dialect, source.secret_ref, refs)
        semantic_candidates: dict[str, set[str]] = {}
        for table in selected_tables:
            for column in table.columns:
                semantic_candidates.setdefault(column.original_name.strip().casefold(), set()).add(column.physical_name)
        semantic_to_physical = {
            semantic: next(iter(physical_names))
            for semantic, physical_names in semantic_candidates.items()
            if len(physical_names) == 1
        }
        columns_by_id = {
            str(column.id): column
            for table in selected_tables
            for column in table.columns
        }
        table_by_column_id = {
            str(column.id): table
            for table in selected_tables
            for column in table.columns
        }
        support_targets: dict[str, tuple[DataTable, DataColumn]] = {}
        support_parts: dict[str, list[str]] = {}
        for column_id, column in columns_by_id.items():
            table = table_by_column_id[column_id]
            table_name = (
                table.physical_name
                if dataset.source_type == DatasetSourceType.MANAGED_FILE
                else f"{table.physical_schema}.{table.physical_name}"
            )
            key = f"{table_name}.{column.physical_name}".casefold()
            values = column.profile.get("values")
            support_parts[key] = [
                str(item.get("value", ""))
                for item in values if isinstance(item, dict) and item.get("value") is not None
            ] if isinstance(values, list) else []
            support_targets[key] = (table, column)
        for hit in value_hits:
            column = columns_by_id.get(str(hit.get("column_id", "")))
            if column is not None:
                table = table_by_column_id[str(column.id)]
                table_name = (
                    table.physical_name
                    if dataset.source_type == DatasetSourceType.MANAGED_FILE
                    else f"{table.physical_schema}.{table.physical_name}"
                )
                support_parts[f"{table_name}.{column.physical_name}".casefold()].append(
                    str(hit.get("text", ""))
                )
        supported_values_by_column = {
            name: "\n".join(parts) for name, parts in support_parts.items()
        }
        complete_value_columns = {
            key
            for key, (_, column) in support_targets.items()
            if isinstance(column.profile.get("values"), list)
            and len(column.profile["values"]) == column.profile.get("distinct_count")
        }
        verify_columns: set[str] = set()
        if source is not None:
            active_version = next(
                (
                    version
                    for version in dataset.versions
                    if version.id == dataset.active_version_id
                ),
                None,
            )
            activated_at = active_version.activated_at if active_version is not None else None
            if activated_at is None:
                complete_value_columns.clear()
                verify_columns = set(support_targets)
            else:
                if activated_at.tzinfo is None:
                    activated_at = activated_at.replace(tzinfo=timezone.utc)
                age = (datetime.now(timezone.utc) - activated_at).total_seconds()
                if age > get_settings().external_profile_max_age_seconds:
                    # A once-complete distinct set is no longer proof after the
                    # source database may have changed. Fall back to live probes.
                    complete_value_columns.clear()
                    verify_columns = set(support_targets)
        probeable_columns = set(support_targets) - complete_value_columns

        def probe_literal(column_key: str, fragment: str) -> bool:
            target = support_targets.get(column_key.casefold())
            if target is None:
                return False
            table, column = target
            if adapter is None:
                return supports_managed_literal(
                    table.physical_schema, table.physical_name, column.physical_name, fragment
                )
            return adapter.supports_literal(
                TableRef(table.physical_schema, table.physical_name), column.physical_name, fragment
            )
        try:
            outcome = execute_with_one_repair(
                tenant_id,
                request.question,
                schema_context,
                evidence_values,
                allowed,
                dialect,
                explain_and_execute if adapter is None else adapter.execute,
                semantic_to_physical,
                scope_labels,
                supported_values_by_column,
                complete_value_columns,
                confirmed_join_limit(selected_tables),
                probeable_columns,
                verify_columns,
                probe_literal,
                full_schema_context,
            )
        except QueryAttemptsFailed as failure:
            failed_at = perf_counter()
            failed_timings = QueryTimings(
                **timing_parts,
                model_ms=failure.model_ms,
                validation_ms=failure.validation_ms,
                execution_ms=failure.execution_ms,
                total_ms=int((failed_at - started) * 1000),
            )
            session.add(QueryTrace(
                tenant_id=tenant_id,
                namespace=request.namespace,
                dataset_ids=[str(item.id) for item in selected_datasets],
                sql_attempts=trace_sql_attempts(failure.attempts),
                error_codes=failure.error_codes,
                model_calls=failure.model_calls,
                timings=failed_timings.model_dump(),
            ))
            session.commit()
            raise
        result = outcome.result
        if outcome.route == "none":
            finished = perf_counter()
            timings = QueryTimings(
                **timing_parts,
                model_ms=outcome.model_ms,
                total_ms=int((finished - started) * 1000),
            )
            session.add(QueryTrace(
                tenant_id=tenant_id, namespace=request.namespace,
                dataset_ids=[], sql_attempts=[], error_codes=[],
                model_calls=outcome.model_calls, timings=timings.model_dump(),
            ))
            session.commit()
            return QueryResponse(route="none", timings=timings, model_calls=outcome.model_calls)
        used_table_names = {table.name for table in parse_one(outcome.sql, dialect=dialect).find_all(exp.Table)}
        used_tables = [table for table in selected_tables if table.physical_name in used_table_names]
        used_datasets = [datasets_by_version[table.version_id] for table in used_tables]
        finished = perf_counter()
        evidence = [
            Evidence(
                kind=hit["kind"],
                table_id=UUID(hit["table_id"]),
                column_id=UUID(hit["column_id"]) if hit["column_id"] else None,
                text=hit["text"],
                score=hit["score"],
            )
            for hit in [*table_hits, *column_hits, *value_hits]
        ]
        timings = QueryTimings(
            **timing_parts,
            model_ms=outcome.model_ms,
            validation_ms=outcome.validation_ms,
            execution_ms=outcome.execution_ms,
            total_ms=int((finished - started) * 1000),
        )
        session.add(QueryTrace(
            tenant_id=tenant_id,
            namespace=request.namespace,
            dataset_ids=[str(item.id) for item in used_datasets],
            sql_attempts=trace_sql_attempts(outcome.sql_attempts),
            error_codes=outcome.error_codes,
            model_calls=outcome.model_calls,
            timings=timings.model_dump(),
        ))
        session.commit()
        return QueryResponse(
            sql=outcome.sql,
            sql_attempts=outcome.sql_attempts,
            columns=result.columns,
            rows=result.rows,
            evidence=evidence,
            sources=[
                SourceRef(
                    dataset_id=datasets_by_version[table.version_id].id,
                    version_id=table.version_id,
                    table_id=table.id,
                    original_file_name=datasets_by_version[table.version_id].original_file_name,
                    sheet_name=table.sheet_name,
                ) for table in used_tables
            ],
            timings=timings,
            model_calls=outcome.model_calls,
        )


def _compact_evidence_values(
    hits: list[dict], question: str, *, per_item_chars: int = 360, total_chars: int = 3600
) -> list[str]:
    """Keep query-relevant value context under a stable, model-agnostic budget."""
    terms = lexical_terms(question, limit=32)
    qualified_subjects = _question_qualified_subjects(question)
    prioritized: list[tuple[int, str]] = []
    raw_texts: list[str] = []
    compacted: list[str] = []
    used = 0
    for hit in hits:
        text = str(hit.get("text", "")).strip()
        if not text:
            continue
        raw_texts.append(text)
        if len(text) > per_item_chars:
            lowered = text.casefold()
            positions = [lowered.find(term.casefold()) for term in terms]
            positions = [position for position in positions if position >= 0]
            center = min(positions) if positions else 0
            start = max(0, center - per_item_chars // 3)
            end = min(len(text), start + per_item_chars)
            start = max(0, end - per_item_chars)
            text = text[start:end]
        priority, prefix = _qualifier_evidence_label(text, qualified_subjects)
        prioritized.append((priority, prefix + text))
    for subject, qualifier in qualified_subjects:
        if not any(qualifier in unicodedata.normalize("NFKC", text).casefold() for text in raw_texts):
            prioritized.append((
                -1,
                f"[限定词证据] 当前候选未证明“{subject}（{qualifier}）”是目标字段中的连续值；"
                f"“{qualifier}”只用于类别消歧，不得拼入 SQL 字面值。筛选片段必须从下列候选连续复制。",
            ))
    for _, text in sorted(prioritized, key=lambda item: item[0]):
        remaining = total_chars - used
        if remaining <= 0:
            break
        text = text[:remaining]
        compacted.append(text)
        used += len(text)
    return compacted


def _question_qualified_subjects(question: str) -> list[tuple[str, str]]:
    """Extract local subject/qualifier pairs without encoding business vocabulary."""
    normalized = unicodedata.normalize("NFKC", question).casefold()
    pairs: list[tuple[str, str]] = []
    for match in re.finditer(r"([^,，。；;、或/]{2,24})\(([^()]{1,16})\)", normalized):
        subject = re.sub(r"^(?:请|问|查询|统计|具有|持有|拥有|是否有)+", "", match.group(1)).strip()
        if len(subject) >= 2:
            pairs.append((subject[-12:], match.group(2).strip()))
    return pairs


def _qualifier_evidence_label(text: str, subjects: list[tuple[str, str]]) -> tuple[int, str]:
    """Put qualifier-consistent values first and explicitly isolate near-name conflicts."""
    if not subjects:
        return 1, ""
    normalized = unicodedata.normalize("NFKC", text).casefold()
    evidence_pairs = [
        (match.group(1).strip()[-12:], match.group(2).strip())
        for match in re.finditer(r"([^,，。；;、:\n]{2,24})\(([^()]{1,16})\)", normalized)
    ]
    for subject, qualifier in subjects:
        if qualifier and qualifier in normalized:
            return 0, f"[与问题限定词“{qualifier}”一致] "
        for evidence_subject, evidence_qualifier in evidence_pairs:
            similarity = SequenceMatcher(None, subject, evidence_subject).ratio()
            if similarity >= 0.62 and evidence_qualifier != qualifier:
                return 2, (
                    f"[近似名称但限定词为“{evidence_qualifier}”，不得与“{qualifier}”作为同一条件 OR 合并] "
                )
    return 1, ""


def _compact_schema_context(
    tables: list[DataTable],
    datasets_by_version: dict[UUID, Dataset],
    relevant_column_ids: set[str],
    question: str,
    full_context: str,
    *,
    max_columns_per_table: int = 32,
) -> str:
    """Prune only wide schemas; a repair call receives the full stored M-Schema."""
    if all(len(table.columns) <= max_columns_per_table for table in tables):
        return full_context
    blocks: list[str] = []
    normalized_question = _normalize_scope_text(question)
    for table in tables:
        ordered = sorted(table.columns, key=lambda item: item.ordinal)
        if len(ordered) <= max_columns_per_table:
            chosen = ordered
        else:
            key_names = {
                str(name).casefold()
                for name in [
                    *table.profile.get("primary_key", []),
                    *[
                        column
                        for relation in table.profile.get("foreign_keys", [])
                        for column in relation.get("constrained_columns", [])
                    ],
                ]
            }
            preferred = [
                column for column in ordered
                if str(column.id) in relevant_column_ids
                or column.physical_name.casefold() in key_names
                or _normalize_scope_text(column.original_name) in normalized_question
            ]
            chosen_ids = {str(column.id) for column in preferred}
            chosen = preferred + [column for column in ordered if str(column.id) not in chosen_ids]
            chosen = sorted(chosen[:max_columns_per_table], key=lambda item: item.ordinal)
        lines = []
        for column in chosen:
            values = column.profile.get("values", [])
            examples = ", ".join(
                str(item.get("value", "")) for item in values[:3] if isinstance(item, dict)
            )
            suffix = f", Examples: [{examples}]" if examples else ""
            lines.append(
                f"  ({column.physical_name}:{column.original_name}, {column.data_type}, "
                f"nullable={str(column.nullable).lower()}{suffix})"
            )
        dataset = datasets_by_version[table.version_id]
        blocks.append(
            f"【数据集文件】{dataset.original_file_name}\n【工作表】{table.sheet_name}\n"
            f"【行数】{table.row_count}\n# Table: {table.physical_name}\n[\n"
            + "\n".join(lines)
            + "\n]"
        )
    return "\n\n".join(blocks)
