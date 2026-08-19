# verified-multi-agent-answering Specification

## Purpose
TBD - created by archiving change add-verified-multi-agent-answering. Update Purpose after archive.
## Requirements
### Requirement: 反思补检索必须获取新证据
当验证报告要求补充检索时，系统 MUST 根据结构化检索计划重新调用受限检索服务；不得把首轮 `EvidenceBundle` 原样返回作为补检索结果。新增证据 MUST 与原证据按稳定来源 ID 去重，并在重新生成草稿后再次验证。

#### Scenario: 补检索获得新证据
- **WHEN** 首轮验证判定证据不足且反思预算可用
- **THEN** 系统执行一次受原权限和总预算约束的补检索
- **AND** 新证据进入新的 evidence bundle，草稿重生成并重新验证

#### Scenario: 补检索没有新证据
- **WHEN** 补检索未返回任何未见过的有效证据
- **THEN** 系统记录 `no_new_evidence`
- **AND** 不得仅通过改写原答案将其标记为验证通过

### Requirement: 验证执行路径可区分
系统 MUST 在追踪和验收结果中区分真实 `verified_agent` 执行与普通 RAG 完成后的验证后处理；普通 RAG 验证不得伪装成 Agent/ReAct 路由。

#### Scenario: 普通 RAG 启用验证
- **WHEN** 普通 RAG 完成后触发验证协调器
- **THEN** 执行摘要标记为 `verified_rag_postcheck`
- **AND** 不记录 Agent 工具循环或 `verified_agent` 路由

