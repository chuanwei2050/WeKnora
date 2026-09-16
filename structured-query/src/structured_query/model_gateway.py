import json
import httpx
from typing import Literal
from openai import APIConnectionError, APIStatusError, APITimeoutError, OpenAI
from pydantic import BaseModel, ConfigDict, ValidationError

from .model_config import get_runtime_model_config
from .config import get_settings
from . import prompts as llm_prompts


class SQLGeneration(BaseModel):
    model_config = ConfigDict(extra="forbid")

    route: Literal["sql", "none"]
    sql: str


class ForcedSQLGeneration(BaseModel):
    """Schema used when the caller already decided the question needs SQL."""

    model_config = ConfigDict(extra="forbid")

    sql: str


class ModelGenerationError(RuntimeError):
    def __init__(self, code: str, *, retryable: bool) -> None:
        super().__init__(code)
        self.code = code
        self.retryable = retryable


def generate_sql(
    tenant_id: str,
    question: str,
    schema_context: str,
    value_evidence: list[str],
    repair: str | None = None,
    dialect: str = "postgres",
    dataset_scope: list[str] | None = None,
    force_sql: bool = False,
) -> SQLGeneration:
    if force_sql:
        response_schema = ForcedSQLGeneration.model_json_schema()
        schema_name = "forced_sql_generation"
    else:
        response_schema = SQLGeneration.model_json_schema()
        schema_name = "sql_generation"

    full_prompt = llm_prompts.build_sql_generation_prompt(
        dialect=dialect,
        question=question,
        schema_context=schema_context,
        value_evidence=value_evidence,
        repair=repair,
        dataset_scope=dataset_scope,
        force_sql=force_sql,
    )
    policy, user_content = llm_prompts.split_system_user(full_prompt)
    response = None
    for connection_attempt in range(2):
        try:
            model = get_runtime_model_config(tenant_id).chat
            response = OpenAI(
                base_url=model.base_url,
                api_key=model.api_key or "not-needed",
                timeout=get_settings().model_request_timeout_seconds,
                max_retries=0,
            ).chat.completions.create(
                model=model.name,
                temperature=0,
                max_completion_tokens=get_settings().sql_max_completion_tokens,
                extra_body={"chat_template_kwargs": {"enable_thinking": False}},
                response_format={"type": "json_schema", "json_schema": {"name": schema_name, "strict": True, "schema": response_schema}},
                messages=[
                    {"role": "system", "content": policy},
                    {"role": "user", "content": user_content},
                ],
            )
            break
        except APITimeoutError as error:
            raise ModelGenerationError("model_timeout", retryable=False) from error
        except APIConnectionError as error:
            if connection_attempt == 0:
                continue
            raise ModelGenerationError("model_unavailable", retryable=False) from error
        except (APIStatusError, httpx.HTTPError, ValueError, KeyError) as error:
            raise ModelGenerationError("model_config_or_request_failed", retryable=False) from error
    try:
        content = response.choices[0].message.content or "{}"
        if force_sql:
            return SQLGeneration(route="sql", sql=ForcedSQLGeneration.model_validate_json(content).sql)
        return SQLGeneration.model_validate_json(content)
    except (AttributeError, IndexError, ValidationError) as error:
        raise ModelGenerationError("invalid_model_response", retryable=True) from error
