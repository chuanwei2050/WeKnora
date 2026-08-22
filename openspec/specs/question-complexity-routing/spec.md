# question-complexity-routing Specification

## Purpose
TBD - created by archiving change add-question-complexity-routing. Update Purpose after archive.
## Requirements
### Requirement: 普通 RAG 与 Agent 使用同一份路由决策
启用复杂度路由时，普通 RAG 入口和 Agent 入口 MUST 使用同一份不可变 `RoutingDecision`，包括实际动作、图谱使用判断、检索预算、最大 Agent 迭代和验证预算；Agent 入口不得使用独立的字符串匹配结果覆盖该决策。

#### Scenario: Agent 入口消费路由结果
- **WHEN** 一个启用复杂度路由的 Agent 收到 L3 多跳问题
- **THEN** Agent 执行上下文包含与普通 RAG 相同的 `RoutingDecision`
- **AND** 图谱开关、迭代上限和验证预算均来自该决策

#### Scenario: 验证式 Agent 能力不可用
- **WHEN** 路由目标为 `verified_agent` 但 Agent 或验证模型能力不可用
- **THEN** 系统沿目标动作已保存的降级链选择实际动作
- **AND** 不把普通 RAG 的验证后处理记录为 `verified_agent` 执行

### Requirement: 有界问题拆解与迭代检索
对于被分类为多跳、比较、因果或假设迁移的问题，系统 SHALL 在预算内生成有序子问题计划，并按前一步结果驱动后一步检索；每个计划 MUST 限制子问题数量、检索调用次数和总耗时。

#### Scenario: 多跳问题按顺序检索
- **WHEN** 问题被分类为 L3 多跳且执行能力可用
- **THEN** 系统生成不依赖未解析代词的有序子问题
- **AND** 后续子问题只能使用原问题和已确认的前序结果

#### Scenario: 达到拆解预算
- **WHEN** 子问题数量、检索调用数或总耗时达到上限
- **THEN** 系统停止生成新的子问题
- **AND** 使用已有证据生成回答或按降级策略保守回答

### Requirement: 图谱使用决策可审计
系统 MUST 在文档入库和问答检索两个边界分别记录图谱跳过、启用或降级的原因；关系信号不足的问题或 chunk 不得因为知识库启用了图谱而强制执行实体抽取或图谱查询。

#### Scenario: 普通事实问题跳过图谱
- **WHEN** 问题不包含实体关系、层级、路径或多跳意图
- **THEN** 系统跳过图谱检索并记录 `graph_not_needed`
- **AND** 普通向量/关键词检索继续执行

