## Why

其他项目需要在保留自身菜单和页面外壳的前提下使用 WeKnora 知识管理能力，同时 WeKnora 必须继续独立运行。需要提供明确的嵌入运行模式、同域代理路径和宿主通信契约，而不是复制前端或引入高耦合微前端运行时。

## What Changes

- 新增 `embedded-page` 运行模式，在宿主 `/knowledge` 内容区通过同域 iframe 显示知识库管理页面。
- 定义 `/knowledge/embed/*`、`/knowledge/api/*` 和精确文件端点 `/knowledge/files?file_path=...` 的反向代理与后端路径映射。
- 为页面、API 和文件增加可配置 base path，并保持 `standalone` 为默认模式。
- 定义最小化 `postMessage` 协议、精确 origin 校验、登录失效通知、主题、语言和路由同步。
- 让 embedded-page 使用 HttpOnly 嵌入会话访问现有知识管理 `/api/v1/*` 与 `/files`，不在 iframe 中保存 Bearer token、refresh token 或租户切换状态。
- 明确同域 iframe 只提供样式和运行时隔离，认证与资源授权仍由后端执行。
- 复用现有 `/knowledge` BidReview 嵌入识别、菜单裁剪、可配置 Vite base 和 Nginx SSE 代理作为兼容基础，抽取通用 embedded runtime，不复制知识管理页面。

## Capabilities

### New Capabilities

- `embedded-knowledge-management`: 定义宿主菜单入口、同域 iframe 页面、代理路径、运行模式、宿主通信和独立模式兼容要求。

### Modified Capabilities

无。

## Impact

- 影响前端路由、应用外壳、运行时配置、认证入口、Nginx 示例和宿主集成说明。
- **实施门禁**：只有 `add-integration-client-authz`、`add-integration-knowledge-api` 已完成实施并通过各自验收，且已归档的 `2026-08-19-add-knowledge-contribution-review` 仍通过回归后，才能开始本 change；仅有 planning artifacts complete 不满足门禁。
- 不引入微前端运行时，不替换现有 `/platform/*`、`/api/v1/*`、`/files/*` 或独立登录流程。
