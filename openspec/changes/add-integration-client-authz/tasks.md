## 1. 数据模型与管理边界

- [x] 1.1 在现有 `User`、`Tenant` 和角色模型上增加 integration client、双 secret 哈希、租户绑定、identity provider、角色映射、最高角色、scopes、知识库 allowlist、浏览器来源、状态和有效期数据结构及迁移，不建立平行租户或角色体系
- [x] 1.2 复用现有用户仓储增加基于 identity provider 的外部身份映射，并增加绑定 origin 的 bootstrap ticket、嵌入会话和认证审计数据结构及到期清理
- [x] 1.3 实现 integration client、identity provider 和外部角色映射的创建、查询、轮换、禁用、撤销及管理员权限校验

## 2. 服务认证

- [x] 2.1 实现 `/api/integration/v1/auth/token` 的 client 凭证校验和短期 service token 签发
- [x] 2.2 校验 service token 的 audience、有效期、client 状态、租户和 scopes
- [x] 2.3 覆盖错误 secret、过期 client、错误 audience、双密钥轮换和撤销测试

## 3. 交互用户认证

- [x] 3.1 实现 `/api/integration/v1/auth/bootstrap` 的外部身份校验、服务端角色映射、权限封顶和预期 origin 绑定，并生成 60 秒有效、只存哈希的一次性 ticket
- [x] 3.2 实现 `/api/integration/v1/auth/exchange` 的 origin 校验、原子消费、共享身份解析和嵌入会话创建
- [x] 3.3 扩展现有 JWT/API Key/OIDC 认证中间件以识别限定 `/knowledge/` 的 HttpOnly/Secure/SameSite Cookie，为 `/api/v1/*`、`/files` 和 Integration API 构造同一主体并实现 CSRF 校验
- [x] 3.4 实现 refresh、logout、15 分钟滚动有效期、8 小时绝对上限和主体状态重校验
- [x] 3.5 让嵌入前端使用 Cookie/CSRF 模式，禁止读写 Bearer/refresh token 和发送 `X-Tenant-ID`
- [ ] 3.6 覆盖 ticket 首次兑换、错误 origin、重放、过期、并发消费、角色提升、会话续期、CSRF 缺失和退出测试

## 4. 授权、审计与发布门禁

- [x] 4.1 建立包含 client、租户、外部用户、scopes 和知识库范围的统一集成请求主体
- [x] 4.2 实现请求范围与 client allowlist、用户权限及租户边界求交，并对混入越权 ID 的请求整体返回 `403`
- [x] 4.3 为 token、bootstrap、exchange 和 refresh 分别增加限流和脱敏审计
- [x] 4.4 将凭证 CORS 改为 client 来源精确匹配并设置 `Vary: Origin`，拒绝通配来源与凭证组合
- [ ] 4.5 执行共享身份源、项目本地身份源、跨租户、跨 client、错误 Origin、撤销传播和权限收回安全测试
- [x] 4.6 验证未启用 integration client 时独立登录、租户 API Key 和 `/api/v1/*` 行为无回归
- [x] 4.7 将现有 BidReview SSO 接入统一主体兼容适配层，并验证旧入口不扩散 `localStorage` Bearer 方案到新嵌入模式
- [x] 4.8 对 Integration 凭证响应增加 `no-store`、安全 Referrer/CSP 策略和日志脱敏测试，确保 secret、ticket、token、Cookie 与 CSRF 原值不落日志
