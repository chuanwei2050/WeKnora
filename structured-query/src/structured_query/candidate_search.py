from __future__ import annotations

from collections import defaultdict
from dataclasses import dataclass
from math import log
import re
from time import perf_counter
from typing import Any
from uuid import UUID

from sqlalchemy import case, func, or_, select
from sqlalchemy.orm import Session

from .database import ColumnValue, DataColumn, DataTable, Dataset, DatasetVersion
from .embeddings import embed_texts
from .vector_store import search_profiles


@dataclass(frozen=True)
class ProfileCandidates:
    table_hits: list[dict[str, Any]]
    column_hits: list[dict[str, Any]]
    value_hits: list[dict[str, Any]]
    lexical_ms: int = 0
    embedding_ms: int = 0
    vector_ms: int = 0


def retrieve_profile_candidates(
    session: Session,
    *,
    tenant_id: str,
    namespace: str,
    version_ids: list[UUID],
    question: str,
) -> ProfileCandidates:
    """Run the complete global Profile retrieval stage with one vector fallback."""
    limits = {"table": 8, "column": 16, "value": 40}

    # Reuse the request session. Opening three child sessions while every request
    # retains its parent connection can exhaust the pool and deadlock in bursts.
    lexical_started = perf_counter()
    lexical_hits = {
        kind: lexical_profile_search(
            session,
            tenant_id=tenant_id,
            namespace=namespace,
            version_ids=version_ids,
            question=question,
            kind=kind,
            limit=limit * 2,
        )
        for kind, limit in limits.items()
    }
    lexical_ms = int((perf_counter() - lexical_started) * 1000)
    embedding_ms = 0
    vector_ms = 0

    if lexical_evidence_is_sufficient(
        lexical_hits["table"], lexical_hits["column"], lexical_hits["value"]
    ):
        result = ProfileCandidates(*(
            fuse_ranked_hits([], lexical_hits[kind], limits[kind])
            for kind in ("table", "column", "value")
        ), lexical_ms=lexical_ms)
    else:
        embedding_started = perf_counter()
        query_vector = embed_texts(tenant_id, [question])[0]
        embedding_ms = int((perf_counter() - embedding_started) * 1000)
        vector_started = perf_counter()
        result = ProfileCandidates(*(
            hybrid_profile_search(
                session,
                tenant_id=tenant_id,
                namespace=namespace,
                version_ids=version_ids,
                question=question,
                kind=kind,
                limit=limits[kind],
                query_vector=query_vector,
                lexical_hits=lexical_hits[kind],
            )
            for kind in ("table", "column", "value")
        ), lexical_ms=lexical_ms, embedding_ms=embedding_ms)
        vector_ms = int((perf_counter() - vector_started) * 1000)
    return ProfileCandidates(
        rank_tables_with_profile_evidence(
            result.table_hits, result.column_hits, result.value_hits
        ),
        result.column_hits,
        result.value_hits,
        lexical_ms=lexical_ms,
        embedding_ms=embedding_ms,
        vector_ms=vector_ms,
    )
def lexical_terms(question: str, limit: int = 64) -> list[str]:
    normalized = re.sub(r"\s+", " ", question.strip().lower())
    terms: list[str] = []

    def append(term: str) -> None:
        if len(term) >= 2 and term not in terms:
            terms.append(term)

    for part in re.split(r"[^\w\u4e00-\u9fff]+", normalized):
        append(part)
    compact = re.sub(r"[^\w\u4e00-\u9fff]", "", normalized)
    if compact:
        append(compact)
        if any("\u4e00" <= char <= "\u9fff" for char in compact):
            # Bigrams provide full-query recall before longer n-grams consume the
            # bounded term budget. This matters for multi-condition questions and
            # common word-order variants without encoding domain synonyms.
            for size in (2, 3, 4):
                for index in range(max(0, len(compact) - size + 1)):
                    append(compact[index : index + size])
                    if len(terms) >= limit:
                        return terms
    return terms[:limit]


def _identity(hit: dict[str, Any]) -> tuple[str, str, str, str]:
    identity_text = str(hit.get("text", "")) if hit.get("kind") == "value" else ""
    return (
        str(hit.get("kind", "")),
        str(hit.get("table_id", "")),
        str(hit.get("column_id", "")),
        identity_text,
    )


def fuse_ranked_hits(vector_hits: list[dict[str, Any]], lexical_hits: list[dict[str, Any]], limit: int) -> list[dict[str, Any]]:
    combined: dict[tuple[str, str, str, str], dict[str, Any]] = {}
    scores: defaultdict[tuple[str, str, str, str], float] = defaultdict(float)
    sources: defaultdict[tuple[str, str, str, str], set[str]] = defaultdict(set)
    for source, hits in (("vector", vector_hits), ("lexical", lexical_hits)):
        weight = 1.25 if source == "lexical" else 1.0
        for rank, hit in enumerate(hits, 1):
            key = _identity(hit)
            combined.setdefault(key, dict(hit))
            if source == "lexical":
                combined[key]["lexical_score"] = float(hit.get("score", 0))
                combined[key]["title_score"] = float(hit.get("title_score", 0))
            scores[key] += weight / (60 + rank)
            sources[key].add(source)
    ranked = sorted(combined, key=lambda key: (-scores[key], key))[:limit]
    max_lexical = max((float(combined[key].get("lexical_score", 0)) for key in ranked), default=0.0)
    max_title = max((float(combined[key].get("title_score", 0)) for key in ranked), default=0.0)
    return [
        {
            **combined[key],
            "score": scores[key]
            + (0.15 * float(combined[key].get("lexical_score", 0)) / max_lexical if max_lexical else 0)
            + (0.75 * float(combined[key].get("title_score", 0)) / max_title if max_title else 0),
            "sources": sorted(sources[key]),
        }
        for key in ranked
    ]


def _lexical_hits(
    session: Session, tenant_id: str, namespace: str, version_ids: list[UUID], question: str,
    kind: str, limit: int, table_ids: set[str] | None = None,
) -> list[dict[str, Any]]:
    # Full text plus all Chinese bigrams fits ordinary questions comfortably in
    # this budget. Larger trigram expansions multiply CASE/LIKE work over every
    # profiled value without materially improving target-table recall.
    terms = lexical_terms(question, limit=48)
    if not terms:
        return []
    scope = (
        Dataset.tenant_id == tenant_id,
        Dataset.namespace == namespace,
        DatasetVersion.id.in_(version_ids),
        Dataset.active_version_id == DatasetVersion.id,
    )
    table_scope = (DataTable.id.in_([UUID(table_id) for table_id in table_ids]),) if table_ids else ()
    if kind == "table":
        column_text = select(func.string_agg(DataColumn.original_name, " ")).where(DataColumn.table_id == DataTable.id).correlate(DataTable).scalar_subquery()
        # The source file name is part of the table's semantic identity. Sheet names
        # such as "人员资质统计" repeat across many workbooks, while the file name
        # distinguishes personnel, company-certificate, finance, and project data.
        content = func.lower(
            func.concat_ws(
                " ", Dataset.original_file_name, DataTable.sheet_name, func.coalesce(column_text, "")
            )
        )
        statement = select(DataTable.id, DataTable.id, content, column_text, Dataset.original_file_name).select_from(DataTable).join(DatasetVersion, DatasetVersion.id == DataTable.version_id).join(Dataset, Dataset.id == DatasetVersion.dataset_id).where(*scope, *table_scope)
        rows = session.execute(statement).all()
        document_frequency = {
            term: sum(1 for row in rows if term in (row[2] or "")) for term in terms
        }
        scored = []
        for row in rows:
            text = row[2] or ""
            score = sum(
                (log((len(rows) + 1) / (document_frequency[term] + 1)) + 1) * max(1, len(term) - 1)
                for term in terms if term in text
            )
            score += sum(
                len(name) * 4 for name in (row[3] or "").lower().split()
                if len(name) >= 2 and name in question.lower()
            )
            if score > 0:
                file_name = (row[4] or "").lower()
                title_score = sum(max(1, len(term) - 1) for term in terms if term in file_name)
                scored.append({"kind": kind, "table_id": str(row[0]), "column_id": "", "text": text, "score": score, "title_score": title_score})
        return sorted(scored, key=lambda hit: (-hit["score"], hit["table_id"]))[:limit]
    elif kind == "column":
        content = func.lower(DataColumn.original_name + " " + DataColumn.data_type)
        statement = select(DataTable.id, DataColumn.id, content).select_from(DataColumn).join(DataTable, DataTable.id == DataColumn.table_id).join(DatasetVersion, DatasetVersion.id == DataTable.version_id).join(Dataset, Dataset.id == DatasetVersion.dataset_id).where(*scope, *table_scope)
        indexed_match = or_(
            *(func.lower(DataColumn.original_name).contains(term) for term in terms),
            *(func.lower(DataColumn.data_type).contains(term) for term in terms),
        )
    elif kind == "value":
        content = func.lower(DataColumn.original_name + ": " + ColumnValue.value)
        statement = select(DataTable.id, DataColumn.id, content).select_from(ColumnValue).join(DataColumn, DataColumn.id == ColumnValue.column_id).join(DataTable, DataTable.id == DataColumn.table_id).join(DatasetVersion, DatasetVersion.id == DataTable.version_id).join(Dataset, Dataset.id == DatasetVersion.dataset_id).where(*scope, *table_scope)
        indexed_match = or_(
            *(func.lower(ColumnValue.value).contains(term) for term in terms),
            *(func.lower(DataColumn.original_name).contains(term) for term in terms),
        )
    else:
        raise ValueError("unsupported_profile_kind")
    lexical_score = sum((case((content.contains(term), 1), else_=0) for term in terms))
    rows = session.execute(
        statement.add_columns(lexical_score.label("lexical_score"))
        .where(indexed_match)
        .order_by(lexical_score.desc())
        .limit(limit)
    ).all()
    return [
        {"kind": kind, "table_id": str(row[0]), "column_id": str(row[1]) if kind != "table" else "", "text": row[2], "score": float(row[3])}
        for row in rows
    ]


def hybrid_profile_search(
    session: Session, *, tenant_id: str, namespace: str, version_ids: list[UUID], question: str, kind: str, limit: int,
    query_vector: list[float] | None = None, lexical_hits: list[dict[str, Any]] | None = None,
) -> list[dict[str, Any]]:
    vector = search_profiles(
        tenant_id=tenant_id, namespace=namespace, version_ids=version_ids,
        question=question, kind=kind, limit=limit * 2, query_vector=query_vector,
    )
    lexical = lexical_hits if lexical_hits is not None else _lexical_hits(
        session, tenant_id, namespace, version_ids, question, kind, limit * 2
    )
    return fuse_ranked_hits(vector, lexical, limit)


def lexical_profile_search(
    session: Session, *, tenant_id: str, namespace: str, version_ids: list[UUID],
    question: str, kind: str, limit: int, table_ids: set[str] | None = None,
) -> list[dict[str, Any]]:
    """Search persisted Profile text without invoking an embedding model."""
    return _lexical_hits(
        session, tenant_id, namespace, version_ids, question, kind, limit, table_ids
    )


def rank_tables_with_profile_evidence(
    table_hits: list[dict[str, Any]], column_hits: list[dict[str, Any]], value_hits: list[dict[str, Any]]
) -> list[dict[str, Any]]:
    """Aggregate schema and real-value links back to tables, including value-only matches."""
    column_scores: defaultdict[str, list[float]] = defaultdict(list)
    value_scores: defaultdict[str, list[float]] = defaultdict(list)
    for hit in column_hits:
        column_scores[str(hit["table_id"])].append(float(hit.get("score", 0)))
    for hit in value_hits:
        value_scores[str(hit["table_id"])].append(float(hit.get("score", 0)))
    by_table = {str(hit["table_id"]): dict(hit) for hit in table_hits}
    for table_id in column_scores.keys() | value_scores.keys():
        by_table.setdefault(
            table_id,
            {"kind": "table", "table_id": table_id, "column_id": "", "text": "", "score": 0.0, "sources": []},
        )
    ranked = []
    for table_id, hit in by_table.items():
        # Bound repeated values from one column so a large Profile cannot overwhelm
        # stronger schema evidence merely because it contains more indexed rows.
        strongest_columns = sorted(column_scores[table_id], reverse=True)[:3]
        strongest_values = sorted(value_scores[table_id], reverse=True)[:3]
        ranked.append({
            **hit,
            "score": float(hit.get("score", 0)) + 2 * sum(strongest_columns) + sum(strongest_values),
        })
    return sorted(ranked, key=lambda hit: (-float(hit["score"]), str(hit["table_id"])))


def lexical_evidence_is_sufficient(
    table_hits: list[dict[str, Any]],
    column_hits: list[dict[str, Any]],
    value_hits: list[dict[str, Any]],
) -> bool:
    """Use lexical-only retrieval only when it has more than a generic token hit."""
    if not table_hits or not (column_hits or value_hits):
        return False
    best_title = max((float(hit.get("title_score", 0)) for hit in table_hits), default=0.0)
    best_column = max((float(hit.get("score", 0)) for hit in column_hits), default=0.0)
    best_value = max((float(hit.get("score", 0)) for hit in value_hits), default=0.0)
    return best_title > 0 or best_column >= 2 or best_value >= 2


def diversify_value_hits(hits: list[dict[str, Any]], limit: int, per_column: int = 5) -> list[dict[str, Any]]:
    """Keep ranked value evidence while preventing one column from occupying all context."""
    counts: defaultdict[str, int] = defaultdict(int)
    selected: list[dict[str, Any]] = []
    for hit in hits:
        column_id = str(hit.get("column_id", ""))
        if counts[column_id] >= per_column:
            continue
        selected.append(hit)
        counts[column_id] += 1
        if len(selected) >= limit:
            break
    return selected


def refine_value_hits_for_tables(
    global_hits: list[dict[str, Any]], targeted_hits: list[dict[str, Any]],
    table_ids: set[str], *, limit: int = 30, per_column: int = 10,
) -> list[dict[str, Any]]:
    """Fuse the global recall with a target-table lexical pass under a bounded budget."""
    scoped_global = [hit for hit in global_hits if str(hit["table_id"]) in table_ids]
    fused = fuse_ranked_hits(scoped_global, targeted_hits, limit * 6)
    return diversify_value_hits(fused, limit=limit, per_column=per_column)
