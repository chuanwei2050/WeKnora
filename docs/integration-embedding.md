# 外挂知识库对接指南

本文面向需要把 WeKnora 知识库菜单、知识管理页面、RAG 接口和悬浮聊天框接入现有业务系统的开发者。

## 1. 总体架构

```text
业务系统后端                         用户浏览器                         WeKnora
client_id + client_secret ──换 token──────────────────────────────────►│
识别当前业务用户 ───────────申请 ticket────────────────────────────────►│
                                      │◄──一次性 ticket─────────────────┘
                                      ├── iframe / Widget 兑换 Cookie
                                      └── 使用 Cookie + CSRF 访问知识库
```

必须遵守以下边界：

- 一个业务项目对应一个 integration client，client 在服务端固定绑定一个 WeKnora 租户。
- `client_secret` 和 service token 只能保存在业务系统后端，不能进入浏览器、URL、日志或前端配置。
- 租户 API Key 只可用于管理员侧确认或初始化租户，不作为外挂页面的运行期凭证。
- 浏览器只接收最长 60 秒、仅能使用一次且绑定 Origin 的 bootstrap ticket。
- 页面和 API 权限最终取 client allowlist、用户权限和租户边界的交集。

## 2. 项目和账号映射

### 2.1 创建身份源

```http
POST /api/v1/admin/integration-identity-providers
Authorization: Bearer <平台管理员令牌>
Content-Type: application/json

{"id":"erp","name":"ERP 用户中心"}
```

### 2.2 创建项目 client

如果项目允许外部管理员进入，必须在 `administrator_user_id` 显式指定一个现有、有效、同租户的 `tenant_admin`。系统不会自动选择或创建管理员。

```http
POST /api/v1/admin/integration-clients
Authorization: Bearer <平台管理员令牌>
Content-Type: application/json

{
  "id": "erp-project-a",
  "name": "ERP 项目 A",
  "tenant_id": 10000,
  "identity_provider_id": "erp",
  "administrator_user_id": "<已有租户管理员用户ID>",
  "scopes": [
    "kb:list", "rag:search", "chat:read", "chat:write",
    "knowledge:read", "knowledge:write", "file:read"
  ],
  "knowledge_base_ids": ["<知识库ID-1>", "<知识库ID-2>"],
  "allowed_origins": ["https://project-a.example.com"],
  "role_mappings": {
    "project_admin": "tenant_admin",
    "project_user": "member"
  },
  "max_role": "tenant_admin"
}
```

响应中的 `client_secret` 只返回一次。普通项目应省略 `administrator_user_id`，只配置 `member` 映射并把 `max_role` 设置为 `member`。

已有 client 升级后可由平台管理员补绑或更换管理员：

```http
PUT /api/v1/admin/integration-clients/<client_id>/administrator
Authorization: Bearer <平台管理员令牌>
Content-Type: application/json

{"user_id":"<已有租户管理员用户ID>"}
```

### 2.3 用户映射规则

外部身份唯一键为：

```text
client_id + external_tenant_id + external_user_id
```

- 普通用户首次访问时，在 client 绑定租户下自动创建 `member`，映射保存到 `integration_external_identities`。
- 同一外部用户编号从两个 client 进入时产生两份隔离映射，不会串项目。
- 外部管理员只映射到 client 显式绑定的现有租户管理员。
- 已建立映射不会因为角色参数变化而静默改绑到另一个内部账号。

## 3. 宿主后端认证

### 3.1 获取 service token

```http
POST /api/integration/v1/auth/token
Content-Type: application/json

{
  "client_id": "erp-project-a",
  "client_secret": "<仅后端保存>"
}
```

### 3.2 为当前用户申请 ticket

业务系统后端必须从自己的登录态读取用户身份，禁止直接信任浏览器传来的用户 ID 或角色。

```http
POST /api/integration/v1/auth/bootstrap
Authorization: Bearer <service_token>
Content-Type: application/json

{
  "external_tenant_id": "project-a",
  "external_user_id": "user-9527",
  "external_roles": ["project_user"],
  "active": true,
  "origin": "https://project-a.example.com"
}
```

Node.js 宿主后端示例：

```js
app.post('/host/weknora-ticket', requireLogin, async (req, res) => {
  const serviceToken = await getCachedWeKnoraServiceToken()
  const response = await fetch(`${WEKNORA_URL}/api/integration/v1/auth/bootstrap`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${serviceToken}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      external_tenant_id: req.user.projectId,
      external_user_id: req.user.id,
      external_roles: req.user.isProjectAdmin ? ['project_admin'] : ['project_user'],
      active: req.user.active,
      origin: new URL(APP_PUBLIC_URL).origin,
    }),
  })
  const result = await response.json()
  res.status(response.status).json(result)
})
```

## 4. 左侧菜单嵌入知识库

宿主菜单点击“知识库”后，在内容区加载同域代理地址：

```html
<iframe
  id="knowledge-frame"
  src="/knowledge/embed/platform/knowledge-bases?mode=embedded-page"
  title="知识库管理"
  referrerpolicy="strict-origin"
  sandbox="allow-scripts allow-same-origin allow-forms allow-downloads">
</iframe>
```

握手流程：

1. iframe 发出 `{version: 1, type: "ready"}`。
2. 宿主浏览器请求自己的 `/host/weknora-ticket`。
3. 宿主以精确 `targetOrigin` 发送 `{version: 1, type: "auth-ready", ticket}`。
4. iframe 兑换 HttpOnly Cookie，并显示知识库列表。

打开指定知识库或文档：

```text
/knowledge/embed/platform/knowledge-bases/<knowledge_base_id>
  ?mode=embedded-page
  &knowledge_id=<knowledge_id>
```

生产构建建议：

```bash
VITE_APP_BASE_PATH=/knowledge/embed/ VITE_API_BASE_PATH=/knowledge npm run build
```

同域反向代理至少需要转发 `/knowledge/embed/*`、`/knowledge/api/*`、`/knowledge/files`、`/assets/*` 和 `/widget/*`。SSE 路由必须关闭代理缓冲和缓存。

## 5. 悬浮聊天框

加载 `/widget/weknora-widget.iife.js` 后初始化：

```js
const widget = WeKnoraWidget.initWidget({
  version: 1,
  instanceId: 'project-a-knowledge-chat',
  iframeUrl: `${location.origin}/knowledge/embed/embed/widget?mode=embedded-widget`,
  targetOrigin: location.origin,
  selection: {
    mode: 'fixed',
    knowledgeBaseIds: ['<知识库ID-1>', '<知识库ID-2>'],
  },
  theme: { title: '项目 A · 知识助手', primaryColor: '#0052d9' },
  preserveSession: true,
})

widget.on('ready', async () => {
  const response = await fetch('/host/weknora-ticket', { method: 'POST' })
  const result = await response.json()
  widget.authenticate(result.data.ticket)
})

widget.on('unauthorized', () => {
  // 重新申请 ticket；不要复用旧 ticket。
})

widget.on('open-document', ({ knowledgeBaseId, knowledgeId }) => {
  openKnowledgeMenu({ knowledgeBaseId, knowledgeId })
  widget.close()
})
```

选库模式：

| 模式 | 用途 |
| --- | --- |
| `fixed` | 固定使用指定知识库，至少传一个 ID |
| `selectable` | 用户可在 client allowlist 内选择知识库 |
| `all-allowed` | 使用该用户最终获准的全部知识库 |

SDK 方法包括 `open()`、`close()`、`destroy()`、`moveTo()`、`resizeTo()`、`maximize()`、`restore()` 和 `resetLayout()`；事件包括 `ready`、`unauthorized`、`answer-completed`、`open-document` 和 `error`。

## 6. Integration API

以下路径统一以前缀 `/api/integration/v1` 开始：

| 方法 | 路径 | Scope | 说明 |
| --- | --- | --- | --- |
| POST | `/auth/token` | client 凭证 | 获取 service token |
| POST | `/auth/bootstrap` | service token | 创建 ticket、同步用户状态 |
| POST | `/auth/exchange` | ticket | 兑换 Cookie 与 CSRF token |
| POST | `/auth/refresh` | Cookie + CSRF | 刷新浏览器会话 |
| POST | `/auth/logout` | Cookie + CSRF | 退出并撤销会话 |
| GET | `/knowledge-bases` | `kb:list` | 列出最终授权知识库 |
| POST | `/rag/search` | `rag:search` | 多知识库检索 |
| GET/POST | `/chat/sessions` | `chat:read/write` | 列出或创建对话 |
| GET/PATCH/DELETE | `/chat/sessions/:id` | `chat:read/write` | 对话详情、改名、删除 |
| GET/POST | `/chat/sessions/:id/messages` | `chat:read/write` | 消息列表、流式问答 |
| GET | `/chat/sessions/:id/messages/:message_id` | `chat:read` | 获取回答快照和引用 |
| GET | `/chat/sessions/:id/messages/:message_id/events` | `chat:read` | SSE 断线续传 |
| POST | `/chat/sessions/:id/messages/:message_id/cancel` | `chat:write` | 取消生成 |
| POST | `/chat/sessions/:id/voice/tts` | `chat:read` | 回答语音合成 |

修改类浏览器请求必须携带 exchange 返回的 `X-CSRF-Token`；创建消息还必须提供唯一的 `Idempotency-Key`。

## 7. 用户停用和重新启用

用户被移出业务项目或离职时，宿主后端调用 bootstrap 并传 `active: false`。接口返回拒绝是预期结果，同时服务端会：

1. 将该 client 下的外部身份标记为停用；
2. 撤销该 client 与该用户组合下的现有浏览器会话；
3. 拒绝后续 ticket。

重新加入项目时传 `active: true`。系统只恢复项目身份；如果内部用户、client、租户或知识库权限已停用，仍会拒绝访问。停用一个 client 的身份不会影响其他项目，也不会停用显式绑定的租户管理员账号本身。

## 8. 权限与安全检查清单

- 每个项目使用独立 client，不共享 `client_secret`。
- `allowed_origins` 使用精确 Origin，不使用 `*`。
- 普通项目设置 `max_role: member`。
- 管理员项目显式填写 `administrator_user_id`。
- `knowledge_base_ids` 只列项目需要的知识库。
- 后端从自身会话读取外部用户和角色，不接受浏览器冒充。
- ticket 不放 URL、不缓存、不重放。
- 收到 `unauthorized` 后重新申请 ticket。
- 用户退出或停用时同步状态并销毁 Widget。
- 定期轮换 secret；结束后调用 `/api/v1/admin/integration-clients/:client_id/revoke-previous-secret`。

常见状态码：`400` 参数非法，`401` 凭证或会话失效，`403` Origin、角色、租户或知识库越权，`409` ticket 重放或幂等冲突，`410` SSE 游标过期，`429` 超过限流。
