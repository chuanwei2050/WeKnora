## ADDED Requirements

### Requirement: WeKnora 侧提升的租户管理员必须对外挂会话生效
系统 MUST 允许已存在的 Integration 外部身份所映射的内部用户，在宿主角色映射结果仍为 `member`（或未映射为管理员）时，若该用户在 WeKnora 中已被提升为有效的 `tenant_admin`，则成功完成 bootstrap 并建立以外挂约束为边界的租户管理员会话，且 MUST NOT 因 client `max_role` 为 `member` 而拒绝。该会话 MUST 使该用户在独立知识库与外挂知识库路径上均具备完整租户管理员能力（包括创建与管理知识库等 `CanManageTenant` 能力），并 MUST 继续受 client scopes 与知识库范围封顶。系统 MUST NOT 因「宿主映射角色与账号 EffectiveRole 不一致」而拒绝此类已提升用户。`max_role` MUST 仅约束宿主角色映射可授予的上限，MUST NOT 阻止 WeKnora 用户管理中已完成的账号提权在外挂路径生效。

#### Scenario: 宿主仍为普通角色但账号已提升
- **WHEN** 已有项目级外部身份映射的用户在 WeKnora 中角色为 `tenant_admin`，且宿主 bootstrap 提交的外部角色映射结果为 `member`
- **THEN** 系统签发 ticket，会话主体为该租户管理员，外挂内可行使完整租户管理员能力（仍受 scopes 与知识库范围限制）

#### Scenario: 账号降回普通成员
- **WHEN** 同一外部身份对应的内部用户被改回 `member` 后再次 bootstrap，宿主映射仍为 `member`
- **THEN** 系统按普通成员建立会话，不再授予租户管理员能力

#### Scenario: client max_role 为 member 时 WeKnora 侧提权仍生效
- **WHEN** 内部用户已是 `tenant_admin`，client `max_role` 为 `member`，宿主映射仍为 `member`
- **THEN** 系统签发 ticket，会话主体为该租户管理员

#### Scenario: 刷新外挂页恢复会话并拿到最新角色
- **WHEN** 浏览器仍持有有效外挂会话凭证（含刷新后从 sessionStorage 恢复），用户在 WeKnora 中已被提升为 `tenant_admin`
- **THEN** 外挂页无需宿主再次点击「重新连接」，通过会话恢复/refresh 即可加载最新用户角色并具备租户管理员能力
### Requirement: 宿主映射提权仍必须绑定显式管理员
系统 MUST 继续要求：当宿主角色映射结果为 `tenant_admin` 时，外部身份只能解析到 client 显式绑定的有效租户管理员账号；MUST NOT 因宿主传参自动创建管理员，也 MUST NOT 将任意已提升的其他 `tenant_admin` 账号当作宿主映射管理员的落点（除非该账号正是绑定的 `administrator_user_id`）。

#### Scenario: 宿主映射管理员且命中绑定账号
- **WHEN** 外部角色映射结果为 `tenant_admin` 且映射用户等于 client 绑定的有效 `administrator_user_id`
- **THEN** 系统允许 bootstrap 并建立受 client 范围限制的管理员会话

#### Scenario: 宿主映射管理员但未命中绑定账号
- **WHEN** 外部角色映射结果为 `tenant_admin`，但外部身份对应的内部用户不是 client 绑定的 `administrator_user_id`
- **THEN** 系统拒绝 bootstrap，不签发 ticket
