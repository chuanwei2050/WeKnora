# software-testing-knowledge-governance Specification

## Purpose
TBD - created by archiving change add-software-testing-knowledge-governance. Update Purpose after archive.
## Requirements
### Requirement: 待发布版本不得写入生产图谱
治理模式下，`draft`、`pending_review` 和未发布的 `indexing` 版本 MAY 生成隔离 staging 数据，但 MUST NOT 写入生产 active 图谱；图谱候选必须携带 `knowledge_version_id`。

#### Scenario: 待审核版本抽取图谱
- **WHEN** 待审核版本完成 chunk 解析并发现实体关系
- **THEN** 系统将关系写入该版本隔离 staging namespace
- **AND** 当前 active 版本的图谱查询结果不发生变化

### Requirement: 图谱发布与当前版本原子切换
系统 MUST 将图谱构建纳入版本发布成功条件，并在图谱、向量和关键词索引全部成功后同时切换图谱 active namespace 与 `current_version_id`；任一索引失败时旧版本继续提供服务。

#### Scenario: 图谱索引失败
- **WHEN** 新版本文本索引成功但图谱索引失败
- **THEN** 新版本进入发布失败状态
- **AND** 旧版本的文本检索、图谱检索和引用继续可用

### Requirement: GraphContext 只使用当前有效版本
系统 MUST 在 GraphContext、结构化图结果和引用生成前过滤非当前、未发布、未生效或已过期版本的节点、边、路径和证据；过滤对象不得参与路径排序或回答生成。

#### Scenario: 混合版本路径
- **WHEN** 图路径同时包含当前版本和待发布版本的证据
- **THEN** 系统移除待发布证据及依赖该证据的路径
- **AND** 回答只能使用剩余可核验路径或转为文本检索

