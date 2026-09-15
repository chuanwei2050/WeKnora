## Context

知识库文档列表（列表视图与卡片更多菜单）当前对非 `pending_review` 文档展示「重建」。解析中（`pending` / `processing`）时，前端 `handleKnowledgeReparse` 会提示「重建进行中」而拒绝再次重建，但操作列仍可能显示「重建」，且没有单文档/批量「停止解析」入口。

多模态文档解析会入队 asynq `image:multimodal`，并用 Redis `multimodal:pending:{knowledgeID}` / `multimodal:pending:{knowledgeID}:*` 跟踪待处理图片。停止后若不清理，残留任务可能复活文档或产生 lease/NOT FOUND 噪音。

维护停止能力（change `kb-maintenance-stop-interrupt`）已提供：

- `InterruptParsesByIDs`：按 ID 将 pending/processing 标为 failed
- `PurgeDocumentPipelineTasks`：定向清理文档管线 asynq（含 `image:multimodal`）与 `multimodal:pending:*`
- `IsDeliberateParseInterrupt`：worker 跳过故意中断文案

本 change 在单文档与批量维度复用上述能力，不经过维护锁 / maintenance progress。

相关入口：

- `frontend/src/views/knowledge/components/DocumentListView.vue`
- `frontend/src/views/knowledge/components/DocumentBatchBar.vue`
- `frontend/src/views/knowledge/KnowledgeBase.vue`
- `internal/handler/knowledge.go`（`ReparseKnowledge` 旁新增 stop）
- `internal/router/router.go`
- `internal/application/service/kb_pipeline_purge.go`

## Goals / Non-Goals

**Goals:**

- 解析中文档：隐藏「重建」、显示「停止」（列表 + 卡片更多菜单）。
- 多选解析中文档：批量栏提供「停止」。
- 停止需确认；确认后中断目标文档解析（failed + 用户停止文案），保留知识库记录。
- **单文档与批量停止均**清理目标 ID 的 `image:multimodal` asynq 任务与 `multimodal:pending:*` Redis key（以及同管线的 `document:process` / `knowledge:post_process`）。
- 残留 worker 不得把已停止文档写回 `processing`。

**Non-Goals:**

- 强杀正在执行的 OS/网络 IO。
- 修改 KB 级「全部重建 / 重新解析」维护停止语义（已由独立 change 覆盖；但其 purge 语义与本 change 对齐）。
- 停止后自动删除文档或自动重新排队。

## Decisions

### 1. 单文档 + 批量两套 API，共享同一服务编排

- **选择**：
  - `POST /knowledge/{id}/stop-parse`
  - `POST /knowledge/stop-parse`（或 KB 作用域批量路径，body 含 `ids`）
- 二者均调用同一内部方法：`InterruptParsesByIDs` → `PurgeDocumentPipelineTasks`。
- **理由**：清理逻辑只维护一处；单/批行为一致，尤其 multimodal Redis/asynq。
- **备选**：批量循环调单文档 HTTP。多轮网络且易漏 purge 一致性，不采用。

### 2. 编排：interrupt → purge 任务 → 清理派生产物

- **选择**：停止路径顺序为：
  1. `InterruptParsesByIDs`（failed + 用户停止文案）
  2. `PurgeDocumentPipelineTasks`（asynq 含 `image:multimodal` + Redis `multimodal:pending:*`）
  3. `cleanupKnowledgeResources`（分块 / 向量 / 图谱 / 提取图片 / 预览）+ 清 preview/summary 元数据；wiki 启用时 `prepareWikiForReparse`
- **保留**：知识行、`FilePath` 源文件
- **理由**：用户要求「原文档保留，其他产物和任务该停的停、该删的删」；复用已有 cleanup/purge，避免双份逻辑。
- **备选**：仅标 failed 不删产物。会留下半成品分块/向量，不采用。

### 3. 专用中断文案 `ParseInterruptedByUserStopMessage`

- **选择**：文案为「解析已中断（用户停止）」；加入 `IsDeliberateParseInterrupt`。
- **理由**：与「用户停止重建」区分场景；worker 跳过集合显式覆盖。
- **备选**：复用 `ParseInterruptedByUserCancelMessage`。可用但文案指向重建，体验略差。

### 4. 非解析中的处理

- **单文档**：非 `pending`/`processing` → 拒绝。
- **批量**：跳过非解析中 ID；若无一可停则拒绝；部分成功时返回 interrupted 数量（或等价摘要）。
- **理由**：批量选中常混有已完成文档，全量拒绝体验差。

### 5. 前端：解析中切换按钮 + 确认框（单/批）

- **选择**：行内/卡片解析中显示停止、隐藏重建；批量栏对选中中至少一篇解析中时显示停止；均二次确认。
- **理由**：与产品确认一致，并覆盖「单个批量都要」。

### 6. 清理范围（单 ID 与 ID 列表相同）

| 层 | 清理对象 |
|----|----------|
| asynq | `document:process` / **`image:multimodal`** / `knowledge:post_process` |
| Redis | **`multimodal:pending:{id}`** 及 **`multimodal:pending:{id}:*`** |
| DB/索引 | 分块、向量索引、知识图谱 |
| 文件产物 | 分块提取图片、生成预览（**不删** `FilePath` 源文件） |
| wiki | pending ingest 清洗（不删知识行） |
| 不碰 | 数据源 sync / chat 等 |

## Risks / Trade-offs

- [竞态：worker 在中断后仍写出 completed] → 接受极少数「几乎完成」成功；故意中断文案保证 failed 不被写回 processing。
- [在途 active `image:multimodal` 仍跑] → 不强杀；删 pending 队列任务 + 清 Redis counter；worker 识别中断后 skip。
- [批量 ID 很多时 purge 扫描成本] → 与维护停止同路径；可接受；必要时后续再优化 inspector 扫描。
- [用户以为停止=删除] → 确认框文案写明文档保留为失败。

## Migration Plan

- 纯增量：新 API + UI 分支；无 schema 迁移。
- 回滚：隐藏停止按钮并下线路由即可；已中断文档仍为 failed，可手动重建。

## Open Questions

- （无）产品决策已确认：失败保留、需确认、列表+卡片、单/批均清 multimodal。
