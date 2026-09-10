from dataclasses import asdict, dataclass, field
from typing import Any

import pandas as pd


@dataclass(frozen=True)
class ValueProfile:
    value: str
    frequency: int


@dataclass(frozen=True)
class ColumnProfile:
    physical_name: str
    original_name: str
    data_type: str
    nullable: bool
    null_count: int
    distinct_count: int
    values: list[ValueProfile] = field(default_factory=list)
    minimum: str | None = None
    maximum: str | None = None


def profile_frame(
    frame: pd.DataFrame,
    mapping: dict[str, str],
    low_cardinality_limit: int,
    high_cardinality_sample: int,
) -> list[ColumnProfile]:
    profiles: list[ColumnProfile] = []
    for physical_name in frame.columns:
        series = frame[physical_name]
        non_null = series.dropna()
        values: list[ValueProfile] = []
        minimum: str | None = None
        maximum: str | None = None
        if pd.api.types.is_numeric_dtype(series) or pd.api.types.is_datetime64_any_dtype(series):
            distinct_count = int(non_null.nunique(dropna=True))
            if not non_null.empty:
                minimum, maximum = str(non_null.min()), str(non_null.max())
        else:
            counts = non_null.astype(str).value_counts(dropna=True)
            distinct_count = len(counts)
            selected = _select_value_counts(
                counts,
                low_cardinality_limit if distinct_count <= low_cardinality_limit else high_cardinality_sample,
                complete=distinct_count <= low_cardinality_limit,
            )
            values = [ValueProfile(value=str(value), frequency=int(count)) for value, count in selected]
        profiles.append(
            ColumnProfile(
                physical_name=physical_name,
                original_name=mapping[physical_name],
                data_type=str(series.dtype),
                nullable=bool(series.isna().any()),
                null_count=int(series.isna().sum()),
                distinct_count=distinct_count,
                values=values,
                minimum=minimum,
                maximum=maximum,
            )
        )
    return profiles


def _select_value_counts(
    counts: pd.Series,
    limit: int,
    *,
    complete: bool,
) -> list[tuple[object, int]]:
    """Keep all low-cardinality values; diversify bounded high-cardinality samples."""
    items = list(counts.items())
    if complete or len(items) <= limit:
        return items
    frequent_count = max(1, limit // 2)
    selected = items[:frequent_count]
    remainder = items[frequent_count:]
    slots = limit - len(selected)
    if slots <= 0 or not remainder:
        return selected
    # Even spacing avoids profiling only the first rows when most values have
    # frequency one, while the first half still preserves common business values.
    positions = {
        min(len(remainder) - 1, int(index * len(remainder) / slots))
        for index in range(slots)
    }
    selected.extend(remainder[position] for position in sorted(positions))
    return selected[:limit]


def to_mschema_context(database: str, table: str, sheet_name: str, columns: list[ColumnProfile]) -> str:
    lines = [f"【DB_ID】 {database}", f"# Table: {table}, {sheet_name}", "[" ]
    for column in columns:
        examples = ", ".join(value.value for value in column.values[:3])
        suffix = f", Examples: [{examples}]" if examples else ""
        lines.append(
            f"  ({column.physical_name}:{column.original_name}, {column.data_type}, "
            f"nullable={str(column.nullable).lower()}{suffix})"
        )
    lines.append("]")
    return "\n".join(lines)


def profile_to_dict(profile: ColumnProfile) -> dict[str, Any]:
    return asdict(profile)
