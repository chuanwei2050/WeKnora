from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True)
class EvaluationFailure:
    code: str
    detail: str


def evaluate_response(case: dict[str, Any], response: dict[str, Any]) -> list[EvaluationFailure]:
    """Compare executed results and latency metadata without judging SQL text formatting."""
    failures: list[EvaluationFailure] = []
    expected_route = case.get("expected_route", "sql")
    if response.get("route") != expected_route:
        failures.append(EvaluationFailure("route_mismatch", f"expected={expected_route}"))
        return failures
    if "expected_rows" in case and response.get("rows") != case["expected_rows"]:
        failures.append(EvaluationFailure("rows_mismatch", f"expected={case['expected_rows']!r}"))
    if "expected_source_files" in case:
        actual = {source.get("original_file_name") for source in response.get("sources", [])}
        expected = set(case["expected_source_files"])
        if actual != expected:
            failures.append(EvaluationFailure("source_mismatch", f"expected={sorted(expected)!r}"))
    max_calls = int(case.get("max_model_calls", 2))
    if int(response.get("model_calls", 0)) > max_calls:
        failures.append(EvaluationFailure("model_calls_exceeded", f"max={max_calls}"))
    max_total_ms = case.get("max_total_ms")
    actual_total_ms = int(response.get("timings", {}).get("total_ms", 0))
    if max_total_ms is not None and actual_total_ms > int(max_total_ms):
        failures.append(
            EvaluationFailure("latency_exceeded", f"actual={actual_total_ms},max={max_total_ms}")
        )
    return failures
