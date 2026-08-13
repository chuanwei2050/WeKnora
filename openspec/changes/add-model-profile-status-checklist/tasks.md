## 1. 后端状态 API

- [x] 1.1 实现 profile/env 解析：空白默认、非法回退、air_gapped 仅 `true` 为真、`${VAR}` 展开、`__FILL_`/残留 `${`→`missing_env`；锁定响应字段且无 API Key
- [x] 1.2 实现对照：`Model.Name` + 类型优先级；同优先级按 `CreatedAt`/`ID` 稳定排序；embedding 维度 `mismatch`；actions 区分 `intent=add|edit`；输出 `summary`
- [x] 1.3 `SystemHandler` 注入 `ModelService`；先 `TenantIDFromContext` 再 `ListModels`；注册路由并更新 container DI
- [x] 1.4 单测：空白/非法 profile、air_gapped 默认、`${VAR}`、`__FILL_`、missing_registration、KnowledgeQA 兜底、专用类型优先、稳定排序、VLLM 匹配 vlm、embedding edit action、无密钥

## 2. 前端可见性与引导

- [x] 2.1 `getModelProfileStatus` 与类型（含 `summary`、`intent`）
- [x] 2.2 `ModelSettings.vue` 状态条/清单 + “不自动切流量 / 名称需与登记名一致”文案
- [x] 2.3 `intent=add` 打开添加对话框；`intent=edit` 打开已有模型编辑；`missing_env` 不跳转
- [x] 2.4 `zh-CN` / `en-US` 的 `modelProfile.*` 文案

## 3. 文档与验收

- [x] 3.1 更新 `.env.example`：`MODEL_PROFILE` 语义 + `*_NAME` 与 UI/`served_model_name` 对齐
- [x] 3.2 更新 `docs/airgap-operations.md`：检查清单入口 + 命名对齐（相对 `model-scripts`）
- [x] 3.3 手动验收：online 摘要合理；仅改 `MODEL_PROFILE=offline` 清单变化且聊天仍用原绑定模型
