# graph-ingest-retrieval-gates Specification

## Purpose
TBD - created by archiving change harden-graph-ingest-and-retrieval-gates. Update Purpose after archive.
## Requirements
### Requirement: 正式与试抽取强制 schema 过滤

系统 MUST 在正式入库抽取写图前，以及试抽取返回前，应用一致的 schema 过滤。关系类型白名单 MUST 使用 `ExtractConfig.Tags`；实体类型白名单 MUST 使用 `ExtractConfig.entity_types`。示例 `nodes` / `relations` MUST NOT 单独作为白名单来源。

当 `ExtractConfig.entity_types` 非空或 `strict_schema` 为 true 时，系统 MUST 在抽取 prompt 中注入实体类型白名单，并 MUST 解析模型输出以填充 `GraphNode.EntityType`。现网仅填充 Name/Attributes、不填充 EntityType 的解析行为 MUST 被修正，否则严格模式不可用。

当 `Tags` 非空时，系统 MUST 丢弃不在白名单中的关系类型，无论 `strict_schema` 取值如何。当 `Tags` 为空且 `strict_schema` 为 false 时，系统 MUST NOT 仅因调用关系过滤而丢弃全部关系。试抽取路径 MUST 与正式路径使用同一过滤语义，且 MUST NOT 在 `Tags` 为空且非严格模式下因无条件调用关系过滤而清空全部关系。

知识库 `ExtractConfig` MUST 支持 `strict_schema`（默认 false）与 `entity_types`（字符串列表，默认空）。当 `strict_schema` 为 true 且 `entity_types` 非空时，系统 MUST 丢弃 `EntityType` 为空或不在白名单中的节点及其相关关系，且 MUST NOT 通过事后将空类型默认成 `"entity"` 来绕过该过滤。当 `strict_schema` 为 true 且（`Tags` 为空或 `entity_types` 为空）时，系统 MUST NOT 向图数据库写入该次抽取结果（试抽取则 MUST 返回无合法关系的结果）。schema 过滤后若不存在任何合法关系三元组，正式路径 MUST NOT 向图数据库写入该 chunk 的图数据。

#### Scenario: Tags 非空时未知关系被拒绝（不依赖 strict）
- **WHEN** `Tags` 仅含 `uses`，模型输出关系类型为 `related_to`（无论 `strict_schema` 真假）
- **THEN** 该关系 MUST 被丢弃且不得写入图库（试抽取 MUST 不返回该关系）

#### Scenario: Tags 为空且非严格时保留探索性关系
- **WHEN** `Tags` 为空且 `strict_schema` 为 false，模型输出若干关系
- **THEN** 系统 MUST NOT 因空白名单关系过滤而丢弃全部关系

#### Scenario: 抽取产出实体类型
- **WHEN** `entity_types` 含 `tool`，模型按 prompt 输出带类型的实体
- **THEN** 解析结果中对应节点的 `EntityType` MUST 被填充（不得长期保持空字符串后再依赖默认 `"entity"`）

#### Scenario: 严格模式下未知或空实体类型被拒绝
- **WHEN** `strict_schema` 为 true，`entity_types` 仅含 `tool`，模型输出实体类型为 `unknown_type` 或未给出类型
- **THEN** 该实体及其相关关系 MUST 被丢弃且不得写入图库

#### Scenario: 严格模式下仍保留合法关系
- **WHEN** `strict_schema` 为 true，`Tags` 含 `uses`，`entity_types` 含所需类型，模型输出合法三元组
- **THEN** 该三元组 MUST 可被写入图库（试抽取 MUST 可返回）

#### Scenario: 过滤后无合法三元组不写图
- **WHEN** 抽取完成且 schema 过滤后关系列表为空
- **THEN** 正式路径 MUST NOT 将该结果写入图数据库

#### Scenario: 严格模式且白名单不完整不写图
- **WHEN** `strict_schema` 为 true，且 `Tags` 为空或 `entity_types` 为空
- **THEN** 正式路径 MUST NOT 向图数据库写入该次抽取结果

### Requirement: 既有入库门禁保持有效

系统 MUST 继续仅在知识库启用图谱索引时对文档 chunk 执行图抽取，且 MUST 仅对通过关系迹象判定的文本类 chunk 入队抽取。本 capability 不要求重建该决策逻辑，但 MUST 以回归测试覆盖以下行为。

#### Scenario: 知识库未开图

- **WHEN** 知识库 `IndexingStrategy.GraphEnabled` 为 false（或等价未启用）

- **THEN** 文档后处理 MUST NOT 创建图抽取任务

#### Scenario: chunk 无关系迹象

- **WHEN** 知识库已开图，但某文本 chunk 未通过关系迹象判定

- **THEN** 系统 MUST NOT 为该 chunk 入队图抽取任务

### Requirement: RAG 搜图自动门禁保持且可观测

系统 MUST 继续由检索阶段自动决定是否查询图数据库，且 MUST NOT 在知识库设置或普通问答 UI 中提供「是否搜索图数据库」的用户开关。

端到端仅当同时满足以下条件时 MUST 执行有效图谱检索：（1）层 1：当前问题被判定需要实体关系，且若已启用复杂度路由则路由预算允许使用图；（2）层 2：检索范围内存在已开图知识库且具备可检索实体。层 1 MUST 由既有 `shouldUseGraph`（及 Agent 侧等价判定）表达，本 capability MUST NOT 要求将「存在开图知识库」并入 `shouldUseGraph`。层 2 不满足时 MUST 在执行层跳过并记录原因。

任一条件不满足时 MUST 跳过图谱检索，并在遥测或流式附加信息中记录可审计的跳过原因。至少 MUST 能区分：不需要实体关系、路由预算禁用图、无开图知识库、无实体、无图证据。无路由决策时，`graph.requested`（或等价字段）MUST 与层 1 判定对齐，不得仅因缺少 `RoutingDecision` 而误标。引入更精确的 reason 时，系统 MUST 保留或以双写方式兼容既有旧 reason 字符串（例如 `routing_disabled_or_relation_not_needed`），MUST NOT 以破坏性更名打断既有验收对旧串的依赖。

#### Scenario: 非关系问题不搜图

- **WHEN** 用户问题不需要实体关系（或路由判定不需要）

- **THEN** 系统 MUST NOT 调用图数据库检索，且 MUST 记录跳过原因

#### Scenario: 关系问题且知识库开图

- **WHEN** 层 1 允许搜图，且层 2 存在开图知识库与可检索实体

- **THEN** 系统 MUST 执行图谱检索并与文本检索结果融合（沿用既有融合路径）

#### Scenario: 层 1 允许但无开图 KB

- **WHEN** 层 1 允许搜图，但检索范围内无开图知识库

- **THEN** 系统 MUST 跳过图谱检索，且 MUST 记录与「无开图知识库」对应的跳过原因（不得记成「不需要实体关系」）

### Requirement: 图谱抽取规则配置增强（不含搜图开关）

知识库编辑中的图谱设置 MUST 在既有启用/tags/示例实体与关系配置之上，允许管理员配置 `strict_schema` 与 `entity_types`，并 MUST 展示入库抽取规则说明（含「Tags 为关系类型白名单」「entity_types 为实体类型白名单」「搜图由系统自动判断」）。UI MUST 提供加载软件测评预设的操作：profile `relations` → `tags`，`concepts` → `entity_types`；加载预设时 MAY 默认打开 `strict_schema`，且结果可再编辑。UI MUST NOT 提供「问答时是否搜索图库」的开关。示例 `nodes` MUST NOT 被当作实体类型白名单的唯一来源。

#### Scenario: 配置严格 schema 与 entity_types 并保存

- **WHEN** 管理员开启严格 schema、编辑 `tags`/`entity_types` 并保存知识库

- **THEN** 后续入库抽取 MUST 按保存后的配置执行过滤

#### Scenario: 加载软件测评预设

- **WHEN** 管理员点击加载软件测评预设

- **THEN** `tags` 与 `entity_types` MUST 被填充为与软件测评 profile 对齐的默认集合，且可再编辑

#### Scenario: 无搜图开关

- **WHEN** 管理员打开知识库图谱设置页

- **THEN** 页面 MUST NOT 出现控制「检索时是否查询图数据库」的开关控件

