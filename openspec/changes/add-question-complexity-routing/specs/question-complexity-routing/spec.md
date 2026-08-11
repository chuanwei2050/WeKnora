## ADDED Requirements

### Requirement: 四级复杂度 taxonomy
系统 SHALL 定义版本化的 L1–L4 问题复杂度 taxonomy，其中 L1 表示显性事实，L2 表示隐性事实，L3 表示可解释推理，L4 表示隐式推理；上下文/常识补全、比较/多跳以及因果/假设/迁移分别作为对应层级的可配置子类，并 SHALL 支持 Few-shot 示例。

#### Scenario: 加载 taxonomy
- **WHEN** 启用了复杂度路由的 Agent 开始处理请求
- **THEN** 系统加载该 Agent 绑定的 taxonomy 版本、子类和示例
- **AND** tracing 记录实际使用的 taxonomy 版本

### Requirement: 基于完整会话语境分类
启用复杂度路由时，系统 MUST 结合当前问题、允许的历史轮次、附件文本和已生成的图像说明进行分类，并返回等级、子类、0 到 1 的置信度及简短依据。

#### Scenario: 指代依赖前文
- **WHEN** 用户问题包含“它”且所指对象出现在允许的会话历史中
- **THEN** 分类输入包含相关历史
- **AND** 输出能够把问题判定为需要上下文补全的 L2 或更高等级

### Requirement: 严格结构化与独立回退
分类输出 MUST 通过类型和取值校验；等级、子类或置信度字段无效时，系统 MUST 使用配置的回退值，且不得因分类响应格式错误中断问答。

#### Scenario: 模型返回未知等级
- **WHEN** 分类模型返回 `L5` 或无法解析的 JSON
- **THEN** 系统记录解析失败并使用保守默认动作
- **AND** 用户请求继续执行

### Requirement: 等级到执行策略映射
系统 SHALL 通过配置把复杂度等级和子类映射到有限的路由动作及预算参数，至少支持 `quick_rag`、`contextual_rag`、`graph_reasoning` 和 `verified_agent`。

#### Scenario: L1 事实问题
- **WHEN** 问题被高置信度分类为 L1 单跳事实且知识库支持混合检索
- **THEN** 系统选择 `quick_rag` 并使用该策略配置的检索预算

#### Scenario: L3 多跳问题
- **WHEN** 问题被分类为 L3 多跳推理且图检索能力可用
- **THEN** 系统选择 `graph_reasoning` 并把路由元数据传给执行链

### Requirement: 能力感知降级
目标动作所需的模型、索引或 Agent 能力不可用时，系统 MUST 从该目标动作自己的确定性降级链中选择首个可用动作，并 MUST 记录目标动作、实际动作和降级原因。降级链不得包含比目标动作成本、权限或外部依赖更高的动作，非法链 MUST 在保存配置时被拒绝。

#### Scenario: 图检索不可用
- **WHEN** 路由结果为 `graph_reasoning` 但目标知识库未启用 Neo4j 图检索
- **THEN** 系统按 `graph_reasoning` 的已保存降级链选择首个可用的 `contextual_rag` 或 `quick_rag`
- **AND** 不向用户暴露内部错误堆栈

#### Scenario: 图谱目标不得升级到验证式回答
- **WHEN** 目标动作为 `graph_reasoning`、该能力不可用但 `verified_agent` 可用
- **THEN** 系统不得选择不在 `graph_reasoning` 降级链中的 `verified_agent`
- **AND** 系统继续检查 `contextual_rag` 和 `quick_rag`

### Requirement: 低置信度处理
当分类置信度低于 Agent 配置阈值时，系统 MUST 使用配置的保守动作，而不得把低置信分类直接用于成本、权限或外部依赖更高的路径。

#### Scenario: 置信度低于阈值
- **WHEN** 分类置信度为 0.45 且阈值为 0.60
- **THEN** 系统选择配置的保守默认动作并标记 `low_confidence`

### Requirement: 权限不扩张
复杂度路由 MUST 仅在 Agent 已授权的知识库、工具、模型和网络访问范围内选择策略，不得因为等级较高而扩大权限。

#### Scenario: L4 请求需要网络但 Agent 禁止网络
- **WHEN** L4 策略建议使用网络检索而当前 Agent 的 `web_search_enabled` 为 false
- **THEN** 系统不得启用网络工具
- **AND** 使用已授权能力完成或降级回答

### Requirement: 可选启用与兼容行为
复杂度路由 SHALL 支持租户/Agent 级开关且默认关闭；关闭时系统 MUST 保持现有 `QueryIntent` 和问答执行行为。

#### Scenario: 旧 Agent 未配置路由
- **WHEN** 既有 Agent 配置中没有复杂度路由字段
- **THEN** 系统按关闭处理且不要求迁移客户端请求

### Requirement: 路由可观测性
系统 MUST 为每次启用路由的请求记录分类耗时、等级、子类、置信度、最终动作和降级原因，并可通过现有事件流或 tracing 查询。

#### Scenario: 管理员排查路由结果
- **WHEN** 管理员查看一次已完成请求的 trace
- **THEN** 能看到该请求的分类与最终派发信息
- **AND** 记录中不包含模型私有 Chain-of-Thought
