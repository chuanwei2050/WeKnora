from structured_query.evaluation import evaluate_response


def test_execution_evaluator_checks_result_source_calls_and_latency():
    case = {
        "expected_rows": [[42, 3, 29]],
        "expected_source_files": ["qualifications.xlsx"],
        "max_model_calls": 1,
        "max_total_ms": 5_000,
    }
    response = {
        "route": "sql",
        "rows": [[43, 3, 29]],
        "sources": [{"original_file_name": "training.xlsx"}],
        "model_calls": 2,
        "timings": {"total_ms": 6_000},
    }

    assert {failure.code for failure in evaluate_response(case, response)} == {
        "rows_mismatch", "source_mismatch", "model_calls_exceeded", "latency_exceeded"
    }


def test_execution_evaluator_accepts_none_route_without_sql_rows():
    assert evaluate_response(
        {"expected_route": "none", "max_model_calls": 1},
        {"route": "none", "model_calls": 1, "timings": {"total_ms": 200}},
    ) == []
