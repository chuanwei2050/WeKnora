## Why

知识库文档列表中，处于「解析中」的文档仍可能显示「重建」，用户无法单独或批量停止解析。需要在解析中改为「停止」，并在停止时中断解析、清理对应文档的 Redis（含 `multimodal:pending:*`）与 asynq（含 `image:multimodal`）残留，避免任务复活或噪音错误。

## What Changes

- 新增单文档停止解析 API：`POST /knowledge/{id}/stop-parse`。
- 新增批量停止解析 API（对所选 ID 列表中仍为 `pending`/`processing` 的文档生效）。
- 仅对解析中文档标记为 `failed`（用户停止中断文案），**不删除**知识库文档。
- 停止时（单文档与批量）均 MUST：
  - 定向清理 asynq（含 `image:multimodal`）与 Redis `multimodal:pending:*`
  - 删除派生产物：分块、向量、图谱、提取图片、生成预览
  - **保留**知识行与源文件，状态为失败中断
- 前端列表与卡片更多菜单：解析中隐藏「重建」、显示「停止」；批量栏对选中解析中文档提供「停止」。
- Worker 识别单文档停止中断文案后跳过残留任务，避免写回 `processing`。

## Capabilities

### New Capabilities

- `document-stop-parse`: 单文档与批量停止解析——UI 切换重建/停止、中断状态、清理 Redis/`image:multimodal`、worker 防复活。

### Modified Capabilities

- （无）现有 `openspec/specs/` 中无对应单文档停止需求规格；维护停止能力见独立 change `kb-maintenance-stop-interrupt`，本 change 复用其 purge 实现但不修改其需求。

## Impact

- 后端：`KnowledgeHandler` 新路由（单/批）、knowledge 服务编排 interrupt + purge、中断文案常量、相关单测。
- 前端：`DocumentListView.vue`、`KnowledgeBase.vue`、`DocumentBatchBar.vue`、API client、i18n（`zh-CN` / `en-US`）。
- 依赖复用：已有 `InterruptParsesByIDs`、`PurgeDocumentPipelineTasks`（必须覆盖 `image:multimodal` 与 `multimodal:pending:*`）。
- API：新增单文档与批量 stop-parse；无破坏性路径变更。
