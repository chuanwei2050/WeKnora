## ADDED Requirements

### Requirement: 图谱检索关系约束来自正式 Schema
Agent 图谱检索 MUST 从知识库正式关系 Schema `ExtractConfig.Tags` 获取允许关系类型，MUST NOT 从 few-shot `Relations` 获取。通用抽取的关系 Schema 为空时 MUST 表示不限制关系类型。

#### Scenario: 自定义 Schema 检索
- **WHEN** 知识库正式关系类型包含 `uses`，few-shot 关系为空
- **THEN** Agent 图谱查询 MUST 允许检索 `uses`

#### Scenario: 通用抽取检索
- **WHEN** 通用抽取产生动态图关系且正式关系 Schema 为空
- **THEN** 图谱查询 MUST 能检索实际存在的关系，不得因 few-shot 为空禁用图检索
