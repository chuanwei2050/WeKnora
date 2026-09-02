## ADDED Requirements

### Requirement: Rerank 结果必须可区分
系统 MUST 将 Rerank 结果表示为成功、无相关结果、服务不可用或候选无效之一，不得仅以空列表同时表达这些状态。

#### Scenario: Rerank 模型解析不可用
- **WHEN** 初始召回非空但无法取得 Rerank 模型
- **THEN** 系统标记 unavailable、保留原始召回并继续回答链路

#### Scenario: Rerank 返回非法候选索引
- **WHEN** Rerank 响应包含负数或越界候选索引
- **THEN** 系统不得访问该索引或部分采信响应，并标记 invalid_candidate

#### Scenario: 召回有结果但全部低于阈值
- **WHEN** 初始召回非空且 Rerank 判定没有相关候选
- **THEN** 系统返回无相关结果并进入无答案流程，不得回退原始召回

#### Scenario: Rerank 服务不可用
- **WHEN** Rerank 因超时或外部服务错误不可用
- **THEN** 系统 MAY 使用原始召回继续，并 MUST 标记降级原因

### Requirement: 空结果控制流必须回归保护
系统 MUST 覆盖召回非空而 Rerank 为空的流水线测试，并保证不会误触发后置回答反思。

#### Scenario: 空结果结束检索
- **WHEN** Rerank 返回无相关结果
- **THEN** 系统生成明确的无答案响应且不调用回答生成或回答后反思
