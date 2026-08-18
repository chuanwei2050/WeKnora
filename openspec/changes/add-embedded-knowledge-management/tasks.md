## 1. 前置条件与路径配置

- [ ] 1.1 执行实施门禁：核验认证授权、Integration API 和文档投稿审批三个依赖 change 已完成代码实施及验收；任一未满足时停止本 change
- [ ] 1.2 为前端增加独立的页面、API 和文件 base path 配置及启动时校验
- [ ] 1.3 配置并验证 `/knowledge/embed/*`、`/knowledge/api/*` 以及 `/knowledge/files?file_path=...` 到 `/files?file_path=...` 的同域代理映射和 query 保留
- [ ] 1.4 为 SSE 关闭代理缓冲和缓存并配置长连接超时，按需转发 WebSocket 升级头

## 2. Embedded page 模式

- [ ] 2.1 增加 `standalone` 和 `embedded-page` 运行模式解析，保持 standalone 为默认
- [ ] 2.2 在 embedded-page 中隐藏应用外壳并保留知识库管理、贡献、审批和配置页面
- [ ] 2.3 为 embedded-page 请求层启用 Cookie/CSRF，禁止使用本地 Bearer/refresh token 和 `X-Tenant-ID`，并验证现有 `/api/v1/*` 与 `/files` 可用
- [ ] 2.4 限制嵌入模式的登录失效行为，改为通知宿主而非跳转独立登录页
- [ ] 2.5 支持语言、主题和初始知识库 ID 的受控初始化

## 3. 宿主通信与认证引导

- [ ] 3.1 实现最小化、版本化的父子页面消息类型和负载校验
- [ ] 3.2 实现 `ready` 后通过精确 `targetOrigin` 接收 ticket 并调用代理 exchange 路径
- [ ] 3.3 实现 `unauthorized`、`route-change` 和 `document-published` 宿主通知
- [ ] 3.4 覆盖错误 origin、伪造消息、ticket 过期和会话撤销的浏览器安全测试

## 4. 双模式验收与文档

- [ ] 4.1 提供宿主 `/knowledge` 页面、iframe 和 Nginx 反向代理参考配置
- [ ] 4.2 在真实浏览器验证嵌入登录、Cookie 访问现有管理 API、投稿审批、搜索、精确文件代理、query 保留和长连接
- [ ] 4.3 验证同域 iframe 不绕过任何后端权限并检查 CSP `frame-ancestors 'self'`
- [ ] 4.4 执行 standalone 登录、租户切换、管理、上传、搜索、聊天和文件访问非回归测试
