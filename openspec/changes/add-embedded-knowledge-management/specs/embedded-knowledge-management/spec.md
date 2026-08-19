## ADDED Requirements

### Requirement: 宿主必须拥有知识库菜单和页面外壳
宿主项目 MUST 自行增加“知识库”菜单并路由到宿主 `/knowledge` 页面；WeKnora MUST 提供可在该页面内容区加载的嵌入知识管理入口。

#### Scenario: 用户从宿主菜单进入知识库
- **WHEN** 用户点击宿主左侧“知识库”菜单
- **THEN** 宿主保留自身侧边栏和顶部导航，并在内容区加载 WeKnora 嵌入页面

### Requirement: 嵌入页面必须使用固定同域代理路径
系统 MUST 使用 `/knowledge/embed/*` 访问 WeKnora 前端、`/knowledge/api/*` 访问后端 API，并使用 `/knowledge/files?file_path=...` 访问当前文件服务；系统 MUST 将浏览器 `/knowledge/api/integration/v1/*` 映射到后端 `/api/integration/v1/*`，将 `/knowledge/api/v1/*` 映射到后端 `/api/v1/*`，并将 `/knowledge/files` 精确映射到后端 `/files` 且保留 query string。

#### Scenario: 嵌入页面调用 Integration API
- **WHEN** iframe 请求 `/knowledge/api/integration/v1/knowledge-bases`
- **THEN** 网关把请求转发至后端 `/api/integration/v1/knowledge-bases`，且不丢失认证 Cookie 和必要代理头

#### Scenario: 嵌入页面预览文件
- **WHEN** 用户在 iframe 中打开授权文档的文件预览
- **THEN** 请求通过 `/knowledge/files?file_path=...` 到达后端 `/files?file_path=...`，完整保留 query string 并复用文档权限校验

### Requirement: 页面、API 和文件 base path 必须可独立配置
WeKnora 前端 MUST 分别支持页面路由、API 和文件 base path 配置，并 MUST 在生成路由和资源 URL 时使用对应配置。

#### Scenario: 部署在 knowledge 前缀下
- **WHEN** 页面 base path 为 `/knowledge/embed/`、API base 为 `/knowledge`、文件端点为 `/knowledge/files`
- **THEN** 页面资源、API、SSE 和文件 URL 均使用正确前缀且不存在重复或缺失 `/api`

### Requirement: Embedded page 必须隐藏自身应用外壳
当运行模式为 `embedded-page` 时，WeKnora MUST 隐藏自身侧边栏、Logo、租户切换和用户菜单，同时 MUST 保留知识库列表、详情、文档贡献、审批和配置内容。

#### Scenario: 管理员打开嵌入知识库页面
- **WHEN** 已授权管理员加载 `mode=embedded-page` 的知识库页面
- **THEN** 页面只显示知识管理内容，不显示 WeKnora 顶层应用外壳

### Requirement: 嵌入认证必须使用一次性票据
iframe MUST 在发送 `ready` 后接收父页面通过精确 `targetOrigin` 传递的 bootstrap ticket，并 MUST 经 `/knowledge/api/integration/v1/auth/exchange` 兑换嵌入会话；ticket MUST NOT 出现在 URL 中。

#### Scenario: iframe 完成登录引导
- **WHEN** 父页面收到合法 iframe 的 `ready` 消息
- **THEN** 父页面向匹配 origin 发送 ticket，iframe 兑换后加载授权内容

#### Scenario: 嵌入会话失效
- **WHEN** iframe 收到会话未授权响应且刷新失败
- **THEN** iframe 向宿主发送 `unauthorized`，不跳转 WeKnora 独立登录页

### Requirement: Embedded page 必须以 Cookie 会话访问现有管理 API
embedded-page MUST 使用统一认证中间件识别的嵌入 Cookie 会话访问 `/knowledge/api/v1/*`、`/knowledge/files` 和 `/knowledge/api/integration/v1/*`；前端 MUST NOT 在嵌入模式读写 Bearer token、refresh token 或租户选择状态，MUST NOT 发送 `X-Tenant-ID`，修改请求 MUST 携带 CSRF token。

#### Scenario: 嵌入页面加载知识库详情
- **WHEN** 已认证 iframe 请求 `/knowledge/api/v1/knowledge-bases/{id}` 且资源位于用户和 client 有效范围
- **THEN** 后端从嵌入 Cookie 构造用户、租户和 client 主体并返回详情，无需浏览器 Bearer token

#### Scenario: 嵌入页面修改知识库内容
- **WHEN** 已认证 iframe 调用现有修改接口并携带有效 CSRF token
- **THEN** 后端同时校验用户权限、client scopes、知识库 allowlist 和 CSRF 后执行操作

#### Scenario: 嵌入页面残留租户切换头
- **WHEN** embedded-page 请求携带 `X-Tenant-ID`
- **THEN** 后端拒绝租户覆盖，前端清理该状态并继续使用 client 服务端绑定租户

### Requirement: 父子页面通信必须受约束
父页面和 iframe MUST 校验消息 origin、类型和负载结构，MUST 使用明确 `targetOrigin`，并 MUST NOT 使用 `*` 发送认证或业务消息。

#### Scenario: 收到未知来源消息
- **WHEN** 父页面或 iframe 收到来源不在配置允许范围的消息
- **THEN** 接收方忽略消息且不改变认证、路由、主题或业务状态

### Requirement: 嵌入通信协议必须保持最小化
宿主到 WeKnora MUST 仅承诺 `auth-ready`、`set-theme`、`set-locale`、`open-knowledge-base`，WeKnora 到宿主 MUST 仅承诺 `ready`、`unauthorized`、`route-change`、`document-published`，新增类型必须版本化。

#### Scenario: 文档成功发布
- **WHEN** 用户在嵌入页面发布文档
- **THEN** iframe 向宿主发送结构化 `document-published` 事件，且不暴露内部模型或敏感配置

### Requirement: 同域 iframe 不得被视为安全边界
系统 MUST 对 iframe 发起的每个 API 和文件请求执行完整后端认证与资源授权；CSP `frame-ancestors` 在同域模式 MUST 默认为 `'self'`。

#### Scenario: iframe 直接请求未授权知识库
- **WHEN** 用户通过开发者工具绕过 UI 请求未授权知识库
- **THEN** 后端拒绝请求，不依赖 iframe、隐藏按钮或父页面状态进行保护

### Requirement: Standalone 必须保持默认和兼容
未显式启用嵌入模式时，系统 MUST 使用 `standalone`，并 MUST 保持原有 `/platform/*`、独立登录、租户切换、`/api/v1/*`、`/files/*` 和完整应用外壳可用。

#### Scenario: 未配置任何集成参数启动
- **WHEN** WeKnora 按现有独立部署配置启动
- **THEN** 用户可继续登录、管理知识库、上传、搜索、聊天和访问文件，且不需要 `/knowledge/*` 前缀

### Requirement: 流式代理必须支持长连接
同域代理 MUST 为 SSE 关闭响应缓冲和缓存并配置足够的读写超时；启用 WebSocket 功能时 MUST 转发升级头。

#### Scenario: 嵌入页面接收长时间回答
- **WHEN** 聊天 SSE 持续时间超过普通 HTTP 请求时长
- **THEN** 代理持续转发事件而不缓冲整段响应或提前关闭连接
