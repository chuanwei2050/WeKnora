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

### Requirement: 共享文本检索必须执行源知识版本门禁
系统 MUST 在共享访问授权成立后，使用知识真实归属可见的元数据过滤文本召回；仅当前发布版本、可检索且未失效的结果可进入排序、上下文和引用。

#### Scenario: 合法共享知识的当前版本
- **WHEN** 请求租户可访问源租户共享知识，召回结果属于其当前可检索版本
- **THEN** 该结果可继续参与排序和回答

#### Scenario: 共享知识的旧版本
- **WHEN** 召回结果属于共享知识但 `knowledge_version_id` 与当前版本不一致
- **THEN** 系统在排序和回答前拒绝该结果并记录 version_mismatch

### Requirement: 元数据缺失必须拒绝且 legacy 必须显式识别
系统 MUST 将知识元数据缺失与已读取且无版本字段的 legacy 知识区分；元数据缺失 MUST fail closed，明确 legacy 知识 MAY 保持兼容。

#### Scenario: 共享召回无法解析知识元数据
- **WHEN** 召回结果引用的知识 ID 无法读取对应元数据
- **THEN** 系统拒绝该结果并记录 metadata_missing，不得按 legacy 自动放行

#### Scenario: 明确的无版本历史知识
- **WHEN** 系统成功读取知识元数据且确认其没有 `CurrentVersionID`
- **THEN** 系统可按 legacy 兼容规则允许其结果

