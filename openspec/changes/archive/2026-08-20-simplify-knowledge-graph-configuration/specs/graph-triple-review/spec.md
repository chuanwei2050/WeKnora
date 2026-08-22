## MODIFIED Requirements

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
