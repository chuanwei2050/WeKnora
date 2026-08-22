## Why

外挂知识库（Integration）当前要求「宿主映射角色」与 WeKnora 账号 `EffectiveRole` 严格一致。运维把对应知识库账号从 `member` 提升为 `tenant_admin` 后，宿主仍传普通角色时 bootstrap 会失败，页面报 Integration 凭证或权限无效。独立登录已能按账号角色生效，外挂路径却不能，导致「只改知识库账号角色」无法同时覆盖独立与外挂场景。

## What Changes

- 调整 Integration 外部用户解析：允许 WeKnora 侧已提升的 `tenant_admin` 在宿主仍映射为 `member` 时正常签发会话，并以账号真实角色行使完整租户管理员能力（不受 client `max_role` 拦截；仍受 scopes 与知识库范围封顶）。`max_role` 仅继续约束宿主角色映射上限。
- 保持宿主驱动提权受限：宿主映射结果为 `tenant_admin` 时，仍只能落到 client 显式绑定的管理员账号，不得因宿主传参自动创建或猜测管理员。
- 降级可逆：账号改回 `member` 后，独立与外挂路径均恢复普通成员能力。
- **非 BREAKING**：不改变独立登录、client 配置字段或宿主 bootstrap 请求契约；仅放宽「已有映射用户」的角色一致性校验。

## Capabilities

### New Capabilities

- （无）

### Modified Capabilities

- `integration-client-authz`: 放宽已有外部身份在 WeKnora 侧被提升为 `tenant_admin` 后的解析与会话规则；明确「宿主映射不可擅自提权，WeKnora 侧提权对外挂生效」。
- `tenant-user-administration`: 允许在保留至少一名启用租户管理员的前提下，于 `tenant_admin` 与 `member` 之间互改，并要求降级时配置知识库范围。

## Impact

- 后端：`internal/integration/service.go` 的 `resolveExternalUser` 及对应单测；`internal/application/service/user.go` 角色互改与最后管理员守卫。
- 规格：`integration-client-authz` 与 `tenant-user-administration` 增量需求。
- 前端：用户管理可改租户管理员角色；外挂页将会话凭证存入 `sessionStorage`，刷新页面时优先用已有会话 refresh 并拉取最新用户角色；无需再点宿主「重新连接」。宿主仍可通过 `auth-ready` 重新发 ticket。
- 安全：提权入口仍仅限 WeKnora 用户管理（已有租户/平台管理员权限）；宿主无法仅凭外部角色声明获得管理员能力。
