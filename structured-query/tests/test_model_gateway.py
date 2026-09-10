from types import SimpleNamespace

import httpx
import pytest

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
    prompt = captured["messages"][0]["content"]
    assert "mysql Text-to-SQL" in prompt
    assert "不能原样发明为筛选值" in prompt
    assert "必须能从对应字段" in prompt
    assert "不得用 OR 同时覆盖用户原词、纠正词" in prompt
    assert "括号限定词首先用于业务类别和字段消歧" in prompt
    assert "不要根据问题主题预先排除结构化查询" in prompt
    assert "组织是否具有某体系" not in prompt
    assert captured["temperature"] == 0
    assert captured["response_format"]["type"] == "json_schema"
    assert captured["response_format"]["json_schema"]["strict"] is True
    assert captured["model"] == "dynamic-model"
    assert captured["max_completion_tokens"] == 512
    assert captured["extra_body"] == {"chat_template_kwargs": {"enable_thinking": False}}


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
