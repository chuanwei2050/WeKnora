## 1. API 边界与 DTO

- [x] 1.1 执行实施门禁：核验 `add-integration-client-authz` 已完成代码实施及验收；未满足时停止本 change
- [x] 1.2 注册 `/api/integration/v1/*` 路由并建立版本化请求、响应和稳定错误模型，在 HTTP 边界把外部 DTO 解析为不可表示非法模式组合的内部命令
- [x] 1.3 实现知识库列表接口，只返回授权知识库和稳定公开字段
- [x] 1.4 为列表、搜索、会话、消息和取消分别接入限流及审计

## 2. 多知识库 RAG 搜索

- [x] 2.1 实现 `knowledge_base_ids` 必填且非空、query、`top_k` 和请求大小的边界验证，并让缺失、null、空数组统一返回 `400`
- [x] 2.2 直接复用现有 `SearchKnowledge`/`HybridSearch` 管线完成多知识库统一召回、统一重排和全局 Top K，不创建第二套检索实现
- [x] 2.3 在结果中返回知识库、知识、版本、chunk、标题、来源和综合得分
- [x] 2.4 对混入越权知识库的请求整体返回 `403` 并验证不执行部分搜索
- [ ] 2.5 完成召回质量、P95 延迟、上下文长度和并发基准，固化配置化默认值与硬上限

## 3. 会话与消息协议

- [x] 3.1 扩展现有 session/message 模型实现 `selected` 与 `all-allowed` 显式会话模式、非法字段组合 `400`、授权上限冻结及 client/租户/用户绑定，不建立平行聊天存储
- [x] 3.2 实现 selected 每轮非空选库、all-allowed 每轮使用冻结上限、实时鉴权及实际范围快照持久化
- [x] 3.3 为会话和消息创建实现 `Idempotency-Key`、相同请求复用及载荷冲突检测
- [x] 3.4 实现会话查询、消息快照查询和消息事件查询端点及跨主体访问保护
- [x] 3.5 复用现有停止生成能力实现消息 cancel 端点、主体校验、生成终止和 `cancelled` 可查询终态

## 4. SSE 与验收

- [x] 4.1 在现有 `StreamManager`/继续流能力外增加四类稳定 SSE envelope 适配、sequence、增量 data、最终回答与来源、稳定错误和三种终态
- [x] 4.2 为事件生成单调 `event_id`，实现有限保留、`Last-Event-ID`/`after_event_id` 续接以及过期 cursor 的 `410 + message_snapshot_url`
- [ ] 4.3 覆盖断线重连、过期 cursor 快照恢复、消息重试、空选库、模式冲突、取消竞争、权限中途收回和跨用户会话访问测试
- [x] 4.4 验证 Integration API 与现有 `/api/v1/*` 并存且独立模式列表、搜索和聊天无回归
