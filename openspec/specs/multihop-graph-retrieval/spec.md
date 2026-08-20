# multihop-graph-retrieval Specification

## Purpose
TBD - created by archiving change add-multihop-graph-retrieval. Update Purpose after archive.
## Requirements
### Requirement: 治理版本图谱隔离
启用知识治理时，图谱数据 MUST 按知识版本写入隔离 namespace；`draft`、`pending_review` 和未发布的 `indexing` 版本不得写入生产 active namespace。未启用治理时可以继续使用兼容的 knowledge 级 namespace。

#### Scenario: 待审核版本抽取关系
- **WHEN** 一个启用治理的知识产生 `pending_review` 版本并触发关系抽取
- **THEN** 抽取结果只能写入该版本的 staging namespace
- **AND** 当前生产图谱的节点、边和路径保持不变

### Requirement: 图谱 namespace 与版本原子切换
系统 MUST 仅在新版本图谱、向量和关键词索引均成功构建并通过证据版本校验后，同时切换图谱 active namespace 与知识的 `current_version_id`；任一索引失败时 MUST 保留旧生产版本。

#### Scenario: 图谱构建失败
- **WHEN** 新版本的图谱构建失败但向量索引已完成
- **THEN** 系统不得切换图谱 namespace 或 `current_version_id`
- **AND** 旧版本继续提供检索和图谱服务

### Requirement: GraphContext 只包含当前有效版本
图谱查询结果 MUST 在转换为 `GraphData`、`GraphContext` 或引用之前过滤掉非当前、未发布或已过期版本的节点、边、路径和证据；被过滤的路径不得参与排序或回答。

#### Scenario: 路径包含待发布证据
- **WHEN** 一条候选路径同时包含当前版本和待发布版本的关系证据
- **THEN** 系统移除待发布证据及其依赖路径
- **AND** 不得将该路径的关系用于回答推理

### Requirement: 图谱检索关系约束来自正式 Schema
Agent 图谱检索 MUST 从知识库正式关系 Schema `ExtractConfig.Tags` 获取允许关系类型，MUST NOT 从 few-shot `Relations` 获取。通用抽取的关系 Schema 为空时 MUST 表示不限制关系类型。

#### Scenario: 自定义 Schema 检索
- **WHEN** 知识库正式关系类型包含 `uses`，few-shot 关系为空
- **THEN** Agent 图谱查询 MUST 允许检索 `uses`

#### Scenario: 通用抽取检索
- **WHEN** 通用抽取产生动态图关系且正式关系 Schema 为空
- **THEN** 图谱查询 MUST 能检索实际存在的关系，不得因 few-shot 为空禁用图检索

