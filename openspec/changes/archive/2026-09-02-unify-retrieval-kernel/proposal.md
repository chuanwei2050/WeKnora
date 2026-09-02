## Why

普通 RAG 与 Agent RAG 重复实现检索底层逻辑，修复和策略容易漂移；但二者的上层交互语义不同，直接合并整条流水线风险过高。

## What Changes

- 提取共享的检索请求、检索结果协议与底层调度内核。
- 普通 RAG 和 Agent RAG 保留各自上层编排与终止语义。
- 用契约测试保证两条入口对相同检索请求采用一致底层行为。

## Capabilities

### New Capabilities
- `shared-retrieval-kernel`: 规定普通与 Agent RAG 共用的检索边界和一致性要求。

### Modified Capabilities

## Impact

影响两类 RAG 的检索入口、公共搜索服务、结果类型和契约测试，不改变外部 API。
