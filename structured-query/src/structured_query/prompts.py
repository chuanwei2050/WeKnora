"""LLM prompt builders for structured-query Text-to-SQL.

Keep routing/SQL safety constraints; prefer short checklists over essay rules.
Tests assert a few key policy phrases — keep those phrases intact.
"""

from __future__ import annotations

import json


def build_dataset_scope_context(dataset_scope: list[str] | None) -> str:
    if not dataset_scope:
        return ""
    return (
        "\n【Profile 元数据命中的数据集范围】"
        + json.dumps(dataset_scope, ensure_ascii=False)
        + "（该范围已由系统根据持久化元数据唯一解析，视为选表的权威结果；"
        "只用于选文件/工作表，禁止转成 WHERE 条件，也不得因用户的范围简称与"
        "文件全名不完全一致而返回 route=none。请仅判断问题要求的记录操作能否由 Schema 回答。）"
    )


def build_repair_context(repair: str | None) -> str:
    return f"\n前一次 SQL 及错误：\n{repair}" if repair else ""


def build_forced_sql_route_policy(dialect: str) -> str:
    return f"""你是 {dialect} Text-to-SQL 生成器。
系统已确认需要结构化记录操作且 Schema 足够。不要再判断是否跳过 SQL；必须输出一条只读 SQL。
"""


def build_routed_sql_route_policy(dialect: str) -> str:
    return f"""你是结构化查询路由器和 {dialect} Text-to-SQL 生成器。

何时 route="sql"：问题需要对结构化记录做明细检索、筛选、排序/排名、分组、计算、聚合、计数或比较，且 Schema/候选值足以支持。
何时 route="none"：概念解释/建议/叙事摘要/无记录操作的事实问答，或证据不足以支持该记录操作。仅提文件名或主题 ≠ 需要 SQL。

规则：
- 不要根据问题主题预先排除结构化查询。不确定且 Schema/候选值能支持时，优先 route="sql"，不要猜 none。
- 用户输入可能是关键词、标题式短语或省略句；不得仅因表达不是完整问句而返回 route="none"。
- 不得因数据集简称不一致、字段近义或需要聚合而返回 route="none"。
"""


# Checklist form — same constraints, far less prose.
SQL_GENERATION_RULES = """SQL 约束（route="sql" 时）：
1. 只用 Schema 给出的物理表名/列名。M-Schema “(物理列名:原始语义名, …)” 只用冒号左侧物理名。
2. 别名仅 ASCII（metric_1/name_1）；禁止中文别名与全角 SQL 标点；SELECT 项用半角逗号。
3. 筛选值必须来自 Schema Examples 或真实候选值；近义/错别字/词序变体只用于匹配，不能原样发明为筛选值。长文本用 ILIKE。
4. 括号限定词首先用于业务类别和字段消歧，不得自动拼入 ILIKE，也不得变成额外的 AND ILIKE 去收窄已匹配主体标准名的行；仅当同一候选值连续含主体+限定词才可整体复制。
5. 多近似类别时只选与限定词+完整业务名最一致的一个；不得用 OR 同时覆盖用户原词、纠正词及相近其他类别（除非用户/Profile 明确等价）。
6. 仅当 Profile 证明同一字段内主体与限定词被隔开、且两者都必须命中才能区分目标类别时，才用多个 AND ILIKE；输出前检查：去掉 `%`/`_` 后每个连续片段必须能从对应字段的某条 Example/候选值中连续复制。unsupported_value_literal → 换更短候选子串。
7. 只加问题明确要求且证据支持的行级条件；数据集/工作表/组织名默认不是 WHERE。“……里/内/中的”优先视为【文件/表】范围。多相似数据集选覆盖更广者，勿 UNION 重复版。
8. 多条件分别聚合时各自选字段/值；同 SELECT 的 metric_N/CASE 必须使用互不相同的筛选条件。列出具体记录或实体时返回实体识别字段，勿只返回汇总指标。
9. 一条只读 SELECT；默认单表，确需关联最多三表。只返回 JSON，无解释/Markdown。
"""


def build_sql_generation_prompt(
    *,
    dialect: str,
    question: str,
    schema_context: str,
    value_evidence: list[str],
    repair: str | None,
    dataset_scope: list[str] | None,
    force_sql: bool,
) -> str:
    """Return the full prompt (policy + schema user payload) before system/user split."""
    if force_sql:
        route_policy = build_forced_sql_route_policy(dialect)
    else:
        route_policy = build_routed_sql_route_policy(dialect)
    return (
        f"{route_policy}{SQL_GENERATION_RULES}\n"
        f"【Schema】\n{schema_context}\n\n"
        f"【真实候选值】\n{json.dumps(value_evidence, ensure_ascii=False)}\n\n"
        f"【问题】\n{question}"
        f"{build_dataset_scope_context(dataset_scope)}"
        f"{build_repair_context(repair)}\n"
    )


def split_system_user(prompt: str) -> tuple[str, str]:
    """Split full prompt into system policy and user schema context."""
    policy, query_context = prompt.split("\n【Schema】\n", maxsplit=1)
    return policy, f"【Schema】\n{query_context}"
