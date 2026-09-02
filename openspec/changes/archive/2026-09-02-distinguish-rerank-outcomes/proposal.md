## Why

Rerank 返回空列表时，当前流水线把“没有相关结果”和“Rerank 服务不可用”都当成检索失败并提前结束，既绕过后续控制流，也无法采用正确的降级策略。

## What Changes

- 为 Rerank 定义可区分的结果状态：成功、无相关结果、服务不可用、候选无效。
- 仅在服务不可用时回退到原始召回；无相关结果保持无答案语义。
- 补充“召回有结果、Rerank 为空”等控制流回归测试。

## Capabilities

### New Capabilities
- `rerank-outcome-control`: 规定 Rerank 各类结果的控制流、降级边界和可观测性。

### Modified Capabilities

## Impact

影响普通 RAG 的 Rerank 节点、会话问答错误处理、检索结果状态以及相关单元和流水线测试。
