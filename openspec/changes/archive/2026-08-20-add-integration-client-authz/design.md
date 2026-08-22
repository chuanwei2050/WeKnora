## Context

WeKnora 已有独立登录、租户 API Key 和 BidReview 专用 SSO，但缺少面向多个外部项目的通用客户端身份、用户映射和知识库级授权范围。一套部署需要同时服务多个项目；不同项目既可能使用不同租户，也可能在同一租户中共享知识库，因此租户隔离与项目授权不能依赖浏览器传参。

## Goals / Non-Goals

**Goals:**

- 为交互式嵌入和服务间 API 提供分离且统一的认证协议。
- 将 integration client 固定绑定到租户、identity provider、角色映射、权限上限、scopes、知识库 allowlist 和浏览器来源。
- 支持票据防重放、短期会话、CSRF、密钥轮换、撤销、审计和限流。
- 让每个项目的外部用户稳定映射且互不串号，并让嵌入会话安全复用现有知识管理 API。
- 保持现有独立登录和内部 API 兼容。

**Non-Goals:**

- V1 不提供隐式跨租户知识库访问。
- 不让外部项目直接持有租户 API Key，也不在浏览器中保存 client secret 或 service token。
- 不替换 WeKnora 现有用户、租户和管理员权限模型。
- 不创建第二套用户仓储、租户解析、角色枚举或知识库授权算法。

## Decisions

### 统一主体建立在现有认证授权链上

Integration 层只新增 client 约束、外部身份映射和 Cookie/CSRF 凭证解析，解析后复用现有 `User`、`Tenant`、JWT/OIDC、角色和知识库访问控制。BidReview SSO 保留现有入口并改为统一主体的兼容适配器；其浏览器 `localStorage` Bearer 行为不扩散到新模式。这样所有业务处理器只消费同一种主体，避免两套权限结果随时间分叉。

### Integration client 服务端绑定租户

每个外部项目创建独立 integration client，服务端保存 `client_id`、secret 哈希、租户、`identity_provider_id`、显式绑定的租户管理员、外部角色映射、最高可授予角色、scopes、知识库 allowlist、允许浏览器来源、状态和有效期。请求中的租户标识只用于外部身份映射，不能覆盖 client 的所属租户。相比接受 `X-Tenant-ID`，该方案消除了客户端横向切换租户的能力。允许映射管理员角色的 client 必须绑定一个处于同一租户且有效的现有租户管理员。

### 交互用户使用一次性 bootstrap ticket

宿主后端验证自身登录态后，以 client 凭证提交外部身份 ID 和已配置的外部角色，申请 60 秒有效的一次性随机 ticket。WeKnora 使用服务端角色映射计算用户角色，取映射结果、client 最高角色、scopes 与知识库 allowlist 的交集，绝不接受 `is_admin` 或任意内部权限声明。ticket 同时绑定预期浏览器 origin，该 origin 必须属于 client 白名单。宿主页面等待 iframe `ready`，再用精确 `targetOrigin` 的 `postMessage` 传递 ticket；iframe 通过浏览器侧 `/knowledge/api/integration/v1/auth/exchange` 兑换会话。服务端只保存 ticket 哈希和唯一 `jti`，并原子标记消费状态。

不采用 URL token，因为查询参数容易进入浏览历史、代理日志和 Referer；不直接把 service token 交给浏览器，因为无法可靠保护 client 权限。

认证端点不得在 URL、日志、审计详情或错误正文回显 secret、ticket、service token、Cookie 或 CSRF 原值；审计只保存不可逆指纹和必要标识。exchange 成功后立即清除 iframe 内存中的 ticket，失败也不得持久化 ticket。

### 嵌入会话使用限定路径 Cookie 与 CSRF

会话 Cookie 设置 `HttpOnly`、`Secure`、`SameSite=Lax`，同域代理下 `Path=/knowledge/`。统一认证中间件必须在 `/api/v1/*`、`/files` 和 `/api/integration/v1/*` 上识别该 Cookie，并构造与独立模式一致的用户和租户上下文，再叠加 client scopes 与知识库 allowlist。嵌入前端使用 Cookie 和 CSRF token，不读取或写入 `weknora_token`、refresh token、租户选择状态，也不发送 `X-Tenant-ID`。会话采用 15 分钟滚动有效期和最长 8 小时上限；修改请求同时校验独立 CSRF token。续期重新校验 client、租户、用户状态和授权，宿主退出或 client 撤销时可使会话失效。

### 服务间调用使用短期 service token

宿主后端通过 TLS 使用 `client_id + secret` 获取 audience 为 `weknora-integration` 的短期 token。token 只携带服务端计算的租户与 scopes，不接受调用方自行扩大声明。secret 支持两个有效槽位以完成无停机轮换，数据库只保存哈希。

### 外部身份、角色与有效资源范围

外部用户唯一键为 `(client_id, external_tenant_id, external_user_id)`。普通用户首次访问时在 client 绑定租户下自动创建 `member`，并将映射保存在集成身份表。映射为管理员的外部身份只允许绑定到 client 显式配置的现有租户管理员，不自动创建管理员，也不选择“租户下第一个管理员”。已存在映射不得静默改绑到其他内部用户。最终角色由服务端角色映射和 client 最高可授予角色共同限制；最终知识库范围为请求选择、client allowlist、当前用户权限和租户/显式共享关系的交集。请求包含任何越权资源时整次拒绝并记录原因，不静默返回部分结果。

bootstrap 接受外部用户启用状态，省略时按启用处理。宿主传入停用状态时，服务端将该项目身份标记为停用、撤销该 client 与内部用户组合下的浏览器会话，并拒绝签发 ticket；不得因此停用显式绑定的共享租户管理员或影响同一内部用户的其他 client。

### 浏览器来源和 CORS 精确校验

bootstrap 申请的预期 origin 必须在 client 允许来源中，exchange 请求的 `Origin` 必须与 ticket 绑定值一致。浏览器认证端点只对精确匹配来源返回凭证 CORS 响应，并设置 `Vary: Origin`；任何带凭证响应不得使用 `Access-Control-Allow-Origin: *`。同域反向代理仍是 V1 默认路径，但来源校验不能依赖代理或浏览器单方面保证。

现有 standalone Bearer/API Key 路由继续按原行为工作；凭证 CORS 的精确来源策略只应用于 Integration 浏览器认证端点，避免把全局 `AllowOrigins: ["*"]` 直接改成破坏兼容的全站策略。

所有 Integration 响应设置 `Cache-Control: no-store`；嵌入入口设置不泄漏路径信息的 `Referrer-Policy`，并通过 CSP `frame-ancestors` 只允许部署配置声明的宿主来源。同域默认仍为 `'self'`。

## Risks / Trade-offs

- [同域 iframe 可访问宿主同源数据] → 不把 iframe 当作安全边界；缩小 Cookie Path、执行后端授权并保持消息负载最小化。
- [一次性票据存储需要原子消费] → 使用支持 compare-and-set 或唯一状态更新的共享存储，并对 `jti` 建唯一约束。
- [短会话增加刷新请求] → 使用滚动续期；刷新时只执行必要状态和授权检查。
- [同租户多个项目可能产生身份或授权混淆] → 外部身份唯一键包含 client，每个项目使用独立 allowlist，审计记录同时包含 client、租户和用户。
- [错误角色映射造成权限提升] → 映射配置仅由 WeKnora 管理员维护，并以 client 最高角色、scopes 和资源 allowlist 多重封顶。
- [现有 Bearer 认证与嵌入 Cookie 产生主体差异] → 两种凭证统一进入同一主体构造和后端资源授权链，只在凭证解析与 CSRF 要求上分流。

## Migration Plan

1. 增加 integration client、identity provider、角色映射、外部身份映射、ticket、会话和审计所需数据结构，不启用任何外部 client。
2. 上线认证端点、精确 CORS 与支持 Cookie/Bearer/API Key 的统一认证中间件，通过功能开关限制在测试租户。
3. 为首个宿主创建最小 scopes 和 allowlist 的 client，验证票据、刷新、撤销和密钥轮换。
4. 逐项目启用；现有独立登录、租户 API Key 和 `/api/v1/*` 保持不变。
5. 回滚时禁用 integration client 与新路由；新增表保留以便审计，不影响独立模式。

## Open Questions

无阻塞性架构问题。ticket 与嵌入会话采用数据库还是现有共享缓存，由实现阶段根据部署拓扑选择，但必须满足原子消费、跨实例一致性和到期清理要求；身份源、角色映射和 Origin 规则已在本设计中固定。
