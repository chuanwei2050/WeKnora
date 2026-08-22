## Why

WeKnora 目前只有租户级 API Key 和业务专用 SSO，无法安全地让多个外部项目共享一套部署并保持租户、用户和知识库权限隔离。需要先建立通用的接入客户端、身份交换和资源授权边界，为页面嵌入及开放 API 提供统一安全基础。

## What Changes

- 新增与租户服务端绑定的 integration client，支持 identity provider、外部角色映射、最高可授予角色、scopes、知识库 allowlist、浏览器来源白名单、状态、过期、撤销和双密钥轮换。
- 将交互式用户认证统一为绑定预期 origin 的一次性 bootstrap ticket，兑换为限定 `/knowledge/` 路径的短期 HttpOnly 会话，并对修改请求执行 CSRF 校验。
- 为服务间调用提供限定 audience 和 scopes 的短期 service token；客户端 Secret 只允许宿主后端持有。
- 使用 `(client_id, external_tenant_id, external_user_id)` 映射外部用户，确保不同项目即使复用身份源和用户编号也不会串号；普通用户首次访问时自动创建为租户成员。
- 允许 integration client 显式绑定一个现有租户管理员；外部管理员身份只能解析到该账号，不允许按租户查询结果或外部角色隐式创建管理员。
- 支持宿主同步外部用户启用状态；停用项目身份后立即拒绝新 ticket 并撤销该 client 下该用户的现有会话。
- 让嵌入会话通过统一认证中间件访问授权范围内的 `/api/v1/*`、`/files` 和 `/api/integration/v1/*`，嵌入前端不保存 Bearer token，也不发送 `X-Tenant-ID`。
- 为页面、API 和后续 Widget 提供统一请求主体、服务端角色映射、资源范围求交、精确 Origin/CORS 校验、拒绝审计、限流及会话撤销能力。
- 复用现有 `User`、`Tenant`、JWT/OIDC、租户 API Key、角色和知识库访问控制；BidReview SSO 作为兼容适配器迁移到统一主体，不新增平行身份、租户或角色体系。

## Capabilities

### New Capabilities

- `integration-client-authz`: 定义外部项目客户端、交互用户身份交换、服务认证、租户绑定、知识库授权范围和审计要求。

### Modified Capabilities

无。

## Impact

- 影响后端认证中间件、租户与用户映射、管理员绑定、外部身份状态、数据模型、管理接口、会话存储、CORS、审计和限流。
- 新增面向第三方项目的外挂知识库对接文档，覆盖账号映射、知识库菜单、悬浮聊天、服务端认证、接口与停用同步。
- 现有 BidReview `localStorage` Bearer 流程仅作为兼容路径保留；新嵌入模式不得复制该凭证存储方式。
- 新增 `/api/integration/v1/auth/*` 契约，但不替换现有独立模式登录、租户 API Key 或 `/api/v1/*`。
- 后续 `add-integration-knowledge-api`、`add-embedded-knowledge-management` 和 `add-floating-chat-widget` 依赖本变更。
