## Why

立项材料要求按显性事实、隐性事实、可解释推理和隐性推理四层理解问题并派发不同任务。当前 `QueryIntent` 只区分知识库检索、闲聊、总结等宽泛意图，普通 RAG 与 ReAct 也没有统一的复杂度标签和调度策略，简单问题可能承担不必要延迟，复杂问题则可能缺少足够推理步骤。

## What Changes

- 定义 L1–L4 问题复杂度及可扩展子类，输出结构化等级、子类、置信度和可审计的简短判定依据。
- 在现有 query understand 阶段加入可配置的 Few-shot 分类，复用多轮上下文、附件和图像描述。
- 建立等级到执行策略的映射：快速事实检索、上下文补全、多跳/比较推理、假设/因果推理与验证。
- 低置信度、分类失败或模型不可用时采用确定性的保守回退，不阻断现有问答。
- 将路由结果写入运行时状态、流式事件和可观测性数据，便于调试和评测。
- 提供租户/Agent 级开关、阈值和示例配置；默认关闭以保持现有行为。

## Capabilities

### New Capabilities

- `question-complexity-routing`: L1–L4 问题复杂度识别、子类判定、策略派发、回退和可观测性。

### Modified Capabilities

无。

## Impact

- 后端：`internal/types/chat_manage.go`、chat pipeline 的 query understand、session/agent 配置、事件与 tracing。
- 配置：新增复杂度 taxonomy、Few-shot 示例、置信度阈值和策略映射。
- 前端：Agent 高级配置及可选的路由状态展示。
- 兼容性：未启用该能力时继续使用当前意图分类与执行链。
