from types import SimpleNamespace
from uuid import uuid4

import pytest

from structured_query import query_service
from structured_query.execution import ExecutionResult, QueryExecutionError
from structured_query.model_gateway import ModelGenerationError, SQLGeneration
from structured_query.sql_safety import UnsafeSQL


def test_success_path_uses_exactly_one_model_call(monkeypatch):
    calls = []
    monkeypatch.setattr(query_service, "generate_sql", lambda *args, **kwargs: calls.append(kwargs) or SQLGeneration(route="sql", sql="SELECT count(*) FROM people"))
    outcome = query_service.execute_with_one_repair("tenant-a", "人数", "schema", [], {"people"}, "postgres", lambda _: ExecutionResult(["count"], [[2]]))
    assert outcome.model_calls == 1 and len(calls) == 1
    assert outcome.result.rows == [[2]]


@pytest.mark.parametrize("rows", [[], [[0]], [[0, 3]]])
def test_valid_empty_or_zero_result_is_not_semantically_rewritten(monkeypatch, rows):
    calls = []
    monkeypatch.setattr(
        query_service, "generate_sql",
        lambda *args, **kwargs: calls.append(1) or SQLGeneration(route="sql", sql="SELECT count(*) FROM people"),
    )

    outcome = query_service.execute_with_one_repair(
        "tenant-a", "不存在的人员", "schema", [], {"people"}, "postgres",
        lambda _: ExecutionResult(["count"], rows),
    )

    assert outcome.model_calls == 1
    assert outcome.result.rows == rows


def test_retryable_execution_error_repairs_once(monkeypatch):
    generations = iter([
        SQLGeneration(route="sql", sql="SELECT missing FROM people"),
        SQLGeneration(route="sql", sql="SELECT name FROM people"),
    ])
    monkeypatch.setattr(query_service, "generate_sql", lambda *args, **kwargs: next(generations))
    executions = []
    def execute(sql):
        executions.append(sql)
        if len(executions) == 1:
            raise QueryExecutionError("invalid_reference", retryable=True)
        return ExecutionResult(["name"], [["张三"]])
    outcome = query_service.execute_with_one_repair("tenant-a", "姓名", "schema", [], {"people"}, "postgres", execute)
    assert outcome.model_calls == 2 and outcome.error_codes == ["invalid_reference"]


def test_second_failure_is_not_retried(monkeypatch):
    monkeypatch.setattr(query_service, "generate_sql", lambda *args, **kwargs: SQLGeneration(route="sql", sql="DELETE FROM people"))
    with pytest.raises(query_service.QueryAttemptsFailed, match="read_only_required"):
        query_service.execute_with_one_repair("tenant-a", "删除", "schema", [], {"people"}, "postgres", lambda _: None)


def test_invalid_model_response_is_repaired_once(monkeypatch):
    generations = iter([
        ModelGenerationError("invalid_model_response", retryable=True),
        SQLGeneration(route="sql", sql="SELECT name FROM people"),
    ])

    def generate(*args, **kwargs):
        value = next(generations)
        if isinstance(value, Exception):
            raise value
        return value

    monkeypatch.setattr(query_service, "generate_sql", generate)
    outcome = query_service.execute_with_one_repair(
        "tenant-a", "姓名", "schema", [], {"people"}, "postgres",
        lambda _: ExecutionResult(["name"], [["张三"]]),
    )
    assert outcome.model_calls == 2
    assert outcome.error_codes == ["invalid_model_response"]


def test_model_unavailable_is_not_retried(monkeypatch):
    def unavailable(*args, **kwargs):
        raise ModelGenerationError("model_unavailable", retryable=False)

    monkeypatch.setattr(query_service, "generate_sql", unavailable)
    with pytest.raises(query_service.QueryAttemptsFailed, match="model_unavailable") as failure:
        query_service.execute_with_one_repair(
            "tenant-a", "姓名", "schema", [], {"people"}, "postgres", lambda _: None
        )
    assert failure.value.model_calls == 1


def test_trace_persists_only_sql_fingerprints():
    traced = query_service.trace_sql_attempts(["SELECT * FROM people WHERE secret = '敏感值'"])
    assert traced[0].startswith("SELECT * FROM people")
    assert "WHERE secret = '?'" in traced[0]
    assert "敏感值" not in traced[0]

def test_value_evidence_is_compacted_around_question_terms():
    long_value = "前置无关内容" * 100 + "系统架构设计师" + "后置无关内容" * 100

    result = query_service._compact_evidence_values(
        [{"text": long_value}], "谁有系统架构设计师证书", per_item_chars=120, total_chars=120
    )

    assert len(result) == 1
    assert len(result[0]) <= 120
    assert "系统架构设计师" in result[0]


def test_value_evidence_prioritizes_matching_qualifier_and_marks_conflicts():
    result = query_service._compact_evidence_values(
        [
            {"text": "专业证书: 高级软件测评师（培训证）"},
            {"text": "职称专业: 软件评测师（软考）"},
        ],
        "具有软件测评师（软考）的人数",
    )

    assert result[0].startswith("[与问题限定词“软考”一致]")
    assert "不得与“软考”作为同一条件 OR 合并" in result[1]


def test_qualified_subjects_use_structural_spans_not_question_verbs():
    pairs = query_service._question_qualified_subjects(
        "请帮我核对：人员具备软件评测师[软考]资格"
    )

    assert pairs
    assert pairs[0][1] == "软考"
    assert pairs[0][0].endswith("软件评测师")


def test_qualified_subjects_ignore_mismatched_brackets():
    assert query_service._question_qualified_subjects("软件评测师（软考]") == []


def test_none_route_stops_before_sql_validation_and_execution(monkeypatch):
    monkeypatch.setattr(
        query_service, "generate_sql",
        lambda *_args, **_kwargs: SQLGeneration(route="none", sql=""),
    )

    outcome = query_service.execute_with_one_repair(
        "tenant-a", "解释质量体系", "schema", [], {"people"}, "postgres",
        lambda _sql: (_ for _ in ()).throw(AssertionError("SQL must not execute")),
    )

    assert outcome.route == "none"
    assert outcome.model_calls == 1
    assert outcome.sql == ""


def _dataset(file_name, *sheet_names):
    version_id = uuid4()
    version = SimpleNamespace(
        id=version_id,
        tables=[SimpleNamespace(sheet_name=name) for name in sheet_names],
    )
    return SimpleNamespace(
        id=uuid4(), original_file_name=file_name,
        active_version_id=version_id, versions=[version],
    )


def test_profile_metadata_scope_resolves_unique_dataset_without_phrase_rules():
    target = _dataset("数科事业部实验室相关人员资质清单202607V3.0.xlsx", "人员资质统计")
    unrelated = _dataset("软件测评相关人员资质清单202607V3.0.xlsx", "人员资质统计")

    selected, labels = query_service._match_profile_metadata_scope(
        "数科事业部人力资源清单里有多少个硕士学历的人员", [target, unrelated]
    )

    assert selected == [target]
    assert labels == [target.original_file_name]


def test_profile_metadata_scope_does_not_treat_shared_topic_as_file_scope():
    first = _dataset("软件测评相关人员资质清单V1.xlsx", "人员资质统计")
    second = _dataset("软件测评相关项目案例清单V2.xlsx", "项目统计")

    selected, labels = query_service._match_profile_metadata_scope(
        "具有软件测评师证书的人员有多少", [first, second]
    )

    assert selected == []
    assert labels == []


def test_profile_metadata_scope_can_match_a_unique_sheet_name():
    first = _dataset("book-a.xlsx", "年度人员明细")
    second = _dataset("book-b.xlsx", "项目明细")

    selected, labels = query_service._match_profile_metadata_scope(
        "请统计年度人员明细中的人数", [first, second]
    )

    assert selected == [first]
    assert labels == ["年度人员明细"]


def test_wide_schema_keeps_relevant_columns_and_bounds_first_prompt():
    version_id = uuid4()
    columns = [
        SimpleNamespace(
            id=uuid4(), ordinal=index, physical_name=f"c_{index:03d}",
            original_name=f"字段{index}", data_type="TEXT", nullable=True,
            profile={"values": []},
        )
        for index in range(1, 41)
    ]
    target = columns[-1]
    table = SimpleNamespace(
        version_id=version_id, columns=columns, row_count=10, sheet_name="宽表",
        physical_name="wide_table", profile={"primary_key": [], "foreign_keys": []},
    )
    dataset = SimpleNamespace(original_file_name="宽表.xlsx")

    context = query_service._compact_schema_context(
        [table], {version_id: dataset}, {str(target.id)}, "查询目标", "FULL", max_columns_per_table=8
    )

    assert "c_040:字段40" in context
    assert context.count("nullable=") == 8
