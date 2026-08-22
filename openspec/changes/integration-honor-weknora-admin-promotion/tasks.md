## 1. 解析逻辑

- [x] 1.1 调整 `resolveExternalUser`：已有映射且宿主映射为 `member` 时，允许账号已是 `tenant_admin` 通过校验，移除严格的 `EffectiveRole == mappedRole` 相等要求
- [x] 1.2 保留宿主映射为 `tenant_admin` 时必须命中 `administrator_user_id` 的约束
- [x] 1.3 WeKnora 侧提权不因 client `max_role=member` 被拒绝（`max_role` 只约束宿主映射）

## 2. 测试

- [x] 2.1 增加单测：宿主 `member` + 账号已提升 `tenant_admin` → 解析成功
- [x] 2.2 增加单测：提升后再降回 `member` → 仍可解析且角色为成员
- [x] 2.3 增加单测：账号 `tenant_admin` 且 `max_role=member` → 仍解析成功
- [x] 2.4 增加/确认单测：宿主映射 `tenant_admin` 但非绑定管理员 → 拒绝；命中绑定管理员 → 成功
- [x] 2.5 运行 `internal/integration` 相关测试并确认通过

## 3. 收尾

- [x] 3.1 确认 Authenticate/Refresh 读取最新 User 角色，提权后无需额外改会话模型
- [x] 3.2 （可选）在租户用户管理或外挂报错提示中补充「提权后需重新连接外挂」说明——仅当现有文案会误导时再改
- [x] 3.3 外挂刷新页可恢复会话：`sessionStorage` 持久化凭证；refresh 返回最新 user；启动时优先 resume
- [x] 3.4 补充 `tenant-user-administration` delta（角色互改 / 最后管理员降级）并与主规格对齐
- [x] 3.5 降级为成员时强制显式知识库范围；自编辑同步 `authStore.role`
