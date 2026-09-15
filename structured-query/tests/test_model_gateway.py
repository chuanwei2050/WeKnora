from types import SimpleNamespace

import httpx
import pytest
from openai import APIConnectionError

from structured_query import model_gateway


def test_generation_prompt_is_bound_to_selected_dialect(monkeypatch):
    captured = {}

    class Completions:
        def create(self, **kwargs):
            captured.update(kwargs)
            return SimpleNamespace(
                choices=[SimpleNamespace(message=SimpleNamespace(content='{"route":"sql","sql":"SELECT 1 FROM `hr`.`people`"}'))]
            )

    monkeypatch.setattr(model_gateway, "get_runtime_model_config", lambda _: SimpleNamespace(chat=SimpleNamespace(base_url="http://llm/v1", api_key="test", name="dynamic-model")))
    monkeypatch.setattr(model_gateway, "OpenAI", lambda **_: SimpleNamespace(chat=SimpleNamespace(completions=Completions())))
    result = model_gateway.generate_sql("tenant-a", "人数", "schema", [], dialect="mysql")
    assert result.sql == "SELECT 1 FROM `hr`.`people`"
    assert [message["role"] for message in captured["messages"]] == ["system", "user"]
    prompt = "\n".join(message["content"] for message in captured["messages"])
    assert "mysql Text-to-SQL" in prompt
    assert "不能原样发明为筛选值" in prompt
    assert "必须能从对应字段" in prompt
    assert "不得用 OR 同时覆盖用户原词、纠正词" in prompt
    assert "括号限定词首先用于业务类别和字段消歧" in prompt
    assert "不要根据问题主题预先排除结构化查询" in prompt
    assert "关键词、标题式短语或省略句" in prompt
    assert "不得仅因表达不是完整问句" in prompt
    assert "明细检索、筛选、排序/排名、分组、计算、聚合、计数或比较" in prompt
    assert "优先 route=\"sql\"，不要猜 none" in prompt
    assert "【文件/表】范围" in prompt
    assert "列出具体记录或实体" in prompt
    assert "证书名称和数量" not in prompt
    assert "分别多少人" not in prompt
    assert "主体粒度不一致" not in prompt
    assert "组织是否具有某体系" not in prompt
    assert captured["temperature"] == 0
    assert captured["response_format"]["type"] == "json_schema"
    assert captured["response_format"]["json_schema"]["strict"] is True
    assert captured["model"] == "dynamic-model"
    assert captured["max_completion_tokens"] == 512
    assert captured["extra_body"] == {"chat_template_kwargs": {"enable_thinking": False}}


def test_force_sql_skips_route_choice(monkeypatch):
    captured = {}

    class Completions:
        def create(self, **kwargs):
            captured.update(kwargs)
            return SimpleNamespace(
                choices=[SimpleNamespace(message=SimpleNamespace(content='{"sql":"SELECT count(*) FROM people"}'))]
            )

    monkeypatch.setattr(model_gateway, "get_runtime_model_config", lambda _: SimpleNamespace(chat=SimpleNamespace(base_url="http://llm/v1", api_key="test", name="dynamic-model")))
    monkeypatch.setattr(model_gateway, "OpenAI", lambda **_: SimpleNamespace(chat=SimpleNamespace(completions=Completions())))

    result = model_gateway.generate_sql("tenant-a", "人数", "schema", [], force_sql=True)
    assert result == model_gateway.SQLGeneration(route="sql", sql="SELECT count(*) FROM people")
    assert "不要再判断是否跳过 SQL" in captured["messages"][0]["content"]
    assert captured["response_format"]["json_schema"]["name"] == "forced_sql_generation"
    assert "route" not in captured["response_format"]["json_schema"]["schema"]["properties"]


def test_connection_error_is_retried_once(monkeypatch):
    calls = {"n": 0}

    class Completions:
        def create(self, **kwargs):
            calls["n"] += 1
            if calls["n"] == 1:
                raise APIConnectionError(request=httpx.Request("POST", "http://llm/v1"))
            return SimpleNamespace(
                choices=[SimpleNamespace(message=SimpleNamespace(content='{"route":"sql","sql":"SELECT 1"}'))]
            )

    monkeypatch.setattr(model_gateway, "get_runtime_model_config", lambda _: SimpleNamespace(chat=SimpleNamespace(base_url="http://llm/v1", api_key="test", name="dynamic-model")))
    monkeypatch.setattr(model_gateway, "OpenAI", lambda **_: SimpleNamespace(chat=SimpleNamespace(completions=Completions())))

    result = model_gateway.generate_sql("tenant-a", "人数", "schema", [])
    assert result.sql == "SELECT 1"
    assert calls["n"] == 2


def test_resolved_dataset_scope_is_authoritative_for_routing(monkeypatch):
    captured = {}

    class Completions:
        def create(self, **kwargs):
            captured.update(kwargs)
            return SimpleNamespace(
                choices=[SimpleNamespace(message=SimpleNamespace(content='{"route":"none","sql":""}'))]
            )

    monkeypatch.setattr(model_gateway, "get_runtime_model_config", lambda _: SimpleNamespace(chat=SimpleNamespace(base_url="http://llm/v1", api_key="test", name="dynamic-model")))
    monkeypatch.setattr(model_gateway, "OpenAI", lambda **_: SimpleNamespace(chat=SimpleNamespace(completions=Completions())))

    model_gateway.generate_sql(
        "tenant-a", "某清单中符合条件的记录数", "schema", [],
        dataset_scope=["完整数据文件.xlsx"],
    )

    user_prompt = captured["messages"][1]["content"]
    assert "视为选表的权威结果" in user_prompt
    assert "范围简称" in user_prompt
    assert "禁止转成 WHERE 条件" in user_prompt


def test_dynamic_config_failure_has_stable_error(monkeypatch):
    monkeypatch.setattr(
        model_gateway,
        "get_runtime_model_config",
        lambda _: (_ for _ in ()).throw(httpx.ConnectError("internal-host")),
    )
    with pytest.raises(model_gateway.ModelGenerationError) as failure:
        model_gateway.generate_sql("tenant-a", "人数", "schema", [])
    assert failure.value.code == "model_config_or_request_failed"
    assert failure.value.retryable is False
