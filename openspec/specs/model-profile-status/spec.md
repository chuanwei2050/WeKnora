# model-profile-status Specification

## Purpose
TBD - created by archiving change add-model-profile-status-checklist. Update Purpose after archive.
## Requirements
### Requirement: 模型 profile 状态只读查询

系统必须（MUST）提供认证后的只读接口 `GET /api/v1/system/model-profile-status`，返回有效 `profile`、`profile_raw`、`profile_valid`、`air_gapped`、`summary`（含 `ok` / `missing_env` / `missing_registration` / `mismatch` 计数）、角色对照列表与 `actions`。`MODEL_PROFILE`：空白默认 `online` 且 `profile_valid=true`；仅接受 `online`/`offline`（大小写不敏感）；其他非空非法值回退有效 `online` 且 `profile_valid=false`。`AIR_GAPPED_MODE` 仅在值为 `true`（忽略大小写与首尾空白）时 `air_gapped=true`，否则为 false。读取期望 env 时必须（MUST）展开 `${VAR}`。响应不得（MUST NOT）包含 API Key 或等价密钥字段。Handler 必须（MUST）先用安全方式检测租户上下文；缺失时返回错误且不得（MUST NOT）调用会因缺租户而 panic 的列举逻辑，也不得返回伪造全量 `ok`。

#### Scenario: 查询 online profile 状态

- **WHEN** `MODEL_PROFILE=online` 且已认证租户请求状态
- **THEN** 响应 `profile=online`、`profile_valid=true`
- **AND** 角色期望取自经展开的 `ONLINE_*`
- **AND** 响应含 `summary` 计数
- **AND** 响应不含 API Key

#### Scenario: 空白 profile 使用默认

- **WHEN** `MODEL_PROFILE` 为空或仅空白
- **THEN** `profile=online` 且 `profile_valid=true`

#### Scenario: 非法 profile 回退

- **WHEN** `MODEL_PROFILE` 为无法识别的非空值
- **THEN** 有效 `profile=online` 且 `profile_valid=false`
- **AND** `profile_raw` 保留原始值

#### Scenario: air_gapped 默认 false

- **WHEN** `AIR_GAPPED_MODE` 未设置或非 `true`
- **THEN** 响应 `air_gapped=false`

#### Scenario: 严格离线标志可见

- **WHEN** `AIR_GAPPED_MODE=true`
- **THEN** 响应 `air_gapped=true`

#### Scenario: 无租户上下文

- **WHEN** 请求缺少可用租户上下文
- **THEN** 接口返回错误
- **AND** 不返回声称全部角色 `ok` 的成功体

### Requirement: 角色缺口检查清单

系统必须（MUST）按角色 `chat`、`verifier_1`、`verifier_2`、`evaluation_judge`、`embedding`、`rerank`、`vlm`、`asr`、`tts` 对照 env 与租户模型。匹配键必须（MUST）为 trim 后的 `types.Model.Name` 与期望名称全等，并按设计锁定的 `ModelType` 优先级选择；同优先级多条必须（MUST）按 `CreatedAt` 升序再 `ID` 升序取第一条。状态仅允许 `ok`、`missing_env`、`missing_registration`、`mismatch`。空名、含 `__FILL_`、或名称/base_url 残留 `${...}` 必须（MUST）为 `missing_env`。名称有效时 base_url 可空并进入登记对照。Embedding 仅在期望维度与匹配模型维度均存在（>0）且不等时为 `mismatch`；不得（MUST NOT）仅因 base_url 不同判 `mismatch`。`actions`：`missing_registration` 必须带 `intent=add` 与 `add_dialog_type`；`mismatch` 且存在匹配 ID 必须带 `intent=edit`、`add_dialog_type` 与 `matched_model_id`；`missing_env` 不得带添加/编辑 action。对照不得（MUST NOT）改写任何模型绑定。

#### Scenario: env 已填但未登记

- **WHEN** 期望名称可解析
- **AND** 无可用类型且 `Name` 相等的登记
- **THEN** 状态为 `missing_registration`
- **AND** 存在 `intent=add` 的 action

#### Scenario: 验证角色以对话模型登记仍匹配

- **WHEN** `verifier_1` 期望名可解析
- **AND** 仅有同名 `KnowledgeQA`
- **THEN** 状态为 `ok` 并返回该模型 `id`/`name`

#### Scenario: 专用类型优先于兜底类型

- **WHEN** 同名同时存在 `Verifier` 与 `KnowledgeQA`
- **AND** 查询 `verifier_1`
- **THEN** 匹配 `Verifier` 记录

#### Scenario: 同类型多条取更早创建者

- **WHEN** 同名同类型存在多条登记
- **THEN** 选择 `CreatedAt` 更早的一条；若相同则 `ID` 字典序更小的一条

#### Scenario: 离线占位未填写

- **WHEN** `MODEL_PROFILE=offline` 且名称或 base_url 含 `__FILL_`、残留 `${...}` 或名称为空
- **THEN** 状态为 `missing_env`
- **AND** 无 add/edit action

#### Scenario: 字面量未展开的 base_url 视为缺 env

- **WHEN** base_url 展开后仍含 `${`
- **THEN** 状态为 `missing_env`

#### Scenario: Embedding 维度不一致走编辑引导

- **WHEN** 已匹配 Embedding 但维度与期望均存在且不等
- **THEN** 状态为 `mismatch`
- **AND** action 为 `intent=edit` 且包含 `matched_model_id`
- **AND** `gap_reason` 提示可能需重建索引

#### Scenario: 只读对照不改写绑定

- **WHEN** 多次调用状态接口
- **THEN** 租户模型与会话/Agent 默认绑定不变

### Requirement: 设置页展示与半自动引导

前端必须（MUST）在设置 → 模型设置展示 profile、air-gapped、`summary` 缺口摘要与角色清单。`intent=add` 必须打开添加对话框；`intent=edit` 必须打开已有模型的编辑对话框（使用返回的 `matched_model_id`）。`modelType` 取自 `add_dialog_type`。允许预填期望名称/Base URL/维度提示；用户必须手动保存。`missing_env` 必须展示且不得提供添加主操作。文案必须说明改 `MODEL_PROFILE` 不自动切流量，且 env 期望名需与登记 `name`（如 `model-scripts` 的 `served_model_name`）一致。

#### Scenario: 打开模型设置看到状态

- **WHEN** 用户打开模型设置页
- **THEN** 显示 profile、air-gapped 与缺口摘要
- **AND** 文案说明 profile 非自动流量开关

#### Scenario: 从清单添加视觉模型

- **WHEN** 点击 `vlm` 的 `intent=add` 引导
- **THEN** 打开 `modelType=vllm` 的添加对话框且保存前不建记录

#### Scenario: 从清单编辑 Embedding 维度

- **WHEN** 点击 embedding 的 `intent=edit` 引导
- **THEN** 打开已有 embedding 模型的编辑对话框
- **AND** 保存前不新建另一条记录

#### Scenario: missing_env 不提供添加跳转

- **WHEN** 角色状态为 `missing_env`
- **THEN** 展示缺口且不提供添加主操作

### Requirement: 配置文档澄清 profile 语义

项目必须（MUST）在 `.env.example` 说明：`MODEL_PROFILE` 仅用于状态/清单；切流量需在模型管理重绑；`AIR_GAPPED_MODE` 控制严格离线；`*_MODEL_NAME` 必须与 UI 登记名一致（使用 `model-scripts` 时应填 `served_model_name`）。必须（MUST）在 `docs/airgap-operations.md` 指向设置 → 模型设置检查清单，并简述上述命名对齐。

#### Scenario: 阅读 env 示例

- **WHEN** 阅读 `.env.example` 的 `MODEL_PROFILE` 注释
- **THEN** 明确非自动流量开关，并指向模型设置与名称对齐要求

#### Scenario: 阅读离线运维手册

- **WHEN** 阅读 `docs/airgap-operations.md`
- **THEN** 可找到检查清单入口与 served 名对齐说明

