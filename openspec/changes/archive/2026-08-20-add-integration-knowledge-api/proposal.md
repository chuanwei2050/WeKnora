## Why

外部项目不能长期依赖 WeKnora 内部 `/api/v1` DTO 和聊天流水线事件，否则内部演进会直接破坏集成方。需要提供稳定、可授权、可审计的知识库列表、RAG 搜索和流式聊天契约。

## What Changes

- 新增 `/api/integration/v1/*` API，覆盖知识库列表、多知识库 RAG 搜索、聊天会话、流式消息和取消。
- 所有资源范围取请求选择、integration client allowlist、当前用户权限和租户边界的交集；越权请求整次返回 `403`。
- 多知识库搜索采用统一召回、统一重排和全局 Top K，并返回知识库、文档、版本及 chunk 来源。
- RAG 搜索必须显式提供非空知识库数组；聊天必须显式选择 `selected` 或 `all-allowed` 模式，缺失、null 和空数组不再隐式代表全部。
- 聊天会话保存授权上限，每轮按选择模式实时重新鉴权并记录实际使用快照。
- 为创建会话和发送消息定义 `Idempotency-Key`，为 SSE 定义稳定事件 envelope、事件 ID、断线续传、消息快照查询、事件过期 `410` 及取消语义。

## Capabilities

### New Capabilities

- `integration-knowledge-api`: 定义外部知识库列表、多库 RAG 搜索和可恢复流式聊天 API 的稳定行为与授权契约。

### Modified Capabilities

无。

## Impact

- 影响后端路由、DTO、检索编排、聊天会话、SSE 事件存储、幂等处理、审计和限流。
- **实施门禁**：只有 `add-integration-client-authz` 已完成实施并通过验收后，才能开始本 change；仅有 planning artifacts complete 不满足门禁。
- 新 API 与现有 `/api/v1/*` 并存，不改变独立模式内部 API 行为。
