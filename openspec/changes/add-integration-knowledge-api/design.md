## Context

WeKnora 内部已有知识库列表、多知识库搜索和流式聊天能力，但内部 DTO、权限入口与 pipeline 事件不适合作为长期外部契约。本变更在 `add-integration-client-authz` 之上建立独立版本化边界，使多个宿主能以稳定方式查询授权知识库、执行 RAG 和维持可恢复聊天。

## Goals / Non-Goals

**Goals:**

- 提供版本化 `/api/integration/v1/*` 列表、搜索和聊天接口。
- 对每个请求执行统一资源范围求交与拒绝审计。
- 支持多知识库统一检索、来源追踪、聊天逐轮选库、幂等、取消和 SSE 续接。
- 与现有 `/api/v1/*` 并存，隔离内部模型演进。

**Non-Goals:**

- V1 不开放内部模型配置、密钥、存储和治理细节。
- V1 不实现每库配额的 balanced retrieval。
- 不在本变更实现页面 iframe 或 Widget UI。

## Decisions

### 独立 Integration API DTO

后端路由固定为 `/api/integration/v1/*`；同域代理可将其呈现为 `/knowledge/api/integration/v1/*`。列表和搜索结果只公开稳定字段，不复用内部完整模型。API 版本升级通过新路径或兼容字段扩展完成。

### 越权请求整体失败

所有知识库 ID 先交由 `integration-client-authz` 计算有效范围。只要请求包含一个未授权 ID，整次返回 `403`，不做部分搜索。相比静默过滤，该行为更容易让宿主发现配置错误并避免误解回答来源。

### 多知识库统一召回与重排

V1 将允许范围内知识库合并进入现有召回管线，统一重排后返回全局 Top K。结果必须带知识库、知识、版本和 chunk 标识。默认可选数量不硬编码为 5；实现前通过召回质量、P95 延迟、上下文长度和并发压测确定默认值、请求大小与租户硬上限。

### 会话保存授权上限、消息保存实际快照

RAG 搜索必须显式提交至少一个 `knowledge_base_id`；缺失、null 或空数组统一返回 `400`，不把空值解释为全部。

聊天创建会话时必须显式提交 `knowledge_base_mode`：

- `selected`：同时提交非空 `knowledge_base_ids`，作为默认选择；每轮消息必须提交非空 `selected_knowledge_base_ids`。
- `all-allowed`：不得提交知识库数组；服务端在会话创建时计算并冻结 `allowed_knowledge_base_ids` 授权上限，每轮使用该上限与当前 client allowlist、用户实时权限的交集，不因后续新增授权自动扩大会话范围。

缺失、null、空数组以及模式与字段组合冲突统一返回 `400`。请求包含越权 ID 时整轮返回 `403`。消息记录保存最终实际范围，确保历史回答可追溯。

### 稳定 SSE 事件与恢复协议

V1 只承诺 `message.created`、`answer.delta`、`answer.completed` 和 `error`。统一 envelope 包含 `event_id`、`event`、`session_id`、`message_id`、`sequence`、`occurred_at` 和 `data`。`answer.delta.data` 包含增量文本；`answer.completed.data` 包含终态 `completed`、最终回答与来源快照；`error.data` 包含稳定错误码、可重试标记和终态 `failed` 或 `cancelled`。客户端用 `Last-Event-ID` 或 `after_event_id` 续接。内部 pipeline 事件只能作为版本化可选扩展。

恢复接口固定为：

```http
GET /api/integration/v1/chat/sessions/{id}
GET /api/integration/v1/chat/sessions/{id}/messages/{message_id}
GET /api/integration/v1/chat/sessions/{id}/messages/{message_id}/events?after_event_id={event_id}
```

当 cursor 超出事件保留期时，事件接口返回 `410 Gone` 和 `message_snapshot_url`；客户端随后查询消息快照，不得重新提交消息。消息终态固定为 `completed`、`failed`、`cancelled`。

### 幂等和取消属于消息协议

创建会话和发送消息接受 `Idempotency-Key`，相同主体、端点和有效载荷的重试返回同一结果；key 相同但载荷冲突时返回稳定错误。取消使用独立消息 cancel 端点，并保证取消后的 `cancelled` 终态可从 SSE 或消息查询接口获得。

## Risks / Trade-offs

- [SSE 续接需要保存事件] → 保存有限时间的事件日志，并定义完成事件后的清理窗口。
- [统一召回可能被强势知识库占满] → V1 保持算法简单并监控每库命中分布，balanced 模式留作独立增强。
- [每轮实时鉴权增加开销] → 缓存低风险授权元数据，但不得缓存越过撤销边界。
- [幂等记录增长] → 按 client 和端点设置有效期与容量上限，完成后异步清理。

## Migration Plan

0. 确认 `add-integration-client-authz` 已完成实施及验收；未满足时停止本 change。
1. 注册 Integration API 路由和独立 DTO。
2. 先上线列表与搜索并与内部接口做结果和授权对照测试。
3. 增加聊天选择模式、消息快照查询、稳定 SSE envelope、事件存储、过期 cursor 恢复、幂等和取消。
4. 通过测试 client 灰度，确定多库数量与请求大小限制后再开放生产 client。
5. 回滚时禁用 Integration API 路由；内部 `/api/v1/*` 和独立聊天不受影响。

## Open Questions

多知识库默认数量、租户硬上限、`top_k` 上限和 SSE 事件保留时长必须由基准测试定稿，不得使用未经验证的常量；空值语义、事件过期响应和快照恢复协议已经固定，不再作为开放问题。
