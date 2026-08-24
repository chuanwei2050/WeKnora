## MODIFIED Requirements

### Requirement: 管理员必须能够管理租户内用户
系统 MUST 允许平台管理员管理任意租户用户，并 MUST 允许租户管理员列出、创建、统一编辑和启用或禁用本租户用户。统一编辑 MUST 支持修改用户名、可选重置密码、在 `tenant_admin` 与 `member` 之间调整角色及配置知识库可见范围。

#### Scenario: 管理员将租户管理员改回普通成员
- **WHEN** 管理员在编辑弹窗将非最后一个启用状态的 `tenant_admin` 改为 `member` 并配置知识库范围
- **THEN** 系统保存为普通成员、应用知识库范围并撤销其现有令牌

### Requirement: 用户管理必须强制租户隔离和角色上限
系统 MUST 拒绝租户管理员读取或修改其他租户用户，MUST 拒绝租户管理员创建或授予 `platform_admin`，MUST 拒绝修改现有 `platform_admin` 的角色，MUST 允许在保留至少一名启用租户管理员的前提下将 `tenant_admin` 与 `member` 互改，并 MUST 拒绝普通成员访问用户管理 API。

#### Scenario: 租户管理员降级为普通成员
- **WHEN** 管理员将非最后一个启用状态的 `tenant_admin` 改为 `member`
- **THEN** 系统保存为普通成员并撤销其现有令牌

#### Scenario: 修改平台管理员角色
- **WHEN** 管理员尝试修改现有平台管理员的角色
- **THEN** 系统拒绝请求且目标管理员角色保持不变

### Requirement: 租户必须保留至少一个可用租户管理员
系统 MUST 拒绝禁用或降级租户最后一个处于启用状态的租户管理员。

#### Scenario: 降级最后一个租户管理员
- **WHEN** 管理员尝试将租户内唯一启用的 `tenant_admin` 改为 `member`
- **THEN** 系统返回冲突响应且目标用户角色保持为 `tenant_admin`
