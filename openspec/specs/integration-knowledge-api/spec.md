# integration-knowledge-api Specification

## Purpose
TBD - created by archiving change add-integration-knowledge-api. Update Purpose after archive.
## Requirements
### Requirement: Integration API 必须使用独立版本化边界
系统 MUST 在 `/api/integration/v1/*` 提供稳定、版本化的知识库清单与维护、单次及批量 RAG、表格分析和聊天接口；系统 MUST NOT 将内部 `/api/v1/*` DTO 直接作为稳定契约。

#### Scenario: 同域代理访问 Integration API
- **WHEN** 浏览器请求 `/knowledge/api/integration/v1/knowledge-bases`
- **THEN** 网关将请求映射到后端 `/api/integration/v1/knowledge-bases` 且响应符合 Integration DTO

### Requirement: 知识库列表必须只返回授权和稳定字段
系统 MUST 只返回当前主体有效范围内的知识库，并且每项最多公开 `id`、`name`、`description`、`type`、`permission` 和 `status` 等版本化字段。

#### Scenario: 用户列出可用知识库
- **WHEN** 具有 `kb:list` scope 的主体请求知识库列表
- **THEN** 系统只返回 client allowlist 与用户权限交集内的知识库，不包含模型密钥、存储配置或内部治理配置

### Requirement: 授权客户端必须能够维护租户知识
系统 MUST 允许具有 `knowledge:write` scope 的主体创建或删除文档知识库并向已授权知识库上传文件。租户和用户身份 MUST 仅来自认证后的 integration principal，请求体 MUST NOT 覆盖租户身份，上传 MUST 受请求体上限约束，所有变更 MUST 写入审计记录。签发带有 `knowledge:write` 的 service token 时 MUST 绑定并重新校验该 client 配置的有效租户管理员，MUST NOT 以空用户身份产生无法发布的治理草稿。

#### Scenario: 创建并填充租户知识库
- **WHEN** 具有 `knowledge:write` scope 的 service client 创建知识库并向返回的 ID 上传合法文件
- **THEN** 两个资源都在主体绑定的租户内创建，且两个变更都有审计记录

#### Scenario: 向越权知识库上传
- **WHEN** 客户端向不在授权范围内的知识库上传文件
- **THEN** 系统在摄取文件前整次拒绝请求

### Requirement: RAG 搜索必须支持多个授权知识库
系统 MUST 要求请求显式提交至少一个 `knowledge_base_id`，对所有授权知识库执行统一召回和统一重排，并返回全局 Top K；字段缺失、null 或空数组 MUST 返回 `400`，MUST NOT 被解释为全部知识库。

#### Scenario: 搜索两个授权知识库
- **WHEN** 具有 `rag:search` scope 的主体提交两个授权知识库、query 和合法 `top_k`
- **THEN** 系统在两个知识库的候选中统一重排并返回不超过 `top_k` 的结果

#### Scenario: 搜索请求包含越权知识库
- **WHEN** `knowledge_base_ids` 包含任一不在最终有效范围的 ID
- **THEN** 系统整次返回 `403`，不执行部分搜索，并记录被拒绝 ID

#### Scenario: 搜索未选择知识库
- **WHEN** `knowledge_base_ids` 缺失、为 null 或为空数组
- **THEN** 系统返回 `400`，不执行搜索，也不自动使用全部授权知识库

### Requirement: RAG 搜索必须支持有界批量查询
系统 MUST 在 `POST /api/integration/v1/rag/search-batch` 接受共享的非空 `knowledge_base_ids` 和有界 `queries` 数组。每个查询 MUST 携带批内唯一稳定 ID、非空 query，并 MAY 携带该查询的 `knowledge_ids` 与 `top_k`。系统 MUST 在执行任何查询前校验整批参数及知识库授权，以受控并发执行检索，按请求顺序返回逐查询状态和结果；批量请求 MUST 作为一次独立限流操作计费。

#### Scenario: 批量搜索多个业务目标
- **WHEN** 具有 `rag:search` scope 的主体提交多个唯一查询和已授权知识库
- **THEN** 系统以配置化并发执行，并按原顺序返回每个查询 ID、状态和全局 Top K 结果

#### Scenario: 批量请求包含重复查询 ID
- **WHEN** 同一批次包含重复查询 ID、空查询或超过服务端查询数上限
- **THEN** 系统整批返回 `400`，不执行部分检索

#### Scenario: 批量请求包含越权知识库
- **WHEN** 批量请求的共享知识库范围包含任一越权 ID
- **THEN** 系统整批返回 `403`，不执行任何查询

### Requirement: 搜索结果必须可追溯来源
每个搜索结果 MUST 包含 `knowledge_base_id`、`knowledge_base_name`、`knowledge_id`、`chunk_id`、命中内容或摘要、综合得分、文档标题与来源；治理知识还 MUST 包含当前生效版本 ID。

#### Scenario: 返回治理知识命中
- **WHEN** 搜索结果来自启用治理的知识
- **THEN** 响应包含该结果对应的 active 版本 ID 和完整知识库、文档及 chunk 标识

### Requirement: 多知识库限制必须由配置和基准约束
系统 MUST 对知识库数量、请求体、query 长度和 `top_k` 应用服务端限制；默认值和硬上限 MUST 在性能基准后配置化确定，客户端配置只能缩小限制。

#### Scenario: 请求超过服务端多库上限
- **WHEN** 请求中的知识库数量超过当前租户硬上限
- **THEN** 系统返回稳定的参数错误且不开始检索

### Requirement: 聊天会话必须保存授权上限
系统 MUST 要求创建会话时显式指定 `knowledge_base_mode` 为 `selected` 或 `all-allowed`，保存服务端计算的 `allowed_knowledge_base_ids`，并将会话绑定到 `client_id`、租户和外部用户主体。`selected` MUST 携带非空 `knowledge_base_ids`；`all-allowed` MUST NOT 携带知识库数组，且创建后授权上限 MUST NOT 因新增授权自动扩大。

#### Scenario: 创建限定知识库会话
- **WHEN** 用户使用 `selected` 和授权范围的非空子集创建会话
- **THEN** 系统保存服务端授权上限和默认选择，且其他 client 或用户不能读取或续接该会话

#### Scenario: 创建 all-allowed 会话
- **WHEN** 用户显式使用 `all-allowed` 且不提交知识库数组
- **THEN** 系统将创建时全部有效知识库保存为会话授权上限，后续只允许该上限缩小而不自动扩大

#### Scenario: 会话选择参数不合法
- **WHEN** 模式缺失、模式未知、`selected` 没有非空数组、`all-allowed` 携带数组，或数组为 null 或空
- **THEN** 系统返回 `400` 且不创建会话

### Requirement: 每轮消息必须重新授权并保存知识库快照
对于 `selected` 会话，系统 MUST 要求每轮消息携带非空 `selected_knowledge_base_ids`；对于 `all-allowed` 会话，系统 MUST 要求消息省略该字段并使用会话授权上限。系统 MUST 在每轮与当前 client allowlist 和用户实时权限求交，并 MUST 保存最终实际知识库 ID 快照。缺失、null、空数组或与模式冲突的字段 MUST 返回 `400`；显式包含越权 ID MUST 返回 `403`。

#### Scenario: 用户在会话中切换授权知识库
- **WHEN** 用户下一轮选择会话授权上限内的不同知识库子集
- **THEN** 系统使用新子集回答并将该轮实际范围保存到消息记录

#### Scenario: selected 会话发送空选择
- **WHEN** `selected` 会话的消息缺失 `selected_knowledge_base_ids`、值为 null 或空数组
- **THEN** 系统返回 `400`，不生成回答，也不回退为默认或全部知识库

#### Scenario: all-allowed 会话发送消息
- **WHEN** `all-allowed` 会话的消息省略 `selected_knowledge_base_ids`
- **THEN** 系统使用会话授权上限与当前权限的交集生成回答并记录实际快照

#### Scenario: 会话期间权限被收回
- **WHEN** 用户发送下一轮消息时所选知识库权限已被收回
- **THEN** 系统返回 `403`，不生成部分回答，也不复用创建会话时的旧权限

### Requirement: 会话和消息创建必须支持幂等
系统 MUST 接受 `Idempotency-Key`，并按主体、端点和有效载荷防止重试产生重复会话、消息或回答。

#### Scenario: 相同请求重试
- **WHEN** 同一主体以相同 `Idempotency-Key` 和相同有效载荷重试创建消息
- **THEN** 系统返回原消息标识和状态，不创建第二条消息或回答

#### Scenario: 幂等键载荷冲突
- **WHEN** 同一主体重复使用 `Idempotency-Key` 但提交不同有效载荷
- **THEN** 系统返回稳定冲突错误且不处理第二个载荷

### Requirement: 流式聊天必须使用稳定且可续接的 SSE 事件
系统 MUST 对外提供 `message.created`、`answer.delta`、`answer.completed` 和 `error` 事件；每个事件 MUST 使用包含单调递增 `event_id`、`event`、`session_id`、`message_id`、`sequence`、`occurred_at` 和 `data` 的统一 envelope，并支持 `Last-Event-ID` 或 `after_event_id` 续接。`answer.delta.data` MUST 包含增量文本，`answer.completed.data` MUST 包含 `completed` 终态、最终回答和来源快照，`error.data` MUST 包含稳定错误码、可重试标记和 `failed` 或 `cancelled` 终态。

#### Scenario: SSE 中途断线后续接
- **WHEN** 客户端以已接收的最后 `event_id` 重新连接
- **THEN** 系统从下一个可用事件继续发送，不重复已确认片段

#### Scenario: 内部 pipeline 产生额外事件
- **WHEN** 内部聊天流程包含未纳入 Integration API 的事件类型
- **THEN** 系统不要求外部客户端理解该内部事件，并保持四类稳定事件契约

### Requirement: 聊天状态必须可通过快照和事件接口恢复
系统 MUST 提供会话查询、消息快照查询和消息事件查询接口。事件 cursor 超出保留期时，系统 MUST 返回 `410 Gone` 和当前主体有权访问的 `message_snapshot_url`；客户端 MUST 能通过快照取得 `completed`、`failed` 或 `cancelled` 终态，而无需重新提交消息。

#### Scenario: 查询消息最终快照
- **WHEN** 会话所有者查询已结束消息
- **THEN** 系统返回最终状态、回答、来源快照、错误或取消信息以及最后事件 ID

#### Scenario: 事件 cursor 已过期
- **WHEN** 客户端使用早于事件保留窗口的 `after_event_id` 续接
- **THEN** 系统返回 `410 Gone` 和消息快照地址，不重新生成回答

#### Scenario: 其他主体查询消息快照
- **WHEN** 其他 client、租户或用户查询该会话、消息或事件
- **THEN** 系统拒绝访问且不暴露消息是否存在或其内容

### Requirement: 生成中的消息必须可取消
系统 MUST 提供消息级 cancel 接口，且只有绑定会话的同一 client、租户和用户主体可取消生成。

#### Scenario: 会话所有者取消生成
- **WHEN** 会话所有者取消仍在生成的消息
- **THEN** 系统停止后续生成、保存 `cancelled` 终态并通过 SSE 和消息快照查询暴露该状态

#### Scenario: 其他用户尝试取消
- **WHEN** 其他用户或 client 提交同一消息的 cancel 请求
- **THEN** 系统返回未授权错误且不影响原生成任务

### Requirement: Integration API 必须独立审计和限流
系统 MUST 分别对列表、搜索、会话创建、消息和取消接口限流，并 MUST 记录 client、租户、用户、知识库范围、操作结果及拒绝原因。

#### Scenario: 搜索超过限流
- **WHEN** 主体超过 RAG 搜索速率限制
- **THEN** 系统返回稳定限流错误，不开始召回，并写入不含敏感内容的审计记录
