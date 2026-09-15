## 1. 类型与进度状态机



- [x] 1.1 在 `internal/types/kb_maintenance.go` 新增 `ParseInterruptedByUserCancelMessage`

- [x] 1.2 修改 `applyKBMaintenanceCancel`：`reparse`/`rechunk` 停止后直接 `canceled` 并设置 `FinishedAt`

- [x] 1.3 更新 `kb_maintenance_progress_test.go` 以匹配立即 `canceled` 行为



## 2. 按目标 ID 中断文档



- [x] 2.1 在 knowledge 服务接口与实现中新增按 ID 列表中断方法（仅 `pending`/`processing`）

- [x] 2.2 在 `CancelMaintenance` 中对文档管线任务先中断 `TargetKnowledgeIDs`，再执行 Cancel 放锁

- [x] 2.3 补充中断范围单测（只影响列表内未完成文档，不删记录）



## 3. Worker 防复活



- [x] 3.1 扩展 `ProcessDocument` 跳过逻辑：用户停止中断文案与 `ParseInterruptedForRebuildMessage` 均直接 return

- [x] 3.2 补充或调整相关单测，确认残留任务不会写回 `processing`



## 4. 前端文案



- [x] 4.1 更新 `zh-CN` / `en-US` 的 `stopSubmitted`（及必要时 `canceled`）提示为「在途已中断、维护已解锁」语义

- [x] 4.2 确认停止成功主路径展示 `canceled`，不再依赖长时间 `canceling` 文案



## 5. 验证



- [x] 5.1 运行相关 Go 单测（maintenance progress、interrupt、document process skip）

- [x] 5.2 手动或集成确认：停止全部重建后维护操作可立即解锁，在途目标变为失败中断且文档仍在库中

## 6. 重建相关队列 / Redis 定向清理

- [x] 6.1 新增 `PurgeDocumentPipelineTasks`：仅删除目标文档的 `document:process` / `image:multimodal` / `knowledge:post_process` 与 `multimodal:pending:*`
- [x] 6.2 Cancel / canceling 自愈路径调用 purge；多模态与 post-process worker 识别用户中断后跳过
- [x] 6.3 单测覆盖任务类型白名单（不误伤 wiki/sync）

