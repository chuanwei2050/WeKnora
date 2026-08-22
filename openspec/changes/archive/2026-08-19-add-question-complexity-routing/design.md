## Context

现有 query understand 在一次 LLM 调用中完成 query rewrite、宽泛意图分类和图像说明，`PipelineState` 只保存 `QueryIntent`。后续普通 RAG 或 Agent 执行链主要由用户选定的 Agent 决定，而不是由问题推理深度决定。立项要求的 L1–L4 分类需要复用多轮上下文，同时不能为每次请求无条件增加第二次模型调用或暴露模型内部思维链。

## Goals / Non-Goals

**Goals:**

- 产出稳定、类型化、可评测的复杂度等级和子类。
- 用声明式策略把等级映射到现有或后续可用的执行能力。
- 在低置信度和依赖能力缺失时安全降级。
- 默认关闭，避免改变现有会话行为和时延。

**Non-Goals:**

- 不训练新的 BERT/DeBERTa 模型；首版采用可替换的 LLM Few-shot 分类器。
- 不保存或展示模型私有 Chain-of-Thought，只保留简短判定依据。
- 不在本变更内实现多跳图检索或验证 Agent 本身。

## Decisions

### 1. 扩展现有 query understand 的结构化输出

新增 `QuestionComplexity`、`ReasoningSubtype` 和 `RoutingDecision` 类型，并把 `complexity_level`、`subtype`、`confidence`、`rationale_summary` 加入现有结构化响应。启用路由时复用同一次模型调用，以降低额外 TTFT；未启用 query rewrite 但启用了复杂度路由时仍执行该调用。

备选方案是独立分类服务，隔离性更强但固定增加一次调用和配置成本，首版不采用；接口保留，未来可替换为本地分类模型。

### 2. taxonomy 与路由策略配置化

在配置中定义四个稳定主级别：L1 显性事实、L2 隐性事实、L3 可解释推理、L4 隐式推理。上下文/常识补全是 L2 子类，比较/多跳是 L3 子类，因果、假设和迁移是 L4 子类；子类、Few-shot 示例和执行策略允许版本化配置。

路由动作使用有限枚举而非直接指定内部函数：`quick_rag`、`contextual_rag`、`graph_reasoning`、`verified_agent`。session service 把动作转换为当前可用能力。

### 3. 能力感知的确定性降级

路由器在派发前读取知识库索引策略、Agent 模式和可用模型。降级链以目标动作作为起点，内置链分别为：`verified_agent → graph_reasoning → contextual_rag → quick_rag`、`graph_reasoning → contextual_rag → quick_rag`、`contextual_rag → quick_rag` 和 `quick_rag`。系统从目标链中选择首个可用动作并记录原因，不得跳到目标链之前的更高成本、权限或外部依赖动作；自定义链若包含升级或未知动作则拒绝保存。分类失败或置信度低于阈值时使用配置的保守默认，首版默认 `contextual_rag`，并只沿该默认动作自身的降级链选择。

### 4. 路由只控制预算，不绕过权限

RoutingDecision 可以调整 query expansion、检索 TopK、图检索开关、Agent 最大迭代和验证开关，但不能扩大 Agent 已授权的知识库、工具、模型或网络访问范围。

### 5. 结果进入事件与 tracing

记录 taxonomy 版本、等级、子类、置信度、最终动作、降级原因和分类耗时。前端默认只显示简短状态；完整字段供评测与管理员诊断使用。

## 集成边界

- 本变更只产出 `RoutingDecision` 和预算建议，不实现图查询或验证协调器。
- `add-multihop-graph-retrieval` 和 `add-verified-multi-agent-answering` 是可选消费者；任一未启用时必须按本变更的确定性规则降级。
- `add-research-acceptance-benchmarks` 负责使用专家标签评估分类结果，本变更只记录 taxonomy 与路由版本。
- `add-air-gapped-model-deployment` 只替换分类模型提供方，不改变 L1-L4 契约和同一套 Few-shot 配置。

## Risks / Trade-offs

- [单次调用同时做改写和分类可能相互影响] → 使用严格 JSON schema、独立字段校验和回归数据集；任一字段失败只回退该字段。
- [分类不稳定导致执行路径抖动] → temperature 设低、taxonomy 版本化、置信阈值与固定降级规则。
- [复杂路由增加成本] → 默认关闭，并允许仅对特定 Agent 或问题类型启用。
- [模型判定理由泄露内部推理] → prompt 明确只返回一句可审计标签依据，不返回逐步思维链。

## Migration Plan

1. 先增加新类型、配置字段和兼容解析，所有开关默认关闭。
2. 扩展 query understand prompt 与测试，缺少新字段时继续按旧响应处理。
3. 接入 routing adapter 和事件，先在测试 Agent 灰度。
4. 使用软件测评问题集校准阈值和 Few-shot 示例后再启用内置 Agent。
5. 回滚仅需关闭开关；新增字段不影响旧流程。

## Open Questions

- L1–L4 子类及示例需由软件测评专家提供和标注一批基准问题后定版。
- L4 默认是否必须启用多模型验证，将在 `add-verified-multi-agent-answering` 的联调评测后确定。

## 补充设计决策

### 6. 普通 RAG 与 Agent 共用一次路由决策

复杂度路由必须在会话入口完成，并把同一份不可变 `RoutingDecision` 传给普通 RAG 和 `AgentEngine`。Agent 入口不得重新用字符串规则或模型自由判断覆盖复杂度、图谱开关、最大迭代和验证预算。路由只负责选择已授权能力，Agent 仍可在授权范围内决定具体工具调用顺序。

`verified_agent` 必须对应真实的 Agent/ReAct 执行分支，并在该分支完成验证协调；若 Agent、验证器或必需模型不可用，则按已保存降级链退回，不得仅通过普通 RAG 的验证后处理伪装成 Agent 路由。

### 7. 复杂问题采用有界的子问题计划

对标记为多跳、比较、因果或假设迁移的问题，路由结果可以携带有序 `SubQuestionPlan`。每个子问题必须引用前置结果而不得依赖未解析的代词；计划保存最大子问题数、单步检索预算和终止条件。执行器按顺序检索、去重和汇总，证据不足时最多按总预算重写一次后续子问题，不建立无界的 Agent 循环。

问题拆解属于路由消费者的执行能力，不改变 L1-L4 taxonomy；不需要拆解的问题仍走单轮检索。
