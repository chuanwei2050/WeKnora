import json
import httpx
from typing import Literal
from openai import APIConnectionError, APIStatusError, APITimeoutError, OpenAI
from pydantic import BaseModel, ConfigDict, ValidationError

from .model_config import get_runtime_model_config
from .config import get_settings


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
    repair_context = f"\n前一次 SQL 及错误：\n{repair}" if repair else ""
    scope_context = (
        "\n【Profile 元数据命中的数据集范围】"
        + json.dumps(dataset_scope, ensure_ascii=False)
        + "（该范围已由系统根据持久化元数据唯一解析，视为选表的权威结果；"
        "只用于选文件/工作表，禁止转成 WHERE 条件，也不得因用户的范围简称与"
        "文件全名不完全一致而返回 route=none。请仅判断问题要求的记录操作能否由 Schema 回答。）"
        if dataset_scope
        else ""
    )
    if force_sql:
        route_policy = f"""你是 {dialect} Text-to-SQL 生成器。
系统已确认该问题需要对结构化记录做明细检索、筛选、排序、分组、比较、计算或聚合，且 Schema 足以支持。
不要再判断是否跳过 SQL；必须输出一条只读 SQL。
"""
        response_schema = ForcedSQLGeneration.model_json_schema()
        schema_name = "forced_sql_generation"
    else:
        # Route criteria mirror the former RAG needs_table_query judgment:
        # classify by required record operation, not by topic vocabulary.
        route_policy = f"""你是结构化查询路由器和 {dialect} Text-to-SQL 生成器。
先独立判断问题是否需要对结构化记录做明细检索、筛选、排序/排名、分组、计算、聚合、计数或比较；这不限于统计。只要是在问满足一个或多个条件的记录有多少、分别多少、列出谁/哪些，就属于结构化操作。
需要且给出的 Schema 足以支持时返回 route="sql" 和一条只读 SQL。
仅当问题可用相关段落回答（概念解释、建议、叙事摘要，或没有记录处理需求的普通事实问答），或 Profile/Schema 证据不足以支持该记录操作时，返回 route="none"、sql=""，立即结束结构化链路。
仅提及文件名或主题本身不要求 SQL。不要根据问题主题预先排除结构化查询。不确定时，若 Schema 有对应字段或候选值能支持记录操作，优先 route="sql"，不要猜 none。
用户输入可能是关键词、标题式短语或省略句，不要求具备疑问词、谓语或问号；不得仅因表达不是完整问句而返回 route="none"。
不得因数据集名称与用户简称不完全一致、问题使用字段近义表达或答案需要聚合而返回 route="none"。
"""
        response_schema = SQLGeneration.model_json_schema()
        schema_name = "sql_generation"
    prompt = f"""{route_policy}route="sql" 时只可使用给出的物理表名和物理列名。
输出列如需别名，只能使用 metric_1、metric_2、name_1 这类 ASCII 别名；禁止中文别名、全角逗号和其他全角 SQL 标点。
M-Schema 字段格式为“(物理列名:原始语义名, 类型, ...)”：SQL 中必须逐字使用冒号左侧的物理列名；冒号右侧只用于理解，绝不能作为 SQL 标识符。
所有筛选值必须逐字采用 Schema Examples 或真实候选值中的数据库值；用户问题中的近义词、错别字或词序变体只能用于匹配，不能原样发明为筛选值。字符串可能是包含多项内容的长文本时使用 ILIKE，并把真实值作为通配内容。
当问题带有括号内的限定词时，优先选择 Examples/候选值同时满足主体与限定词的字段和值，不得改用只有主体近似的其他字段。
括号限定词首先用于业务类别和字段消歧，不得自动拼入 ILIKE 字面值；只有目标筛选字段的同一条候选值连续包含完整主体和限定词时才可整体复制，否则应使用该字段候选值支持的主体标准词，并保留限定词用于排除近似类别。
当主体词疑似存在错别字、词序差异或简称，且真实候选值中存在多个近似但类别限定不同的名称时，只能选择与用户限定词和完整业务名称最一致的一个标准值；不得用 OR 同时覆盖用户原词、纠正词及名称相近的其他类别。只有用户明确把这些类别分别列为查询对象，或 Profile 明确声明它们等价时，才可分别使用。
用户的主体词和限定词不一定在库内连续书写；候选值显示它们被标点、编号或其他文字分开时，必须对同一字段使用多个 AND ILIKE 子串条件，不得把用户原话拼成数据库中不存在的连续字面值。
输出前逐个检查字符串筛选条件：去掉 `%`、`_` 通配符后的每个连续片段，必须能从对应字段的某条 Schema Example 或真实候选值中连续复制；括号中的解释、类别或简称也不能擅自拼接到主体值后。
若前一次错误为 unsupported_value_literal，表示 WHERE/HAVING 的某个连续字面值未出现在 Schema 或真实候选值中；必须换成候选值支持的较短子串，必要时用多个 AND ILIKE 表达组合条件。
只允许添加问题中明确表达、且候选值或 Schema 能支持的筛选条件；描述数据范围的数据集名、工作表名、组织名和命名空间不是行级筛选条件，除非问题明确要求在对应字段内过滤。
用户用“……里/内/中的”指向数据集或工作表时，优先视为【文件/表】范围，不得据此添加组织等行级条件。问题未限定子集且多个候选数据集语义相近时，优先选择覆盖范围更广、行数更多的数据集；不要 UNION 重复版本或重复文件。
问题要求按多个条件分别聚合时，为每个条件独立选择最匹配的字段和值；不能因为部分条件位于同一字段，就把其余条件强行放进该字段。同一条 SELECT 里的多个 metric_N / CASE 聚合必须使用互不相同的筛选条件，禁止把某一个指标的 ILIKE/等值条件复制到另一个指标。问题要求列出具体记录或实体时，返回能识别这些实体的字段，而不是只返回汇总指标。
只生成一条只读 SELECT。Schema Linking 与 SQL 生成在本次调用内一并完成：默认只选一张表；只有问题确实需要关联或合并互补数据时才使用两到三张，最多三张表。
SQL 别名必须使用 ASCII 标识符；禁止生成中文别名。多个 SELECT 项之间必须使用半角英文逗号。
不要生成图表、解释、Markdown 或答案，只返回 JSON。

【Schema】
{schema_context}

【真实候选值】
{json.dumps(value_evidence, ensure_ascii=False)}

【问题】
{question}{scope_context}{repair_context}
"""
    policy, query_context = prompt.split("\n【Schema】\n", maxsplit=1)
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
                    {"role": "user", "content": f"【Schema】\n{query_context}"},
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
