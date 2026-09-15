## Context

知识库维护面板支持「全部重建」（`reparse`）与「重新解析」（`rechunk`）等文档管线任务。当前停止逻辑为：停止领取后续任务后进入 `canceling`，维护锁一直持有到在途 `pending`/`processing` 文档自然结束。用户在在途文档较多（例如数十篇处理中）时会长时间无法启动其他维护操作。

相关实现集中在：

- `internal/handler/knowledgebase.go`：`CancelMaintenance` / `GetMaintenanceStatus`
- `internal/application/service/kb_maintenance_progress.go`：锁与进度状态机
- `internal/application/service/knowledge.go`：`InterruptStuckParses`、`ProcessDocument`
- `frontend/src/views/knowledge/settings/KBMaintenanceSettings.vue` 与 i18n

## Goals / Non-Goals

**Goals:**

- 用户停止文档管线维护后，本轮目标中未完成文档立即标记为失败（用户中断）。
- 维护进度立即变为 `canceled` 并释放锁，UI 可马上开启下一次维护。
- 残留队列 worker 不得把已中断文档重新写回 `processing`。
- 知识库文档行本身保留，不删除。

**Non-Goals:**

- 强杀正在执行的 OS/网络 IO（仅在 worker 下次检查状态时跳过即可）。
- 改变 `vector` / `keywords` / `questions` / `structured` / `graph` 等非文档管线维护的停止语义（它们已是立即 `canceled`）。
- 提供「等待收尾」与「立即中断」双模式开关。

## Decisions

### 1. 停止即中断目标文档，而不是等收尾

- **选择**：Cancel 时对 `TargetKnowledgeIDs` 中仍为 `pending`/`processing` 的文档写入失败 + 专用中断文案，然后进度直接 `canceled`。
- **理由**：与用户期望一致（快停、不丢文件）；已有 `InterruptStuckParses` 模式可复用。
- **备选**：仅改 Redis 状态放锁（文档可能长期卡在处理中）；短等待后再强停（仍要等，实现更复杂）。均不采用。

### 2. 使用按 ID 作用域中断，而非整库 `InterruptStuckParses`

- **选择**：新增按 ID 列表中断（如 `InterruptParsesByIDs`），只处理本轮维护目标。
- **理由**：避免误伤同库中与本次维护无关的 pending/processing 文档。
- **备选**：直接调用 KB 级 `InterruptStuckParses`。范围过大，不采用。

### 3. 取消专用中断文案，并扩展 worker 跳过集合

- **选择**：新增 `ParseInterruptedByUserCancelMessage`；`ProcessDocument` 对用户取消与 `ParseInterruptedForRebuildMessage` 与现有 rechunk 中断一样直接 skip。
- **理由**：当前仅 rechunk 中断文案会跳过；其它 failed 文案仍可能被 worker 改回 `processing`。
- **备选**：用通用「任意 failed 都跳过」。会改变正常失败重试语义，过宽，不采用。

### 4. 去掉 document-pipeline 的长时间 `canceling` 特殊分支

- **选择**：`applyKBMaintenanceCancel` 对 reparse/rechunk 与其它操作一致，直接 `canceled` + `FinishedAt`。
- **理由**：中断后 `active` 会归零，无需再靠 `canceling` 占锁等收尾。
- **备选**：保留 `canceling` 仅作 UI 过渡。增加状态复杂度且用户仍可能看到锁定提示，不采用。前端类型可保留 `canceling` 以兼容旧 Redis 残留。

### 5. 中断失败时仍优先放锁

- **选择**：中断过程 best-effort；即便部分文档更新失败，也尽量把维护状态写成 `canceled`，避免 UI 永久锁死；错误记日志并可在响应中提示。
- **理由**：卡住锁比个别文档状态短暂不一致更伤用户体验；用户可再次重建或手动重试。

### 6. 停止时只清重建相关 asynq / Redis，避免 lease recover 异常

- **选择**：Cancel 后按 `TargetKnowledgeIDs` 清理：
  - Redis `multimodal:pending:{id}` / `multimodal:pending:{id}:*`
  - asynq 中类型为 `document:process` / `image:multimodal` / `knowledge:post_process` 且 payload 命中目标 ID 的 pending/retry/scheduled/archived 任务（队列：`default` / `document-large` / `low` / `critical`）
- **不清理**：wiki / 数据源同步 / 会话等非重建任务；不整队列 Flush。
- **理由**：残留任务 + 已删除 Redis 元数据会导致 asynq recoverer `lease expired … NOT FOUND`；定向删除可降低该噪音，同时不误伤其他业务。
- **在途 active**：不强杀；worker 识别用户中断文案后跳过，且不再因 multimodal finalize 把文档写回完成态。

## Risks / Trade-offs

- [竞态：worker 在中断后仍写出 completed] → 接受极少数「几乎完成」的文档最终成功；下次状态检查对 cancel/rebuild 中断文案 skip，防止 failed→processing 复活。
- [在途任务仍短暂消耗 CPU/模型] → 不强杀进程；enqueue 已停，worker 尽快跳过；pending/retry 队列任务被定向删除。
- [失败文档增多] → 文案标明「用户停止重建」；用户可再次「全部重建」覆盖。
- [旧客户端仍展示 canceling] → 主路径不再进入；保留兼容文案即可；status 轮询会对 canceling 执行中断+purge+放锁。
- [asynq DeleteTask 与 recover 竞态] → best-effort 删除 + 中断标记双保险；不整库 flush。

## Migration Plan

- 纯行为变更，无需数据迁移。
- 部署后：正在 `canceling` 的旧任务仍按现有 progress 轮询收尾逻辑结束；新停止走立即中断。
- 回滚：恢复 `applyKBMaintenanceCancel` 的 canceling 分支并去掉 Cancel 时的中断调用即可。

## Open Questions

- 无阻塞问题。若后续需要「等待收尾」模式，可另开变更加显式选项，不在本次范围。
