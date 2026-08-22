## Context

项目已具备 `ONLINE_*` / `OFFLINE_*` 分套 env、模型设置 UI、以及 `AIR_GAPPED_MODE` 严格离线门禁。`MODEL_PROFILE` 目前只是 `.env` 标记，不参与运行时选模。

本变更是 `add-air-gapped-model-deployment` 完成后的运维可见性增强。

现状约束（实现必须遵守）：

- 登记身份字段是 `types.Model.Name`（JSON `name`）；前端把表单 `modelName` 写入该字段。
- 内网部署脚本 `model-scripts/config.yaml.example` 使用 Hub `model_id` 与对外 `served_model_name` 两套名字；WeKnora 登记与 env 期望名应对齐**实际 OpenAI 请求用的名字**（通常是 `served_model_name`），而不是未服务的 Hub id。
- 前端仅支持添加：`chat`→`KnowledgeQA`、`embedding`、`rerank`、`vllm`→`VLLM`、`asr`、`tts`；可用现有编辑对话框改维度等。
- `ListModels` 内部使用 `MustTenantIDFromContext`；handler 必须先安全检测租户，避免 panic。
- `godotenv.Load()` 不展开 `${VAR}`，对照逻辑必须自行展开。
- 当前 `.env.example` 中 `OFFLINE_RERANK/ASR/TTS` 常为空，而 `model-scripts` 可能已启用对应服务 → 清单应显示 `missing_env` 直到 env 补齐，这是预期而非缺陷。

## Goals / Non-Goals

**Goals:**

- 只读 status API + 模型设置清单/引导。
- 文档澄清 profile 语义，以及 offline 名称与登记名对齐。

**Non-Goals:**

- 自动写库重绑；新增 Verifier/Judge/VLM 管理分区；改 chat 流水线；改 `model-scripts` prepare/deploy；v1 因 base_url 不同判 `mismatch`。

## Decisions

1. **API**：`GET /api/v1/system/model-profile-status`（认证 `/api/v1/system`）。

2. **匹配**：`TrimSpace(Model.Name) == TrimSpace(expected_name)` + 可接受 `ModelType` 优先级；同优先级多条按 `CreatedAt` 升序、再按 `ID` 升序取第一条。  
   - `mismatch` **仅** embedding：期望维度与匹配模型 `parameters.embedding_parameters.dimension` 均 >0 且不等。  
   - 不因 base_url 判 mismatch；响应仍返回 `expected_base_url` 供人工核对。

3. **角色表**（`{P}`=`ONLINE`|`OFFLINE`）

   | role | env 词干 | 可接受类型（左优先） | `add_dialog_type` |
   |------|----------|----------------------|-------------------|
   | `chat` | `{P}_LLM_MODEL` | `KnowledgeQA` | `chat` |
   | `verifier_1` | `{P}_VERIFIER_MODEL_1` | `Verifier`, `KnowledgeQA` | `chat` |
   | `verifier_2` | `{P}_VERIFIER_MODEL_2` | `Verifier`, `KnowledgeQA` | `chat` |
   | `evaluation_judge` | `{P}_EVALUATION_JUDGE_MODEL` | `EvaluationJudge`, `KnowledgeQA` | `chat` |
   | `embedding` | `{P}_EMBEDDING_MODEL` + `{P}_EMBEDDING_MODEL_DIMENSION` | `Embedding` | `embedding` |
   | `rerank` | `{P}_RERANK_MODEL` | `Rerank` | `rerank` |
   | `vlm` | `{P}_VLM_MODEL` | `VLM`, `VLLM` | `vllm` |
   | `asr` | `{P}_ASR_MODEL` | `ASR` | `asr` |
   | `tts` | `{P}_TTS_MODEL` | `TTS` | `tts` |

4. **Env 规范化**：多轮 `${VAR}` 展开；空名 / `__FILL_` / 残留 `${...}` → `missing_env`；名称有效时 base_url 可空并进入登记对照。

5. **Profile**：空白→`online`+valid；`online`/`offline`→valid；其他非空→有效 `online`+`profile_valid=false`。  
   **Air-gap**：仅当 `AIR_GAPPED_MODE`（trim，大小写不敏感）为 `true` 时 `air_gapped=true`，否则 false。

6. **Actions**  
   - `missing_registration` → `intent=add` + `add_dialog_type`  
   - `mismatch` 且已有 `matched_model_id` → `intent=edit` + `add_dialog_type` + `matched_model_id`（前端打开编辑对话框）  
   - `missing_env` → 无 action

7. **响应最小字段（锁定）**

```text
profile, profile_raw, profile_valid, air_gapped,
summary: { ok, missing_env, missing_registration, mismatch },
roles[]: {
  role, expected_name, expected_source, expected_base_url, expected_dimension,
  status, gap_reason, matched_model_id, matched_model_name, matched_model_type
},
actions[]: { id, role, intent, add_dialog_type, matched_model_id }
```

禁止出现 api_key / secret 类字段。

8. **Handler**：先用 `TenantIDFromContext`；缺失则错误返回。有租户后再 `ListModels`。解析/对照纯函数单测。

9. **命名对齐**：文档写明 `OFFLINE_*_NAME`（及 online）必须等于 UI/`Model.Name` 所用服务名；若用 `model-scripts`，填 `served_model_name`，不要只填未登记的 Hub `model_id`。

## Risks / Trade-offs

- [Hub id ≠ served name 导致假 missing_registration] → 文档与清单 `gap_reason` 可提示核对 served 名。  
- [`${VAR}` 未展开] → 实现展开 + 单测。  
- [ListModels 前未查租户会 panic] → handler 强制先查。  
- [不做 URL mismatch] → air-gap 与人工核对弥补。

## Migration Plan

纯增量；回滚移除路由/UI/DI 即可。

## Open Questions

无阻塞项。
