## Context

宿主项目需要自行增加左侧菜单和 `/knowledge` 页面，同时在内容区显示 WeKnora 的知识管理能力。WeKnora 当前是完整 SPA，适合通过同域反向代理与 iframe 保持独立发布；直接微前端化会引入宿主技术栈、路由、样式和依赖耦合。本变更依赖通用认证、Integration API 与文档投稿审批能力已经稳定。

## Goals / Non-Goals

**Goals:**

- 提供 `standalone` 与 `embedded-page` 两种可共存的知识管理页面模式。
- 固定宿主路由、同域代理、前端 base path 和后端路径映射。
- 提供最小、可校验的父子页面通信和登录引导。
- 保证未启用集成功能时独立部署无回归。

**Non-Goals:**

- V1 不引入 Module Federation、qiankun、single-spa 或共享前端依赖。
- WeKnora 不负责修改宿主左侧菜单；只定义宿主入口契约。
- iframe 不作为租户、用户或资源的安全隔离边界。

## Decisions

### 宿主拥有 `/knowledge` 外壳

宿主项目自行增加菜单并路由到 `/knowledge`，保留宿主侧边栏和顶部导航；内容区加载 `/knowledge/embed/platform/knowledge-bases?mode=embedded`。WeKnora embedded 模式隐藏自身应用外壳，保留知识库列表、详情、贡献、审批和配置内容。

### 同域代理使用三个固定前缀

- `/knowledge/embed/*` 映射 WeKnora 前端。
- `/knowledge/api/*` 映射后端 `/api/*`，因此浏览器 `/knowledge/api/integration/v1/*` 到达后端 `/api/integration/v1/*`。
- `/knowledge/files?file_path=...` 精确映射后端 `/files?file_path=...` 并保留 query string。V1 不使用通配式文件路径表达当前文件服务。

SSE 路由关闭代理缓冲并配置长读取超时；需要 WebSocket 时显式转发升级头。前端分别配置页面、API 和文件 base path，禁止通过字符串猜测路径。

### 嵌入认证通过父页面传递一次性票据

iframe 加载后发送 `ready`，父页面使用精确 `targetOrigin` 发送 bootstrap ticket。iframe 经代理调用 exchange 端点建立限定 `/knowledge/` 的会话。该 Cookie 会话由统一认证中间件用于现有 `/api/v1/*`、`/files` 和 Integration API，确保知识库管理页无需改写为 Integration CRUD API。embedded-page 的请求层使用 Cookie 和 CSRF token，不读取或刷新 `localStorage` Bearer token，不发送 `X-Tenant-ID`。登录失效时 iframe 通知父页面 `unauthorized`，不跳转独立登录页。

### 页面通信协议保持最小

宿主到 WeKnora 仅包含 `auth-ready`、`set-theme`、`set-locale`、`open-knowledge-base`；WeKnora 到宿主仅包含 `ready`、`unauthorized`、`route-change`、`document-published`。双方校验 origin、消息类型和负载 schema，不使用 `targetOrigin="*"`。

### standalone 保持默认

未显式传入嵌入模式时继续呈现完整侧边栏、独立登录、租户切换和管理能力。嵌入模式只改变外壳、认证入口和通信，不分叉知识库、治理、RAG 或聊天业务逻辑。

## Risks / Trade-offs

- [同源父子页面不存在安全隔离] → 后端完整鉴权；CSP `frame-ancestors 'self'` 只限制谁能嵌入，不声明数据隔离。
- [base path 配置不一致导致资源或 API 404] → 启动时校验三个路径，提供固定反向代理验收用例。
- [iframe 高度和内部路由体验割裂] → 宿主固定内容区尺寸，并通过 `route-change` 同步必要状态，不共享完整路由控制权。
- [嵌入改造破坏独立模式] → 复用同一业务组件，并将 standalone 非回归列为发布门禁。

## Migration Plan

0. 确认三个前置 change 已完成实施及验收；任何依赖未完成时停止本 change，不以任务勾选代替依赖验收。
1. 增加页面、API 和精确文件端点 base path 配置以及 `embedded-page` 模式，不改变默认值。
2. 实现应用外壳切换和最小通信协议，在本地同域代理下验证。
3. 接入 bootstrap ticket，并覆盖登录、退出、过期和撤销场景。
4. 对首个宿主开放 `/knowledge/*` 代理，完成独立与嵌入双模式验收。
5. 回滚时移除宿主菜单与代理规则；WeKnora standalone 继续运行。

## Open Questions

无阻塞性问题。现有管理 API 统一采用嵌入 Cookie 会话的方案已经确定，不新增完整 Integration CRUD API。跨域 iframe 与更深主题融合不在 V1；若未来开放跨域嵌入，需要单独评审第三方 Cookie、CSP 来源和宿主信任边界。
