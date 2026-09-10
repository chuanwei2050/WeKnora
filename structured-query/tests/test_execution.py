from types import SimpleNamespace

import pytest

from structured_query import execution
from structured_query.execution import QueryExecutionError, _plan_root, bounded_result, classify_database_error


def test_extracts_postgres_json_plan_root():
    assert _plan_root([{"Plan": {"Total Cost": 12.5, "Plan Rows": 3}}]) == {
        "Total Cost": 12.5,
        "Plan Rows": 3,
    }


def test_classifies_only_reference_errors_as_repairable():
    missing_column = SimpleNamespace(orig=SimpleNamespace(sqlstate="42703"))
    timeout = SimpleNamespace(orig=SimpleNamespace(sqlstate="57014"))
    repairable = classify_database_error(missing_column)
    terminal = classify_database_error(timeout)
    assert isinstance(repairable, QueryExecutionError) and repairable.retryable is True
    assert repairable.code == "invalid_reference"
    assert terminal.retryable is False and terminal.code == "query_timeout"


def test_classifies_mysql_reference_and_timeout_errors():
    missing_column = SimpleNamespace(orig=SimpleNamespace(args=(1054, "unknown column secret")))
    timeout = SimpleNamespace(orig=SimpleNamespace(args=(3024, "timeout at internal-host")))
    assert classify_database_error(missing_column).code == "invalid_reference"
    assert classify_database_error(missing_column).retryable is True
    assert classify_database_error(timeout).code == "query_timeout"
    assert classify_database_error(timeout).retryable is False


def test_result_payload_and_cell_sizes_are_bounded(monkeypatch):
    monkeypatch.setattr(execution, "get_settings", lambda: SimpleNamespace(
        max_result_cell_bytes=4, max_result_bytes=8,
    ))
    with pytest.raises(QueryExecutionError, match="result_cell_too_large"):
        bounded_result(["c"], [["12345"]])
    with pytest.raises(QueryExecutionError, match="result_too_large"):
        bounded_result(["c"], [["1234"], ["5678"]])
