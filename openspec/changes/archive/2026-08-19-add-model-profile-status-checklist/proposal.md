## Why

线上/离线模型已在 `.env` 用 `MODEL_PROFILE` 与 `ONLINE_*` / `OFFLINE_*` 分套保存，但 profile 不驱动运行时选模，设置页也看不到“当前意图 profile、是否严格离线、哪些角色尚未登记”。运维容易误以为改 env 即切换流量。需要在不自动改库的前提下补齐可见性与切换检查清单。

## What Changes

- 新增只读 API：按 `MODEL_PROFILE` 对照 env 期望角色与当前认证租户已登记模型，返回 profile、`AIR_GAPPED_MODE`、角色缺口、`summary` 与建议动作（不含 API Key）。
- 在设置 → 模型设置页增加状态条与可展开检查清单；`missing_registration` 跳转**添加**对话框，`mismatch`（且已有匹配 ID）跳转**编辑**对话框；`missing_env` 只展示、不跳转添加。
- 澄清 `.env.example` 与 `docs/airgap-operations.md`：`MODEL_PROFILE` 非自动流量开关；`OFFLINE_*_NAME` 必须与 UI 登记的 `Model.Name`（通常为推理服务 `served_model_name`）一致。
- **不**根据 profile 自动创建/覆盖模型绑定，**不**自动重绑知识库/Agent/验证配置，**不**改写 `model-scripts` 部署逻辑。

## Capabilities

### New Capabilities

- `model-profile-status`: 模型 profile 状态可见性与半自动切换检查清单（只读对照 + UI 引导，不自动改绑）。

### Modified Capabilities

- （无）

## Impact

- 后端：`SystemHandler` 注入 `ModelService`；`GET /api/v1/system/model-profile-status`；env `${VAR}` 展开；按 `types.Model.Name` 匹配；container DI。
- 前端：`api/system`、`ModelSettings.vue`、i18n；按 action 打开添加或编辑 `ModelEditorDialog`。
- 文档：`.env.example`、`docs/airgap-operations.md`（含与 `model-scripts` 命名对齐说明）。
- 运行时问答/检索与模型绑定语义不变。
