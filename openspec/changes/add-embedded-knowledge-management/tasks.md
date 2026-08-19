## 1. 前置条件与路径配置

- [ ] 1.1 执行实施门禁：核验认证授权和 Integration API 已完成代码实施及验收，并核验已归档 `2026-08-19-add-knowledge-contribution-review` 回归通过；任一未满足时停止本 change
- [x] 1.2 在现有 Vite base 配置上增加独立的页面、API 和文件 base path 配置及启动时校验
- [x] 1.3 扩展现有 Nginx 配置并验证 `/knowledge/embed/*`、`/knowledge/api/*` 以及 `/knowledge/files?file_path=...` 到 `/files?file_path=...` 的同域代理映射和 query 保留
- [x] 1.4 复用现有 SSE 禁止缓冲和长超时配置并补齐嵌入路径，按需转发 WebSocket 升级头

## 2. Embedded page 模式

- [x] 2.1 增加 `standalone` 和 `embedded-page` 运行模式解析，保持 standalone 为默认
- [x] 2.2 将现有 BidReview `/knowledge` 菜单裁剪抽取为通用 embedded runtime，在 embedded-page 中隐藏应用外壳并复用现有知识库管理、贡献、审批和配置页面
- [x] 2.3 为 embedded-page 请求层启用 Cookie/CSRF，禁止使用本地 Bearer/refresh token 和 `X-Tenant-ID`，并验证现有 `/api/v1/*` 与 `/files` 可用
- [x] 2.4 限制嵌入模式的登录失效行为，改为通知宿主而非跳转独立登录页
- [x] 2.5 支持语言、主题和初始知识库 ID 的受控初始化

## 3. 宿主通信与认证引导

- [x] 3.1 实现最小化、版本化的父子页面消息类型和负载校验
- [x] 3.2 实现 `ready` 后通过精确 `targetOrigin` 接收 ticket 并调用代理 exchange 路径
- [x] 3.3 实现 `unauthorized`、`route-change` 和 `document-published` 宿主通知
- [ ] 3.4 覆盖错误 origin、伪造消息、ticket 过期和会话撤销的浏览器安全测试

## 4. 双模式验收与文档

- [x] 4.1 提供宿主 `/knowledge` 页面、iframe 和 Nginx 反向代理参考配置
- [ ] 4.2 在真实浏览器验证嵌入登录、Cookie 访问现有管理 API、投稿审批、搜索、精确文件代理、query 保留和长连接
- [x] 4.3 验证同域 iframe 不绕过任何后端权限，并检查 CSP `frame-ancestors`、`Referrer-Policy` 和认证响应 `Cache-Control: no-store`
- [ ] 4.4 执行 standalone 登录、租户切换、管理、上传、搜索、聊天和文件访问非回归测试
- [x] 4.5 验证现有 BidReview 嵌入入口继续可用，且其旧 Bearer 兼容路径不被新 embedded-page 使用
