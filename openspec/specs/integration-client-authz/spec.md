# integration-client-authz Specification

## Purpose
TBD - created by archiving change add-integration-client-authz. Update Purpose after archive.
## Requirements
### Requirement: Integration client 必须绑定租户和授权范围
系统 MUST 为每个 integration client 在服务端绑定唯一租户、`identity_provider_id`、角色映射、最高可授予角色、scopes、知识库访问模式、允许浏览器来源、状态和有效期，并且 MUST NOT 使用客户端传入的租户标识覆盖该绑定。知识库访问模式 MUST 为 `selected` 或 `all`；`selected` 使用固定 allowlist，`all` 在认证边界实时解析 Client 绑定租户当前的全部知识库。

#### Scenario: 客户端访问绑定租户内的允许知识库
- **WHEN** 有效 client 请求其 scopes 和 allowlist 内的知识库资源
- **THEN** 系统使用服务端绑定的租户构造请求主体并继续执行用户和资源授权

#### Scenario: 客户端尝试切换租户
- **WHEN** client 在 header、query 或 body 中提交与服务端绑定不一致的租户标识
- **THEN** 系统拒绝请求并记录 client、绑定租户、请求租户和拒绝原因

#### Scenario: 租户级 Client 新建知识库
- **WHEN** `all` 模式 Client 的租户管理员在有效嵌入会话中创建知识库
- **THEN** 系统在后续认证边界将该知识库加入当前有效范围，并继续限制为 Client 绑定租户

### Requirement: 外部角色必须由服务端映射并受 client 上限约束
系统 MUST 只接受 client 配置中声明的外部角色值，MUST 使用 WeKnora 服务端维护的映射生成内部角色，并 MUST 将结果限制在 client 最高可授予角色、scopes、知识库 allowlist 和当前用户权限范围内；系统 MUST NOT 接受宿主直接提交内部角色、`is_admin` 或权限列表。

#### Scenario: 合法外部角色映射
- **WHEN** 宿主为用户提交 client 配置允许的外部角色
- **THEN** 系统使用服务端映射和 client 权限上限生成用户角色与资源范围

#### Scenario: 宿主尝试声明管理员权限
- **WHEN** bootstrap 请求直接提交内部管理员角色、`is_admin=true` 或超出映射配置的权限
- **THEN** 系统拒绝请求并记录权限提升尝试，不创建 ticket

### Requirement: 服务凭证必须仅供宿主后端使用
系统 MUST 允许宿主后端通过 TLS 使用 `client_id` 和 secret 获取短期 service token；token 的 audience MUST 为 `weknora-integration`，且浏览器 MUST NOT 获得 client secret 或 service token。

#### Scenario: 有效服务凭证换取 token
- **WHEN** 启用状态且未过期的 client 提交有效 secret
- **THEN** 系统签发包含服务端计算租户和 scopes 的短期 service token

#### Scenario: 错误 audience 调用集成 API
- **WHEN** token 的 audience 不是 `weknora-integration`
- **THEN** 系统拒绝该 token，且不执行目标业务操作

### Requirement: Client secret 必须可安全轮换和撤销
系统 MUST 只保存 client secret 的不可逆哈希，MUST 支持最多两个并行有效 secret 完成无停机轮换，并 MUST 在 client 或 secret 被撤销后拒绝新的 token 和 ticket 请求。

#### Scenario: 双密钥轮换
- **WHEN** 管理员创建新 secret 且旧 secret 仍处于轮换窗口
- **THEN** 两个 secret 均可完成认证，直至管理员撤销旧 secret

#### Scenario: 撤销客户端
- **WHEN** 管理员禁用 integration client
- **THEN** 系统拒绝其新认证请求，并使该 client 的现有嵌入会话和可撤销 token 失效

### Requirement: 交互用户必须通过一次性票据建立嵌入会话
系统 MUST 由宿主后端为已认证用户申请最长 60 秒有效的一次性 bootstrap ticket，ticket MUST 绑定 client、租户、外部用户、服务端映射角色、知识库范围和预期浏览器 origin，并 MUST 通过原子操作确保每个 ticket 只能成功兑换一次。

#### Scenario: 首次兑换有效 ticket
- **WHEN** iframe 在有效期内向 exchange 端点提交未使用 ticket
- **THEN** 系统原子标记 ticket 已使用并建立绑定 client、租户和外部用户的嵌入会话

#### Scenario: 重放或过期 ticket
- **WHEN** iframe 提交已使用、已过期或已撤销的 ticket
- **THEN** 系统拒绝兑换，不创建会话，并记录对应 `jti` 的失败原因

#### Scenario: 从错误 origin 兑换 ticket
- **WHEN** exchange 请求的 `Origin` 与 ticket 绑定 origin 不一致或不在 client 白名单
- **THEN** 系统拒绝兑换且 ticket 不产生有效会话

### Requirement: 嵌入会话必须限制浏览器凭证范围
系统 MUST 使用设置了 `HttpOnly`、`Secure`、`SameSite=Lax` 且 Path 默认为 `/knowledge/` 的 Cookie 保存嵌入会话；统一认证中间件 MUST 在 `/api/v1/*`、`/files` 和 `/api/integration/v1/*` 上识别该会话并构造用户、租户、client、scopes 和知识库范围；所有修改类请求 MUST 同时验证 CSRF token。

#### Scenario: 合法修改请求
- **WHEN** 请求同时携带有效嵌入会话 Cookie 和匹配的 CSRF token
- **THEN** 系统在重新执行资源授权后允许该修改操作

#### Scenario: 缺失 CSRF token
- **WHEN** 修改类请求只有有效 Cookie 但没有有效 CSRF token
- **THEN** 系统拒绝请求且不修改资源

#### Scenario: 嵌入会话调用现有管理 API
- **WHEN** iframe 使用有效嵌入会话调用授权范围内的 `/api/v1/knowledge-bases/*` 或 `/files`
- **THEN** 统一认证中间件建立与独立模式一致的用户和租户上下文，并叠加 client scopes 与知识库 allowlist 后继续处理

#### Scenario: 嵌入请求尝试切换租户
- **WHEN** 使用嵌入会话的请求携带 `X-Tenant-ID` 或其他租户覆盖值
- **THEN** 系统拒绝租户覆盖并始终使用 client 服务端绑定租户

### Requirement: 嵌入会话必须短期有效并支持撤销
系统 MUST 采用 15 分钟滚动有效期和最长 8 小时绝对有效期，并 MUST 在刷新时重新校验 client、租户、用户状态、scopes 和知识库授权。

#### Scenario: 在允许窗口内续期
- **WHEN** 活跃会话尚未超过绝对有效期且所有主体和授权仍有效
- **THEN** 系统延长滚动有效期但不超过 8 小时上限

#### Scenario: 授权已被收回
- **WHEN** 会话刷新时发现 client、用户或知识库授权已失效
- **THEN** 系统拒绝续期并终止该会话

### Requirement: 外部用户身份必须按项目稳定解析
系统 MUST 使用 `(client_id, external_tenant_id, external_user_id)` 作为外部身份唯一键；普通外部用户首次访问时 MUST 在 client 绑定租户下自动创建 `member` 并持久化映射，不同 client MUST NOT 因复用 identity provider 或相同外部用户编号而自动共享内部账号。

#### Scenario: 普通用户首次访问
- **WHEN** client 提交尚无映射的有效普通外部用户
- **THEN** 系统在 client 绑定租户中创建 `member`，保存项目级身份映射并签发 ticket

#### Scenario: 两个项目使用相同用户 ID
- **WHEN** 两个 client 提交相同外部租户和用户 ID
- **THEN** 系统解析为两个隔离用户，不发生错误合并

### Requirement: 外部管理员必须显式绑定现有租户管理员
系统 MUST 要求可授予 `tenant_admin` 的 integration client 显式绑定一个同租户、有效的现有租户管理员；外部管理员身份 MUST 解析到该账号，MUST NOT 自动创建管理员或按查询顺序猜测管理员。

#### Scenario: 显式绑定管理员进入
- **WHEN** 外部角色映射结果为 `tenant_admin` 且 client 已绑定有效租户管理员
- **THEN** 系统将该项目外部身份绑定到指定管理员并建立受 client 范围限制的会话

#### Scenario: 缺少或非法管理员绑定
- **WHEN** client 允许管理员映射但未绑定管理员，或绑定账号不属于该租户、已停用或不是租户管理员
- **THEN** 系统拒绝 client 配置或 bootstrap，不创建管理员账号或 ticket

### Requirement: 外部用户停用必须按项目传播
系统 MUST 接受宿主后端同步外部用户启用状态；停用时 MUST 标记对应项目身份不可用、撤销该 client 与用户组合下的现有会话并拒绝新 ticket，且 MUST NOT 影响其他 client 或停用共享的内部管理员账号。

#### Scenario: 停用普通外部用户
- **WHEN** 宿主为已有映射提交停用状态
- **THEN** 系统停用该项目身份、撤销对应会话并拒绝 bootstrap

#### Scenario: 重新启用外部用户
- **WHEN** 宿主再次提交启用状态且内部用户、client 和授权仍有效
- **THEN** 系统恢复该项目身份并允许建立新会话

### Requirement: 浏览器来源和凭证 CORS 必须精确限制
系统 MUST 对 bootstrap、exchange、refresh 和 logout 等浏览器认证流程校验 client 允许来源；带凭证的 CORS 响应 MUST 返回精确匹配的 origin 和 `Vary: Origin`，MUST NOT 返回 `Access-Control-Allow-Origin: *`。

#### Scenario: 允许来源调用浏览器认证端点
- **WHEN** 请求 origin 精确匹配 client 白名单且其他认证条件有效
- **THEN** 系统返回对应 origin 的凭证 CORS 响应并继续认证流程

#### Scenario: 未允许来源调用浏览器认证端点
- **WHEN** 请求 origin 缺失、格式非法或不在 client 白名单，且该端点要求浏览器来源
- **THEN** 系统拒绝请求，不签发或刷新会话，也不返回通配凭证 CORS 响应

### Requirement: 有效知识库范围必须在服务端求交
系统 MUST 将请求选择、client 知识库访问模式解析结果、当前用户权限和租户或显式共享关系求交；请求包含任一越权知识库时 MUST 整次返回 `403`，不得静默忽略或返回部分结果。

#### Scenario: 请求全部位于有效范围
- **WHEN** 请求中的所有知识库均处于最终有效范围
- **THEN** 系统将该范围附加到请求主体并允许后续处理

#### Scenario: 请求混入未授权知识库
- **WHEN** 请求同时包含授权和未授权知识库
- **THEN** 系统返回 `403`，不处理任何知识库，并审计全部被拒绝 ID

### Requirement: 集成认证和授权必须可审计与限流
系统 MUST 记录 client、租户、用户、scope、资源、操作、结果和拒绝原因，并 MUST 分别限制 token、ticket、exchange 和 refresh 请求速率。

#### Scenario: 认证失败被审计
- **WHEN** client secret、ticket、CSRF 或资源授权校验失败
- **THEN** 系统写入不包含明文 secret、ticket 或 token 的安全审计记录

#### Scenario: 超过认证限流
- **WHEN** 主体超过对应认证端点的速率限制
- **THEN** 系统拒绝请求并返回稳定的限流错误，不执行凭证签发或兑换
