# graph-triple-review Specification

## Purpose
TBD - created by archiving change add-graph-triple-review-queue. Update Purpose after archive.
## Requirements
### Requirement: 三元组审核开关

知识库 `ExtractConfig` MUST 支持 `require_triple_review`（默认 false）。为 false 时，正式抽取在 schema 过滤后 MUST 直接写入图库。为 true 时，正式抽取在 schema 过滤后 MUST 创建 `pending` staging 记录，且 MUST NOT 在状态变为 `written` 之前写入 Neo4j。

#### Scenario: 关闭审核直写

- **WHEN** `require_triple_review` 为 false 且过滤后存在合法关系

- **THEN** 系统 MUST 按既有路径写入图库

#### Scenario: 开启审核入队

- **WHEN** `require_triple_review` 为 true 且过滤后存在合法关系

- **THEN** 系统 MUST 创建 `pending` staging，且 MUST NOT 立即写入图库

#### Scenario: 开启审核且过滤后无关系

- **WHEN** `require_triple_review` 为 true 且 schema 过滤后无合法关系

- **THEN** 系统 MUST NOT 创建 staging，也 MUST NOT 写图

### Requirement: 审核状态机与写图

系统 MUST 使用 `pending | written | rejected | superseded` 状态机。候选 MUST 绑定创建时的知识库配置指纹和知识版本。`approve` 仅适用于仍匹配当前配置、chunk 和有效知识版本的 `pending` 候选；不匹配时 MUST 转为 `superseded` 且不得写图。关闭人工审核或改变建图配置时，相关知识库现有 pending 候选 MUST 转为 `superseded`。

#### Scenario: 当前候选通过后入图
- **WHEN** 候选为 pending 且配置、chunk 和知识版本仍有效
- **THEN** approve 成功后图库 MUST 含该候选关系且状态 MUST 为 written

#### Scenario: 通过后入图
- **WHEN** 审核人对仍匹配当前配置、chunk 和知识版本的 `pending` 候选执行 approve 且写图成功
- **THEN** 图库 MUST 含该候选关系，且状态 MUST 为 `written`

#### Scenario: 写图失败可重试
- **WHEN** approve 时写图失败
- **THEN** 状态 MUST 仍为 `pending`，且允许再次 approve

#### Scenario: 驳回
- **WHEN** 审核人驳回 `pending`
- **THEN** 状态 MUST 为 `rejected`，且 MUST NOT 写图

#### Scenario: 关闭审核使旧候选失效
- **WHEN** 管理员关闭三元组人工审核
- **THEN** 该知识库现有 pending 候选 MUST 变为 superseded

#### Scenario: 配置或版本已变化
- **WHEN** 审核人批准的候选不再匹配当前配置或知识版本
- **THEN** 系统 MUST 拒绝写图并将候选标记为 superseded

### Requirement: 同 chunk 候选收敛

当同一 chunk 再次产生需审核的抽取结果时，系统 MUST 在创建新 `pending` 之前，将该 chunk 上既有 `pending` 候选标记为 `superseded`（或等价取消）。

#### Scenario: 重复抽取替代旧单

- **WHEN** chunk C 已有 pending 候选，再次抽取并为 C 创建新候选

- **THEN** 旧候选 MUST 不再处于 pending

### Requirement: 审核 UI 独立于版本治理

管理界面 MUST 提供图三元组待审列表与详情，并支持通过/驳回。MUST 调用三元组审核 API，MUST NOT 复用知识版本 approve/reject 接口。试抽取 MUST NOT 进入审核队列。

#### Scenario: 列表可见待审

- **WHEN** 存在 pending 候选

- **THEN** 审核页 MUST 能列出并打开详情

#### Scenario: 试抽取不入队

- **WHEN** 管理员执行试抽取

- **THEN** MUST 即时返回，且 MUST NOT 仅因此创建 pending 审核单
