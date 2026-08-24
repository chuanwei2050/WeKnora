## Context

Integration 外部身份解析在 `resolveExternalUser` 中对「已有映射用户」要求 `user.EffectiveRole() == mappedRole`。运维将外挂自动创建的 `member` 提升为 `tenant_admin` 后，宿主仍传普通角色会导致 bootstrap `403`，前端表现为外挂知识库 Integration 凭证或权限无效。独立登录路径直接读取账号角色，不受该校验影响，因此出现「独立可用、外挂不可用」的分裂。

本变更只调整已有映射用户的角色一致性规则，不改 client 数据模型、bootstrap 契约或独立认证链。

## Goals / Non-Goals

**Goals:**

- 宿主与知识库账号初始均为普通角色时，仅将 WeKnora 账号改为 `tenant_admin`，独立知识库与外挂知识库均获得完整租户管理员能力（`CanManageTenant` 等），仍受 client scopes 与知识库范围限制；**不依赖** client `max_role`。
- 宿主映射为 `tenant_admin` 的路径继续要求显式绑定 `administrator_user_id`，禁止宿主自行提权或自动创建管理员；宿主映射上限仍受 `max_role` 约束。
- 账号降回 `member` 后，两条路径同步失去管理员能力。

**Non-Goals:**

- 不改为「多 `administrator_user_id`」模型（本需求用 WeKnora 侧直接提权账号即可支持多名管理员）。
- 不引入细粒度「仅建库」权限；本次目标是完整 `tenant_admin`。
- 不修改宿主侧角色体系或强制宿主传管理员角色。
- 不改变会话 Cookie、CSRF、ticket 生命周期。

## Decisions

### 以 WeKnora 持久化角色为外挂提权来源

对已有映射用户：

1. 计算 `mappedRole = mappedRole(client, externalRoles)`。
2. 若 `mappedRole == tenant_admin`：仍要求 `identity.UserID == client.AdministratorUserID`，且账号必须是有效租户管理员。
3. 若 `mappedRole == member`（或未映射到管理员）：
   - 校验租户一致且账号启用；
   - **不再**要求 `EffectiveRole == mappedRole`；
   - 若账号 `EffectiveRole == tenant_admin`，会话按租户管理员主体建立，**不**再受 client `max_role` 拦截（`max_role` 只约束宿主映射可授予的上限）；
   - 若账号仍是 `member`，按普通成员建立。

产品选择：运维在用户管理里把 member 账号改成 tenant_admin 后，独立与外挂必须立即按真实账号角色生效，不能要求再去改 client `max_role`。

### 新用户路径不变

首次访问仍按映射创建 `member`，或将宿主管理员映射绑定到已配置的 `AdministratorUserID`。WeKnora 侧提权发生在账号已存在之后，由用户管理 API/UI 完成。

### 会话建立后复用现有授权

Exchange / Authenticate 已加载完整 `User`；`CanCreateKnowledgeBase` 等基于 `CanManageTenant()`。Integration 中间件继续叠加 `IntegrationKnowledgeBaseScope` 与 scopes，与现有「绑定管理员进外挂」行为一致。

## Risks / Trade-offs

- [多名外挂用户被提权为完整租户管理员] → 接受为产品意图；提权仍需已有租户/平台管理员操作，并保留 client scopes 与 KB 范围封顶。
- [client `max_role=member` 仍允许 WeKnora 侧提权进外挂] → 接受为产品意图；宿主仍无法通过映射自行提权（`mappedRole` 受 `max_role` 限制），提权入口仅限 WeKnora 用户管理。
- [旧会话仍是提权前主体] → 外挂页将 session/csrf 写入 `sessionStorage`；刷新页面时先 restore 再 refresh，响应带上最新 `user.role` 写入 authStore，无需点宿主「重新连接」。会话失效时再回退等待宿主 ticket。

## Migration Plan

1. 发布后端变更与单测；无需数据迁移。
2. 已误提权导致外挂失败的账号在发布后可直接「重新连接」恢复。
3. 回滚：恢复旧 `EffectiveRole == mappedRole` 校验即可；无 schema 回滚。

## Open Questions

无。产品已确认：目标为完整租户管理员能力（非仅建库）。
