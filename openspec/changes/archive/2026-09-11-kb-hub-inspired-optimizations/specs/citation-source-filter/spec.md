## ADDED Requirements

### Requirement: 系统可按模型实际引用过滤来源列表
系统 MUST 支持引用过滤模式 `off`、`cited_or_all`、`cited_only`。模式为 `off` 时，系统 MUST 保持现有「返回全部候选来源」行为。模式非 `off` 时，系统 MUST 在持久化与推送 references 之前，根据助手回答中的引用标记过滤来源。

#### Scenario: 关闭过滤时行为不变
- **WHEN** 引用过滤模式为 `off` 且问答产生多条召回来源
- **THEN** 系统返回的 references 与现网一致，包含全部合并后的候选来源

#### Scenario: cited_or_all 在有引用时只保留被引用项
- **WHEN** 模式为 `cited_or_all` 且回答中包含有效引用编号或 chunk 引用标记
- **THEN** 系统只返回被引用标记命中的来源，且顺序稳定可复现

#### Scenario: cited_or_all 在无引用时回退全量
- **WHEN** 模式为 `cited_or_all` 且回答中不包含任何可解析引用
- **THEN** 系统回退返回全部候选来源，MUST NOT 返回空 references（候选非空时）

#### Scenario: cited_only 在无引用时返回空列表
- **WHEN** 模式为 `cited_only` 且回答中不包含任何可解析引用
- **THEN** 系统返回空 references 列表

### Requirement: Knowledge QA 使用编号引用约定
在引用过滤启用时，Knowledge QA 上下文 MUST 为每条证据提供稳定全局序号，并指示模型在需要处使用 `[n]` 标注；过滤器 MUST 将 `[n]` 映射到对应候选，忽略越界或非法编号。

#### Scenario: 合法编号映射
- **WHEN** 候选有 3 条且回答包含 `[1]` 与 `[3]`
- **THEN** 过滤结果恰好包含第 1 与第 3 条候选

#### Scenario: 非法编号被忽略
- **WHEN** 回答包含 `[0]`、`[99]` 或非数字括号内容
- **THEN** 系统忽略这些标记且不因此失败整次问答

### Requirement: Agent 路径按 chunk 级引用过滤
Agent 路径在过滤启用时 MUST 解析答案中的知识库引用标记（含 `chunk_id`），并据此过滤 `KnowledgeRefs`；未启用过滤时 MUST 保持现有全量收集行为。

#### Scenario: Agent 按 chunk_id 过滤
- **WHEN** 模式非 `off` 且最终答案引用了两个不同 chunk_id
- **THEN** 推送与落库的 KnowledgeRefs 仅包含这两个 chunk 对应条目
