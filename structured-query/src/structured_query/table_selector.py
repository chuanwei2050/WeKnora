from __future__ import annotations

from typing import Any

from .database import DataTable


def shortlist_tables(hits: list[dict[str, Any]], tables: dict[str, DataTable], max_tables: int = 3) -> list[DataTable]:
    """Return a small ranked schema shortlist; the SQL model still chooses 1..3 tables."""
    selected: list[DataTable] = []
    seen: set[str] = set()
    for hit in hits:
        table_id = str(hit["table_id"])
        if table_id in seen or table_id not in tables:
            continue
        selected.append(tables[table_id])
        seen.add(table_id)
        if len(selected) >= max_tables:
            break
    return selected


def select_minimal_tables(hits: list[dict[str, Any]], tables: dict[str, DataTable], max_tables: int = 3) -> list[DataTable]:
    if not hits:
        return []
    ordered = [tables[str(hit["table_id"])] for hit in hits if str(hit["table_id"]) in tables]
    if not ordered:
        return []
    selected = [ordered[0]]
    top_score = float(hits[0].get("score", 0)) or 1.0
    for hit, candidate in zip(hits[1:], ordered[1:]):
        score = float(hit.get("score", 0))
        if len(selected) >= max_tables or score < top_score * 0.85:
            break
        # Relevance identifies the best starting table; only persisted relationships
        # are authoritative enough to enlarge the executable SQL scope.
        if _has_confirmed_relation(selected, candidate):
            selected.append(candidate)
    return selected


def select_schema_candidates(
    hits: list[dict[str, Any]], tables: dict[str, DataTable], max_tables: int = 3
) -> list[DataTable]:
    """Select one table by default, plus a near-tied sibling sheet if ambiguous."""
    selected = select_minimal_tables(hits, tables, max_tables=max_tables)
    if not selected or len(selected) >= max_tables:
        return selected
    top = selected[0]
    top_score = float(hits[0].get("score", 0)) or 1.0
    selected_names = {table.physical_name for table in selected}
    for hit in hits[1:]:
        candidate = tables.get(str(hit["table_id"]))
        if candidate is None or candidate.physical_name in selected_names:
            continue
        # Sibling worksheets can be alternative representations (detail versus
        # summary). Only expose a second schema when ranking is genuinely
        # ambiguous; unrelated datasets never expand through this path.
        if candidate.version_id != top.version_id or float(hit.get("score", 0)) < top_score * 0.95:
            continue
        selected.append(candidate)
        break
    return selected


def _has_confirmed_relation(selected: list[DataTable], candidate: DataTable) -> bool:
    candidate_names = {candidate.physical_name, f"{candidate.physical_schema}.{candidate.physical_name}"}
    selected_names = {table.physical_name for table in selected} | {
        f"{table.physical_schema}.{table.physical_name}" for table in selected
    }
    for table in selected:
        for relation in table.profile.get("foreign_keys", []):
            referred = relation.get("referred_table", "")
            schema = relation.get("referred_schema")
            if referred in candidate_names or (schema and f"{schema}.{referred}" in candidate_names):
                return True
    for relation in candidate.profile.get("foreign_keys", []):
        referred = relation.get("referred_table", "")
        schema = relation.get("referred_schema")
        if referred in selected_names or (schema and f"{schema}.{referred}" in selected_names):
            return True
    return False


def confirmed_join_limit(tables: list[DataTable]) -> int:
    """Return the connected, Profile-backed join width allowed for a shortlist."""
    if not tables:
        return 1
    connected = [tables[0]]
    for candidate in tables[1:]:
        if _has_confirmed_relation(connected, candidate):
            connected.append(candidate)
    return len(connected)
