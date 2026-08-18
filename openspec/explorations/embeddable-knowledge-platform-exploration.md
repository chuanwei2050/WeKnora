# WeKnora 外挂知识库与模块化接入架构基线

> 状态：架构决策基线，供后续 OpenSpec proposal 使用，尚未进入实现阶段
> 日期：2026-08-18
> 目标：让 WeKnora 在保持独立运行的同时，能够作为其他项目的知识库管理、RAG 搜索和聊天模块接入。

## 1. 背景与目标

WeKnora 当前是一套可以独立部署和运行的知识库与问答系统。后续目标不是把整套源码编译进其他项目，而是把它建设成一个可独立部署、通过稳定契约接入的“知识能力服务”。

目标接入能力包括：

1. 其他项目在自己的左侧菜单增加“知识库”，点击后在宿主项目内容区显示 WeKnora 知识库页面。
2. 其他项目可在任意页面挂载悬浮图标，展开后显示聊天框。
3. 向其他项目提供知识库列表、RAG 搜索和聊天接口，并支持选择多个知识库作为数据来源。
4. 建立清晰的跨项目认证、租户隔离、知识库授权、用户角色和文档审批机制。
5. WeKnora 的独立运行模式和嵌入运行模式共用同一套后端领域能力，避免形成两套产品。

## 2. 第一性原则与边界

### 2.1 核心定位

WeKnora 应作为独立部署的知识能力服务，而不是其他项目内部的源码模块。

其他项目只依赖三类契约：

- 页面契约：知识库管理页面和聊天 Widget。
- API 契约：知识库列表、RAG 搜索、会话和流式聊天。
- 身份契约：宿主身份交换、服务客户端认证和资源授权。

### 2.2 不做的事情

- 不要求所有宿主项目采用 Vue、Vite 或 TDesign。
- 不把 WeKnora 的内部数据库模型直接作为开放 API DTO。
- 不把租户 API Key 暴露给浏览器。
- 不以客户端传入的知识库 ID 作为授权依据。
- 不因为采用嵌入模式而绕过后端权限校验。
- V1 不引入微前端运行时和共享前端依赖。

## 3. 现状与改造基线

### 3.1 已有基础能力

- 已有知识库列表接口：`GET /api/v1/knowledge-bases`。
- 已有无会话 RAG 搜索接口：`POST /api/v1/knowledge-search`。
- RAG 搜索请求已支持 `knowledge_base_ids: string[]`。
- 普通知识问答和 Agent 问答已支持多知识库与 SSE 流式输出。
- 前端设置状态和知识库选择器已使用知识库 ID 数组。
- 聊天页面已有 `embeddedMode`，但目前主要用于内部 Wiki 场景，并不是通用外部嵌入协议。
- 已有 BidReview 专用 SSO、嵌入模式和角色控制，可作为通用方案的实现经验。
- 已有知识治理版本状态：`draft`、`pending_review`、`approved`、`indexing`、`active`、`rejected` 等。
- 已有提交者不能审核自己版本的职责分离校验。

### 3.2 当前限制

- BidReview 的路径、角色、Secret 和前端逻辑均为业务专用，不能直接作为通用协议。
- 当前租户 API Key 是租户级单密钥，没有项目级 scope、知识库 allowlist、独立吊销和轮换能力。
- 当前知识库列表返回内部完整模型，不适合作为稳定的开放 API DTO。
- 当前前端仅通过 `VITE_APP_BASE_PATH` 配置路由和资源前缀，API 与文件地址仍需要补充独立的 base path 配置。
- Nginx 当前设置 `X-Frame-Options: SAMEORIGIN`，不同域名不能直接嵌入。
- 后端 CORS 当前使用通配来源并允许凭证，正式开放前应改为配置化来源白名单。
- 当前 `Knowledge` 没有文档创建者字段，无法完整表达“普通成员只能管理自己的文档”。
- 当前普通成员可以读取同租户知识库，但上传、编辑和治理操作通常依赖知识库管理权限。
- 当前上传接口要求 `OrgRoleAdmin`，尚未形成“普通成员投稿、审核员审批后入库”的协作模型。
- 当前治理版本的创建、提交和审批均使用知识库管理权限；虽然有状态机和禁止自审，但投稿者与审核员权限尚未拆开。

## 4. 总体目标架构

```text
┌─────────────────────────────────────────────────────────────┐
│                        其他业务项目                           │
│                                                             │
│  左侧知识库菜单 ──▶ 宿主 /knowledge 页面 ──▶ 管理页 iframe  │
│  任意业务页面 ───────────────────────────▶ 悬浮聊天 Widget  │
│  业务后端/BFF ───────────────────────────▶ 集成 API         │
└──────────────────────────────┬──────────────────────────────┘
                               │ 同域反向代理
┌──────────────────────────────▼──────────────────────────────┐
│                           WeKnora                            │
│                                                             │
│  嵌入页面  │ Widget 页面 │ Integration API │ 身份与授权     │
│                         ↓                                   │
│        知识库、文档治理、检索、RAG、聊天、模型服务          │
└─────────────────────────────────────────────────────────────┘
```

其他项目负责：

- 左侧菜单和页面外壳。
- 用户登录和组织上下文。
- 决定允许接入哪些知识库和功能。
- 加载知识库 iframe 和聊天 Widget。

WeKnora 负责：

- 知识库管理内容页。
- 文档贡献、审批和发布。
- 聊天 UI 与会话管理。
- 多知识库 RAG。
- 服务端二次权限校验、审计和限流。

## 5. 页面接入决策

### 5.1 决策

V1 固定采用：

> 同域反向代理 + 宿主内容区 iframe + WeKnora `embedded` 模式。

反向代理负责统一页面、API、SSE 和文件请求的域名与路径；iframe 负责把 WeKnora 页面呈现在宿主内容区。二者职责不同，必须同时使用。

该方案满足以下约束：

- 保留宿主左侧菜单和顶部导航。
- WeKnora 可独立部署、发布和回滚。
- 不限制宿主技术栈。
- 隔离复杂样式、预览组件和运行时依赖。
- 将集成边界稳定在 URL、消息协议和 API，而不是前端内部组件。

这里的“隔离”仅指 CSS、DOM 渲染上下文和 JavaScript 故障传播隔离，不代表同域安全隔离。安全边界仍由后端认证、资源授权和最小权限会话提供。

### 5.2 未采用的方案

- 微前端：当前 WeKnora 是完整的 Vue 3 + Vite SPA，且目标宿主可能使用不同技术栈。引入微前端需要额外维护依赖共享、路由、全局样式、弹层挂载、卸载清理和联合回归契约，V1 收益不足以覆盖成本。
- 整页跳转：无法在保留宿主菜单和顶部导航的同时提供一体化知识管理体验，不满足页面接入目标。

只有当宿主技术栈、组件库和发布节奏统一，并明确要求共享 DOM、路由和主题时，才重新评估微前端。重新评估只替换页面容器，不改变同域代理、Integration API、身份和授权边界。

## 6. 同域反向代理与显示方式

### 6.1 推荐路径

为避免宿主页与 WeKnora 页面路径冲突：

```text
/knowledge
    宿主项目自己的知识库路由页面

/knowledge/embed/*
    反向代理到 WeKnora 前端

/knowledge/api/*
    反向代理到 WeKnora 后端

/knowledge/files?file_path=...
    精确反向代理到 WeKnora /files?file_path=...
```

示例：

```text
宿主菜单地址：
https://project.example.com/knowledge

宿主内容区 iframe：
https://project.example.com/knowledge/embed/platform/knowledge-bases?mode=embedded
```

### 6.2 网关示例

```nginx
server {
    listen 443 ssl;
    server_name project.example.com;

    location ^~ /knowledge/api/ {
        proxy_pass http://weknora-backend:8080/api/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        proxy_http_version 1.1;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }

    location = /knowledge/files {
        proxy_pass http://weknora-backend:8080/files;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location ^~ /knowledge/embed/ {
        proxy_pass http://weknora-frontend/;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Prefix /knowledge/embed;
    }

    location / {
        proxy_pass http://other-project-frontend;
    }
}
```

SSE 路由必须关闭代理缓冲并设置足够长的读取超时；WebSocket 或语音功能还需要转发 `Upgrade` 和 `Connection`。

### 6.3 WeKnora 前端配置要求

目标配置应支持：

```text
VITE_APP_BASE_PATH=/knowledge/embed/
VITE_API_BASE_URL=/knowledge
VITE_FILES_BASE_PATH=/knowledge/files
```

当前只有页面 base path 能力，API 和文件 base path 需要新增配置。

### 6.4 宿主页面显示

宿主自己的左侧菜单增加“知识库”，菜单跳转到宿主路由 `/knowledge`。宿主路由保留自己的菜单和顶部导航，只在内容区域放置 iframe：

```html
<iframe
  src="/knowledge/embed/platform/knowledge-bases?mode=embedded"
  title="知识库"
  style="width:100%;height:calc(100vh - 64px);border:0;display:block"
></iframe>
```

WeKnora `embedded` 模式需要：

- 隐藏 WeKnora 自己的侧边栏、Logo、租户切换和用户菜单。
- 保留知识库列表、详情、文档贡献、审批和配置内容。
- 登录失效时通知宿主，而不是跳转到独立登录页。
- 支持宿主传入语言、主题和初始知识库 ID。
- 路由变化、文档发布和未授权事件通过约定协议通知宿主。

### 6.5 页面通信协议

协议应保持最小化：

```text
宿主 → WeKnora
- auth-ready
- set-theme
- set-locale
- open-knowledge-base

WeKnora → 宿主
- ready
- unauthorized
- route-change
- document-published
```

通信必须校验 `origin`，并使用明确的 `targetOrigin`，不能使用 `*`。

## 7. 悬浮聊天 Widget

### 7.1 形态

对宿主暴露一个很薄的 JS SDK 或 Web Component：

```html
<weknora-chat-widget
  api-base="/knowledge/api"
  position="bottom-right">
</weknora-chat-widget>
```

外壳负责：

- 悬浮按钮。
- 展开和收起。
- 创建聊天 iframe。
- 认证握手和宿主事件通知。

聊天 UI 运行在 iframe 内，避免 Markdown、Mermaid、代码高亮、文档预览、语音、附件和 TDesign 弹层污染宿主。

### 7.2 Widget 模式

Widget 页面仅显示：

- 标题栏。
- 知识库多选。
- 消息列表。
- 输入框、附件和停止生成等必要能力。

不显示知识库管理、Agent 管理、组织管理和系统设置。

### 7.3 知识库选择模式

建议支持：

- `fixed`：宿主固定一组知识库，用户不能修改。
- `selectable`：用户在服务端允许范围内多选。
- `all-allowed`：显式使用会话创建时冻结的授权上限，并在每轮与当前实时权限求交。

默认采用 `selectable`。首版可将 5 个知识库作为性能基准的候选上限，但最终限制必须由召回规模、重排候选数、响应延迟和并发测试确定，不能把 5 直接写成未经验证的产品常量。宿主配置和前端选项只能缩小权限，不能扩大服务端授权。

## 8. 集成 API 与多知识库检索

### 8.1 稳定 API 边界

不建议外部项目长期依赖内部 `/api/v1` 数据结构。建议新增：

```http
GET  /api/integration/v1/knowledge-bases
POST /api/integration/v1/rag/search
POST /api/integration/v1/chat/sessions
GET  /api/integration/v1/chat/sessions/{id}
POST /api/integration/v1/chat/sessions/{id}/messages
GET  /api/integration/v1/chat/sessions/{id}/messages/{message_id}
GET  /api/integration/v1/chat/sessions/{id}/messages/{message_id}/events
POST /api/integration/v1/chat/sessions/{id}/messages/{message_id}/cancel
```

经同域代理后可呈现为：

```text
/knowledge/api/integration/v1/knowledge-bases
/knowledge/api/integration/v1/rag/search
```

路径映射固定为：

```text
浏览器 /knowledge/api/integration/v1/*
    → Nginx proxy_pass .../api/
    → WeKnora /api/integration/v1/*
```

因此后端 Integration API 必须注册在 `/api/integration/v1/*`，所有后端路由都不得省略 `/api` 前缀。

### 8.2 知识库列表 DTO

只返回外部需要的稳定字段：

```json
{
  "id": "kb-a",
  "name": "产品知识库",
  "description": "产品说明和使用规范",
  "type": "document",
  "permission": "viewer",
  "status": "ready"
}
```

不得返回模型密钥、存储配置或内部治理配置等敏感字段。

### 8.3 RAG 搜索请求

```json
{
  "query": "合同违约责任是什么？",
  "knowledge_base_ids": ["kb-a", "kb-b"],
  "top_k": 10
}
```

### 8.4 多知识库策略

V1 采用：

> 多知识库统一召回、统一重排、返回全局 Top K。

优点是复用现有管线、语义一致、实现成本低。风险是某个强势知识库可能占满结果。

如后续业务要求每个知识库都有结果，可增加：

```json
{
  "retrieval_mode": "balanced",
  "per_knowledge_base_top_k": 3
}
```

V1 不提前实现 `balanced`。

### 8.5 搜索结果要求

每个结果至少应包含：

- `knowledge_base_id`
- `knowledge_base_name`
- `knowledge_id`
- `chunk_id`
- 内容摘要或命中内容
- 综合得分
- 可追溯的文档标题和来源
- 当前生效版本 ID（治理启用时）

### 8.6 聊天会话、知识库范围与流式协议

聊天会话必须显式选择 `selected` 或 `all-allowed` 模式，并在创建时保存服务端计算出的 `allowed_knowledge_base_ids` 授权上限。`selected` 创建时必须携带非空 `knowledge_base_ids`；`all-allowed` 不得携带知识库数组，且授权上限不能因后续新增授权自动扩大。模式缺失、未知、字段为 null、空数组或字段组合冲突均返回 `400`，不得把空值解释为全部。

`selected` 会话每轮消息必须携带非空 `selected_knowledge_base_ids`；`all-allowed` 会话必须省略该字段并使用冻结的授权上限。规则为：

```text
本轮实际知识库
= selected_knowledge_base_ids
∩ 会话 allowed_knowledge_base_ids
∩ 当前客户端 allowlist
∩ 当前用户实时权限
```

- 每轮都重新校验权限，不能只依赖创建会话时的快照。
- 消息记录必须保存该轮实际使用的知识库 ID 快照，保证历史回答可追溯。
- 请求包含越权知识库时整轮返回 `403`，不执行部分搜索。
- 会话必须绑定 `client_id`、租户和外部用户主体，其他客户端或用户不能读取、续接或取消该会话。
- 创建会话和发送消息支持 `Idempotency-Key`，防止宿主重试产生重复会话或重复回答。

SSE 对外只承诺稳定的集成事件，不暴露内部 pipeline 事件名称。V1 建议事件：

```text
message.created
answer.delta
answer.completed
error
```

每个事件使用统一 envelope，包含单调递增的 `event_id`、`session_id`、`message_id`、事件类型和数据。客户端可通过 `Last-Event-ID` 或显式 `after_event_id` 续接；事件 cursor 超出保留期时，服务端返回 `410 Gone` 和 `message_snapshot_url`，客户端查询消息快照且不得重发消息。取消生成使用独立的 cancel 接口，消息终态固定为 `completed`、`failed` 或 `cancelled`。内部检索步骤如需展示，应通过版本化的可选事件扩展，不能让外部客户端依赖当前内部流水线结构。

## 9. 跨项目身份与权限

### 9.1 租户映射

默认规则：

> 外部项目的一个组织或工作空间，对应一个 WeKnora 租户。

租户是数据隔离边界，用户角色和资源授权决定租户内部可以执行哪些操作。

一套 WeKnora 部署可以同时服务多个外部项目：相互独立的项目绑定不同租户；同一组织下需要共享知识库的多个项目可以绑定同一租户，但必须使用不同的集成客户端和独立 allowlist。集成客户端与租户在服务端静态绑定，外部请求不能通过 `X-Tenant-ID` 任意切换租户。

### 9.2 交互用户与服务调用分离

交互用户统一采用一次性 bootstrap ticket，不在 URL 中携带长期 Token：

```text
1. 宿主后端验证本项目登录态
2. 宿主后端用 integration client 凭证请求 WeKnora bootstrap ticket
3. WeKnora 映射租户、外部用户、角色、scopes 和知识库 allowlist
4. WeKnora 返回 60 秒有效、只能使用一次的随机 ticket
5. 宿主父页面等待 iframe ready 后，通过指定 targetOrigin 的 postMessage 发送 ticket
6. iframe 调用浏览器侧 `/knowledge/api/integration/v1/auth/exchange`，经代理映射到后端 `/api/integration/v1/auth/exchange`，原子消费 ticket
7. WeKnora 建立嵌入会话并设置限定 Path 的 HttpOnly Cookie，同时返回 CSRF Token
```

嵌入会话 Cookie 必须设置 `HttpOnly`、`Secure`、`SameSite=Lax`，其 `Path` 由外部 base path 配置决定；同域代理场景默认限定为 `/knowledge/`，避免与宿主其他页面共享。修改类请求同时校验 `X-CSRF-Token`。

建议 ticket 有效期为 60 秒且只能消费一次；嵌入访问会话短期有效，建议 15 分钟滚动有效期，最长续期 8 小时。续期时必须重新校验客户端状态、租户状态、用户角色和知识库授权。宿主退出、客户端撤销或用户停用后，应通过注销/撤销接口使会话失效；高风险管理操作不得只依赖陈旧的会话权限快照。

ticket 只存储服务端哈希和唯一 `jti`，消费时原子标记为已使用，以防重放。外部身份唯一键固定为：

```text
(identity_provider_id, external_tenant_id, external_user_id)
```

需要共享用户身份的多个集成客户端必须绑定同一个 `identity_provider_id`；没有共同身份源的项目默认使用各自独立的 provider ID。不同身份源中相同的外部用户 ID 不得自动视为同一用户。

服务间调用：

```text
宿主后端
  → 通过 TLS 使用 client_id + secret 获取短期 service token
  → service token 的 audience 固定为 weknora-integration
  → 使用 service token 调用 WeKnora 集成 API
```

客户端 Secret 只允许宿主后端持有，数据库只保存其哈希并支持双密钥轮换。浏览器不得持有集成客户端 Secret、service token 或租户 API Key。

建议认证端点：

```http
POST /api/integration/v1/auth/token
POST /api/integration/v1/auth/bootstrap
POST /api/integration/v1/auth/exchange
POST /api/integration/v1/auth/refresh
POST /api/integration/v1/auth/logout
```

### 9.3 集成客户端

每个接入项目独立配置：

- `client_id`
- Secret 哈希
- 所属租户
- `identity_provider_id`
- 外部角色映射与最高可授予角色
- 精确允许的浏览器来源
- 允许的知识库 ID
- scopes
- 状态和过期时间
- 创建、轮换、撤销时间

建议 scopes：

- `kb:list`
- `kb:read`
- `rag:search`
- `chat:create`
- `chat:stream`
- `kb:contribute`
- `kb:review`
- `kb:manage`

外部角色声明只能作为映射输入，最终角色必须由服务端映射并受客户端最高可授予角色、scopes 和知识库 allowlist 共同限制，不能直接信任宿主传入管理员角色。

### 9.4 最终资源范围

每次请求的有效知识库范围必须是：

```text
请求选择的知识库
∩ 集成客户端允许范围
∩ 当前用户允许范围
∩ 当前租户或显式共享关系允许范围
```

如果请求包含未授权知识库，开放 API 应返回 `403` 并记录拒绝原因和知识库 ID，不应静默忽略或返回部分结果。

### 9.5 跨租户共享

V1 不向集成客户端开放隐式跨租户共享。集成客户端默认只能访问所属租户内显式授权的知识库。

后续若需要平台公共知识库，应通过显式资源授权关系纳入 allowlist，而不是授予客户端任意跨租户查询能力。

## 10. 租户内部的知识库与文档权限

### 10.1 核心修正

知识库管理权和文档贡献权必须分开：

- 知识库由某个用户创建，不代表同租户普通成员不能贡献文档。
- 普通成员可以在知识库贡献策略允许时，向同租户知识库提交自己的文档。
- 普通成员只能管理自己的草稿、被驳回文档和待提交版本。
- 对治理已启用的知识库，审核通过并激活前，候选版本不能被 RAG 或聊天召回。

### 10.2 知识库贡献策略

每个知识库增加 `contribution_mode`：

```text
closed
    仅知识库管理员可上传和修改内容

members
    同租户普通成员可以贡献自己的文档

allowlist
    仅显式授权的用户或角色可以贡献
```

- 历史知识库迁移时默认 `closed`，保持现有上传权限不变。
- 新建知识库必须显式选择贡献策略；请求未提供时默认 `closed`，避免静默放宽权限。
- `members` 和 `allowlist` 必须启用知识治理与审批；如果治理未启用，普通成员投稿应被拒绝，管理员仍沿用当前直接上传流程。
- `contribution_mode` 只授予贡献自己的文档或版本的能力，不授予编辑他人文档、修改知识库配置或审批的能力。

### 10.3 推荐权限矩阵

| 操作 | 普通成员 | 审核员 | 知识库管理员 |
|---|---|---|---|
| 查看、搜索、聊天 | 是 | 是 | 是 |
| 向同租户知识库上传文档 | 贡献策略允许时 | 贡献策略允许时 | 是 |
| 编辑自己的草稿或驳回文档 | 是 | 是 | 是 |
| 删除自己的未发布文档 | 是 | 是 | 是 |
| 提交自己的文档审批 | 是 | 是 | 是 |
| 编辑其他人的文档 | 否 | 可选 | 是 |
| 审批其他人的提交 | 否 | 是 | 是 |
| 审批自己的提交 | 否 | 否 | 否 |
| 修改知识库配置、删除知识库 | 否 | 否 | 是 |
| 清空知识库全部内容 | 否 | 否 | 是 |

### 10.4 文档生命周期

```text
成员上传
  → draft
  → pending_review
  → approved
  → indexing
  → active
```

驳回流程：

```text
pending_review
  → rejected
  → 原提交者编辑
  → pending_review
```

`pending_review` 内容冻结，不允许直接编辑或删除。提交者可以在审核决定前显式 `withdraw` 回到 `draft`；撤回与审核决定必须原子竞争，只有一个状态转换成功，并记录完整审计。进入 `approved`、`indexing`、`active` 或 `rejected` 后不得撤回。

“上传”只表示候选文档已存储。对治理已启用的知识库，文件可以提前解析并写入待发布索引，但只有当前 `active` 且处于有效期内的版本能进入正式检索范围。

对于未启用治理的知识库和 `current_version_id` 为空的历史知识，继续沿用现有 `parse_status`、`enable_status` 和索引可见性规则，不要求补造 `active` 版本，也不能因为引入投稿审批而停止现有检索。

### 10.5 修改已发布文档

不能直接覆盖线上内容：

```text
当前 active 版本继续提供检索
       +
贡献者创建新的 pending 版本
       ↓
审核和索引成功
       ↓
原子切换为新的 active 版本
```

新版本审批或索引失败时，旧版本仍可正常检索。

### 10.6 所需数据

当前治理版本已有 `created_by`，但 `Knowledge` 缺少文档最初贡献者。建议增加：

```text
knowledge.created_by
    文档最初贡献者

knowledge_version.created_by
    当前版本提交者，现有字段

knowledge_version_review.reviewer_id
    审核人，现有 review 记录可表达
```

权限判断应从“能管理知识库才能改文档”调整为：

```text
上传：知识库管理员，或同租户成员且 `contribution_mode` 允许、治理与审批已启用

编辑草稿：
文档或版本提交者本人，或者知识库管理员

审批：
审核员或管理员，并且审核人 != 提交人

检索：
治理知识只允许当前 active 且处于有效期内的版本；非治理和历史知识沿用现有可见性规则
```

## 11. 独立模式与嵌入模式

建议前端明确支持三种运行模式：

### 11.1 `standalone`

- 完整侧边栏和用户菜单。
- 独立登录和租户切换。
- 可访问全部管理功能。

### 11.2 `embedded-page`

- 隐藏自身应用外壳。
- 由宿主提供菜单、标题和返回逻辑。
- 保留完整知识库管理、贡献和审批内容。

### 11.3 `embedded-widget`

- 只保留聊天相关内容。
- 禁止进入管理、组织和系统设置页面。
- 知识库范围受宿主配置和服务端授权限制。

三种模式必须共用同一套后端权限，不以 UI 是否显示按钮作为安全边界。

### 11.4 独立模式兼容性与非回归约束

外挂能力必须以“新增运行模式”的方式实现，不能把现有独立应用改造成只能依附宿主运行的子应用。`standalone` 始终是默认模式；未显式传入嵌入配置、未启用集成功能时，系统行为应与当前独立部署保持兼容。

兼容性要求：

- 原有 `/platform/*` 页面路由、独立登录、租户切换和完整应用外壳保持可用。
- 原有 `/api/v1/*` 和 `/files?file_path=...` 继续作为独立部署的默认接口，不被 Integration API 替换。
- `/api/integration/v1/*`、嵌入页面路径和同域代理前缀均为新增能力。
- 独立部署仍可使用根路径 `/`、API 路径 `/api/v1` 和文件路径 `/files`，不强制配置 `/knowledge/*` 前缀。
- 嵌入模式只改变页面外壳、认证入口和宿主通信，不复制或分叉知识库、文档治理、RAG、聊天等业务逻辑。
- 新的文档贡献和审核权限应拆成独立权限，不改变现有“管理知识库”权限的含义，也不削弱现有管理员能力。
- 不能通过直接放宽 `CanManageKnowledgeBase` 来允许普通成员投稿；应新增“贡献自己的文档”“审核文档”“管理知识库”等明确权限。
- 同一发布物应同时支持独立和嵌入模式；如需不同 base path，可通过构建或运行时配置选择，而不是维护两套前端源码。
- 自动化验收必须覆盖独立登录、知识库管理、上传、搜索、聊天、文件预览以及嵌入页面和 Widget，防止只验证嵌入场景。

硬性验收标准：

> 未启用集成功能时，现有独立部署的页面、登录、知识库管理、文档上传、搜索、聊天、文件访问和内部 API 行为保持兼容。

## 12. 安全与运维要求

- API、页面和文件请求使用同域代理，但后端仍必须执行完整认证与授权。
- 同域 iframe 提供样式和运行时隔离，但不是安全隔离边界；不得依赖 iframe 阻止宿主读取同源数据。
- 同域代理场景的 CSP `frame-ancestors` 默认使用 `'self'`；只有明确支持跨域嵌入时才使用配置化宿主来源白名单。
- iframe `sandbox` 只能作为纵深防御；一旦启用 `allow-same-origin` 就不能把它视为父子页面之间的安全边界，所需 flags 必须经过下载、文件选择、预览和语音功能兼容测试。
- CORS 使用配置化白名单，不使用 `*` 与凭证组合。
- iframe 消息校验 `origin`、消息类型和负载结构。
- Token 不放入 URL、查询参数或可长期读取的日志。
- 集成 Secret 只保存在宿主后端，并支持轮换和撤销。
- 对列表、搜索、聊天和上传分别限流。
- 审计记录至少包括客户端、租户、用户、scope、知识库、操作、结果和拒绝原因。
- SSE 代理关闭缓冲并配置长连接超时。
- WebSocket 路由转发升级头。
- 文件下载和预览必须复用知识库和文档权限校验。
- 所有客户端传入的知识库和文档 ID 均视为不可信输入。

## 13. 已确认决策

1. WeKnora 保持独立部署，不作为源码包嵌入宿主。
2. 默认使用同主域名反向代理。
3. 宿主自己的 `/knowledge` 页面保留宿主菜单，在内容区加载 WeKnora iframe。
4. 知识管理页面采用 iframe，不在 V1 引入微前端运行时。
5. 悬浮聊天采用 JS SDK 或 Web Component 外壳，内部使用 iframe。
6. 开放稳定的 Integration API，不直接承诺内部 `/api/v1` DTO 为长期契约。
7. 多知识库搜索 V1 采用统一召回、统一重排、全局 Top K。
8. 外部组织或工作空间默认一对一映射到 WeKnora 租户。
9. 交互用户 SSO 与服务间客户端认证分离。
10. 集成客户端使用 scopes 和知识库 allowlist。
11. V1 不开放隐式跨租户访问。
12. 同租户普通成员可以在知识库 `contribution_mode` 允许且治理已启用时，向他人创建的知识库贡献自己的文档。
13. 普通成员只能编辑和删除自己的未发布文档或版本。
14. 审核员或管理员可以审批，但提交人不能审核自己的版本。
15. 治理知识只有当前 `active` 且处于有效期内的版本可以被 RAG 和聊天召回；非治理和历史知识保持现有可见性规则。
16. 外挂能力通过新增运行模式和 Integration API 提供；`standalone` 保持默认，且必须通过独立模式非回归验收。
17. 交互式嵌入统一采用一次性 bootstrap ticket，经 `postMessage` 交付并交换为限定路径的 HttpOnly 会话。
18. 聊天会话必须显式使用 `selected` 或 `all-allowed`；前者每轮提交非空选择，后者使用创建时冻结的授权上限；两者都重新鉴权并记录该轮实际知识库快照。
19. 共享身份源的多个集成客户端使用共同的 `identity_provider_id` 映射同一外部用户；无共同身份源时默认隔离。
20. 待审版本内容冻结，提交者只能在审核决定前显式撤回到草稿，撤回与审核决定原子竞争。

## 14. 实施分期

### 阶段一：身份和授权基础

- 集成客户端、Secret 哈希、scope、来源白名单和知识库 allowlist。
- 通用用户身份交换和租户映射。
- 统一请求主体和后端资源授权。
- 审计、限流和密钥轮换。

### 阶段二：稳定集成 API

- 知识库列表 DTO。
- 多知识库 RAG 搜索。
- 外部会话和流式聊天。
- 明确的 `403` 和错误契约。

### 阶段三：文档贡献与审批

- 文档创建者字段。
- 普通成员上传和管理自己的草稿。
- 审核员角色与审批权限。
- 审批、索引、激活的完整状态流转。
- 旧 active 版本持续可用和原子切换。

### 阶段四：嵌入管理页

- `standalone`、`embedded-page`、`embedded-widget` 模式。
- 页面、API 和文件 base path 配置。
- 宿主通信协议。
- 同域代理参考配置。

### 阶段五：聊天 Widget

- JS SDK 或 Web Component。
- 固定、多选和全部允许三种知识库模式。
- 多页面会话保持。
- 宿主事件和错误处理。

## 15. OpenSpec proposal 拆分

不要把该架构基线直接落成一个“大而全”的 change。将“外挂知识库平台”作为项目级 initiative，按以下五个可独立验收的 change 推进：

1. `add-integration-client-authz`
   - 范围：接入方身份、服务端租户绑定、外部用户映射、一次性引导票据、会话 Cookie、服务令牌、来源白名单、知识库授权范围和审计主体。
   - 验收重点：客户端不能伪造租户；票据只能兑换一次；浏览器与服务端接口分别通过 CSRF、audience 和授权范围校验。
2. `add-knowledge-contribution-review`
   - 范围：知识库投稿策略、文档级所有权、草稿/审批/发布/驳回、版本切换与旧知识库兼容。
   - 验收重点：普通成员只能在允许投稿的知识库中维护自己的待审内容；未经批准的版本不可被检索；未启用治理的旧知识库行为不变。
3. `add-integration-knowledge-api`
   - 范围：知识库列表、RAG 搜索、流式聊天、会话知识库授权上限、逐轮选择快照、幂等、断线续传、取消、限流和错误码。
   - 依赖：`add-integration-client-authz`。
   - 验收重点：所有接口只返回当前主体被授权的数据；多知识库检索可追踪来源；重试、重连和取消不会产生重复或越权结果。
4. `add-embedded-knowledge-management`
   - 范围：宿主菜单契约、`/knowledge/*` 同域入口、反向代理、iframe 集成、登录引导和独立运行兼容。
   - 依赖：`add-integration-client-authz`、`add-integration-knowledge-api`、`add-knowledge-contribution-review`。
   - 验收重点：宿主入口与 WeKnora 独立入口都可用；代理路径、Cookie Path、前端基路径和 API 路径一致；独立部署原有入口无回归。
5. `add-floating-chat-widget`
   - 范围：SDK/组件初始化、悬浮图标、聊天面板、知识库多选、主题、页面生命周期和错误恢复。
   - 依赖：`add-integration-client-authz`、`add-integration-knowledge-api`。
   - 验收重点：组件只能选择授权范围内知识库；每轮请求携带并记录实际选择；刷新、切页、断网重连和销毁行为可预测。

依赖关系如下：

```text
add-integration-client-authz ─┬─> add-integration-knowledge-api ─┬─> add-floating-chat-widget
                              │                                  └─> add-embedded-knowledge-management
add-knowledge-contribution-review ─────────────────────────────────> add-embedded-knowledge-management
```

其中前两个 change 可以分别设计；进入页面与聊天组件实现前，必须先稳定认证授权和 Integration API 契约。每个 proposal 都应明确 V1 边界、历史数据迁移、兼容策略、安全验收和端到端验收用例，只包含本 change 所需内容。

## 16. Proposal 前必须定稿的参数

- 审核员是租户级角色还是知识库级授权；建议优先采用知识库级授权。
- 普通成员是否允许对其他人的 active 文档发起新版本提案；建议 V1 不允许，只能维护自己创建的文档。
- 文档审批通过后由系统自动发布，还是需要管理员确认发布；建议 V1 审批通过即发布，减少中间状态。
- 多知识库性能基准是否支持“候选默认值 5”；proposal 应以召回质量、P95 延迟、上下文长度和并发压测结果确定默认值、请求大小限制与租户级硬上限。
- 宿主主题能同步到什么程度；建议 V1 只支持位置、主色、标题、图标和浅色/深色模式。
- 历史知识库和历史文档缺失创建者时如何归属；建议回填租户管理员并记录迁移审计。
- 公共知识库未来采用显式跨租户授权还是复制到目标租户；建议 V1 保持租户内语义，不新增隐式跨租户公开。

这些参数不改变总体架构，但必须在对应 proposal 中形成明确决策、规格和验收场景后才能进入实现。
