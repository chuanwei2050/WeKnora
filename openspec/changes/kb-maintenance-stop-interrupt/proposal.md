## Why

知识库「全部重建 / 重新解析」点停止后，系统会进入 `canceling` 并等待在途文档自然收尾，维护锁长时间不释放，用户无法立刻进行其他维护操作。需要改为停止时立即中断在途目标文档并放锁，避免长时间等待。

## What Changes

- 文档管线类维护（`reparse` / `rechunk`）停止时，将本轮目标中仍为 `pending` / `processing` 的文档标记为失败（用户中断），**不删除**知识库文档本身。
- 维护进度直接进入 `canceled` 并释放维护锁，不再长时间停留在 `canceling` 等待在途收尾。
- 文档处理 worker 识别用户停止/重建中断文案后跳过残留任务，避免把已中断文档重新写回 `processing`。
- 前端停止成功提示改为「在途已中断、维护已解锁」；`canceling` 文案仅作兼容残留状态。

## Capabilities

### New Capabilities

- `kb-maintenance-stop`: 知识库文档管线维护任务的停止语义——中断在途目标文档、立即放锁、worker 防复活。

### Modified Capabilities

- （无）现有 `openspec/specs/` 中无对应维护停止需求规格。

## Impact

- 后端：`CancelMaintenance`、`KBMaintenanceStore.applyKBMaintenanceCancel`、知识服务中断辅助方法、`ProcessDocument` 幂等跳过逻辑、相关单测。
- 前端：维护设置页 i18n（`zh-CN` / `en-US`）停止提示文案。
- API：`POST .../maintenance/cancel` 响应语义变化（document-pipeline 停止后更快变为 `canceled`）；无破坏性路径参数变更。
