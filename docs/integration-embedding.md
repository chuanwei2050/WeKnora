# WeKnora 集成接入

## 悬浮聊天 Widget

生产镜像会将 `frontend/dist-widget` 发布到同域 `/widget/`；宿主可加载 `/widget/weknora-widget.js`（ESM）或 `/widget/weknora-widget.iife.js`。浏览器配置只包含公开 iframe 地址和选库模式；`client_secret` 与 service token 必须留在宿主后端。

```js
const widget = WeKnoraWidget.initWidget({
  version: 1,
  instanceId: 'help-chat',
  iframeUrl: `${location.origin}/knowledge/embed/embed/widget?mode=embedded-widget`,
  selection: { mode: 'fixed', knowledgeBaseIds: ['kb-id'] },
  theme: { title: '知识库助手', primaryColor: '#0052d9' },
})

widget.open()
widget.moveTo({ x: 24, y: 80 })
widget.resizeTo({ width: 520, height: 720 })
widget.maximize()
widget.restore()
widget.resetLayout()
```

iframe 发出 `ready` 后，宿主后端使用 service token 调用 `/api/integration/v1/auth/bootstrap`，再由宿主页以精确 `targetOrigin` 发送 `{ version: 1, type: 'auth-ready', ticket }`。ticket 不得放入 URL、日志或持久化存储。

client 由平台管理员在 `/api/v1/admin/integration-clients` 创建，secret 只在创建或轮换响应中返回一次。轮换窗口结束后调用 `POST /api/v1/admin/integration-clients/:client_id/revoke-previous-secret` 清除旧 secret 并撤销该 client 的现有会话，再由宿主重新获取短期 token。按最小权限配置以下 scopes：

- `kb:list`：列出授权知识库
- `rag:search`：调用 `POST /api/integration/v1/rag/search`
- `chat:read` / `chat:write`：读取与创建 Integration 聊天
- `knowledge:read` / `knowledge:write`：嵌入知识管理页访问既有知识接口
- `file:read`：读取 allowlist 内知识对应的文件

宿主后端以 `client_id` 和 `client_secret` 调用 `/api/integration/v1/auth/token`，不得把 secret 或 service token 下发到浏览器。搜索默认值与硬上限可通过 `INTEGRATION_DEFAULT_TOP_K`、`INTEGRATION_MAX_TOP_K`、`INTEGRATION_MAX_KNOWLEDGE_BASES`、`INTEGRATION_MAX_QUERY_BYTES`、`INTEGRATION_MAX_REQUEST_BYTES` 调整；SSE 保留期使用 `INTEGRATION_EVENT_RETENTION`（Go duration，例如 `1h`）。

## 嵌入知识管理页

宿主 `/knowledge` 页面保留自己的导航，在内容区加载：

```html
<iframe
  src="/knowledge/embed/platform/knowledge-bases?mode=embedded-page"
  title="知识库管理"
  referrerpolicy="strict-origin"
  sandbox="allow-scripts allow-same-origin allow-forms allow-downloads">
</iframe>
```

生产构建设置：

```bash
VITE_APP_BASE_PATH=/knowledge/embed/ VITE_API_BASE_PATH=/knowledge npm run build
```

参考代理配置位于 `frontend/nginx.conf`。它将 `/knowledge/api/*` 映射到 `/api/*`，将精确 `/knowledge/files?file_path=...` 映射到 `/files`，并对 SSE 关闭缓冲、缓存且使用长超时。

## 生命周期和错误处理

SDK 支持 `ready`、`open`、`close`、`layout-changed`、`unauthorized`、`answer-completed`、`error`。`destroy()` 会移除 DOM 和监听器；同一 `instanceId` 重复初始化返回现有实例。收到 `unauthorized` 时，宿主应重新申请一次性 ticket；不得把 client secret 发送到浏览器。
