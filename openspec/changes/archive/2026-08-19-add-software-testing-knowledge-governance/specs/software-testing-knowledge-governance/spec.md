## ADDED Requirements

### Requirement: 软件测评领域知识模型
系统 SHALL 提供带版本号的软件测评领域 profile，定义标准知识、基础理论知识、企业内部体系知识、项目/专家经验知识四层来源，以及测评对象、质量特性、测试方法、工具、指标、缺陷、结论等核心概念、属性及允许关系，并允许知识库管理员在启用时创建知识库级配置快照。

#### Scenario: 启用领域 profile
- **WHEN** 知识库管理员为一个文档知识库启用 `software-testing` profile
- **THEN** 系统创建带 profile 版本号的实体、关系和来源字段配置快照
- **AND** 后续全局 profile 更新不得静默覆盖该知识库快照

### Requirement: 来源与效力元数据
治理模式下的知识版本 MUST 保存四层知识来源之一、具体来源类别、版本标签和权威等级；profile 标记为必填的标准编号、发布日期、生效日期或失效日期 MUST 在提交审核前通过校验。

#### Scenario: 标准缺少版本信息
- **WHEN** 编辑者提交来源类别为“标准”的知识版本但未填写 profile 要求的标准编号或版本
- **THEN** 系统拒绝提交审核并返回缺失字段列表

#### Scenario: 内部规范标记知识层级
- **WHEN** 编辑者提交企业内部测试流程或体系规范
- **THEN** 系统要求其知识层级为 `internal` 并保存具体来源部门或体系标识

### Requirement: 不可变知识版本
系统 SHALL 为内容或受治理元数据发生变化的知识创建不可变版本，并 MUST 保留版本创建人、时间、内容哈希、来源快照和前一版本引用；相同哈希与相同元数据的重复同步不得生成新版本。

#### Scenario: 外部数据源内容变化
- **WHEN** 已存在知识的外部修订号或内容哈希发生变化
- **THEN** 系统创建新的待审核版本
- **AND** 当前已发布版本继续保持可检索

#### Scenario: 重复同步相同内容
- **WHEN** 数据源再次同步相同内容和相同治理元数据
- **THEN** 系统复用现有版本并记录同步结果为无变化

### Requirement: 知识版本生命周期与权限
系统 MUST 使用单一、可穷举的生命周期状态表示知识版本，至少包含 `draft`、`pending_review`、`approved`、`indexing`、`scheduled`、`active`、`publish_failed`、`superseded`、`rejected` 和 `expired`，并 MUST 拒绝未定义的状态迁移。审核、发布和当前有效标记 MUST 从生命周期状态及 `current_version_id` 派生，不得由可互相矛盾的独立布尔字段表达。系统 MUST 只允许具备知识库审核权限且不是该版本提交人的用户执行批准或驳回。

#### Scenario: 编辑者提交并由管理员批准
- **WHEN** 编辑者提交一个校验通过的草稿版本且另一名知识库管理员批准
- **THEN** 系统记录不可变审核意见、审核人和审核时间
- **AND** 版本从 `pending_review` 进入 `approved`，随后只能按定义迁移到 `indexing`

#### Scenario: 提交人尝试自审
- **WHEN** 版本提交人尝试批准自己的待审核版本
- **THEN** 系统拒绝操作且版本状态不变

#### Scenario: 索引失败后重试
- **WHEN** `indexing` 版本的任一生产索引构建失败
- **THEN** 版本进入 `publish_failed` 且旧 `active` 版本保持不变
- **AND** 重试只能将该版本重新转入 `indexing`，不得直接设为 `active`

### Requirement: 原子发布与检索可见性
系统 MUST 在新版本全部生产索引成功后才切换当前版本；检索、图谱和问答 SHALL 只使用当前已发布且处于有效期内的版本。发布失败时旧版本 MUST 保持可用。

#### Scenario: 新版本索引失败
- **WHEN** 已批准版本的任一向量、关键词或图谱索引步骤失败
- **THEN** 系统将新版本标记为 `publish_failed` 并保留可重试状态
- **AND** 当前版本指针与旧版本检索结果保持不变

#### Scenario: 知识版本到期
- **WHEN** 当前版本到达配置的失效时间且没有新的有效已发布版本
- **THEN** 系统将其标记为 `expired` 并从生产检索与图谱中停用
- **AND** 系统在同一原子操作中清空 `current_version_id`，不得使当前指针指向非 `active` 版本

#### Scenario: 发布未来生效版本
- **WHEN** 管理员批准并发布一个 `effective_at` 晚于当前时间的新版本
- **THEN** 系统完成预备索引、将新版本设为 `scheduled` 并继续使用旧的 `active` 版本
- **AND** 到达生效时间后才原子地把新版本设为 `active`、旧版本设为 `superseded` 并切换当前指针

### Requirement: 版本回滚
知识库管理员 SHALL 能够把当前版本回滚到历史上已批准、状态为 `superseded` 且当前仍有效的版本；回滚 MUST 使目标版本重新经过 `indexing` 和原子切换流程。

#### Scenario: 回滚到历史版本
- **WHEN** 管理员选择一个符合条件的历史版本并确认回滚
- **THEN** 系统完成索引切换后更新当前版本指针
- **AND** 保留回滚操作审计记录

### Requirement: 可追溯引用
问答引用 MUST 返回稳定 knowledge ID、具体 version ID、知识层级、来源标题以及可用的标准编号、版本标签和生效信息，使用户能够核验答案所依据的知识版本。

#### Scenario: 答案引用标准条款
- **WHEN** 回答使用已发布的软件测试标准知识作为证据
- **THEN** 引用中展示该标准的编号、版本标签和对应 version ID

### Requirement: 兼容未治理知识库
未启用治理模式的知识库 MUST 保持现有创建、同步、解析、索引和检索行为，现有 API 客户端不得被强制提供治理字段。

#### Scenario: 旧知识库上传文档
- **WHEN** 用户向未启用治理模式的既有知识库上传文档
- **THEN** 系统按当前即时处理流程完成入库
- **AND** 请求不因缺少治理元数据而失败

## ADDED Requirements

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
