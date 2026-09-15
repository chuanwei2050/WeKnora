## 1. 中断文案与 worker 识别

- [x] 1.1 在 `internal/types/kb_maintenance.go` 新增 `ParseInterruptedByUserStopMessage`（「解析已中断（用户停止）」）
- [x] 1.2 将其纳入 `IsDeliberateParseInterrupt`，并补充类型单测
- [x] 1.3 确认 `image:multimodal` / post_process worker 对故意中断文案跳过（与维护停止一致）

## 2. 单文档停止 API

- [x] 2.1 服务层编排：校验解析中 → `InterruptParsesByIDs([id], stopMessage)` → `PurgeDocumentPipelineTasks([id])` → 产物清理
- [x] 2.2 Handler + 路由：`POST /knowledge/{id}/stop-parse`（Editor 权限，对齐 reparse）
- [x] 2.3 非 pending/processing 返回业务错误；成功返回更新后的 knowledge
- [x] 2.4 单测：中断 + 调用 purge；非解析中拒绝

## 3. 批量停止 API

- [x] 3.1 服务层对 ID 列表：仅中断 pending/processing；对实际中断的 ID 调用 purge + 产物清理
- [x] 3.2 Handler + 路由：批量 stop-parse（body 含 ids；Editor 权限）
- [x] 3.3 全无可停止目标时拒绝；混合列表只影响解析中文档
- [x] 3.4 单测：批量中断范围 + purge 收到全部被中断 ID

## 4. Purge + 产物清理（单/批共用）

- [x] 4.1 复核 `PurgeDocumentPipelineTasks`：asynq 白名单含 `image:multimodal`；Redis 清 `multimodal:pending:{id}` 与 `multimodal:pending:{id}:*`
- [x] 4.2 停止路径在 purge 后调用 `cleanupKnowledgeResources`（分块/向量/图谱/图片/预览），保留知识行与源文件
- [x] 4.3 单测或断言：单 ID 与多 ID 调用均清理 multimodal；产物清理在缺少依赖时 best-effort 不 panic
- [x] 4.4 确认单文档与批量停止路径都调用同一 purge + artifact cleanup，无旁路遗漏

## 5. 前端 UI

- [x] 5.1 `DocumentListView`：解析中隐藏重建、显示停止；确认后 emit/调用停止
- [x] 5.2 `KnowledgeBase` 卡片更多菜单：同上
- [x] 5.3 `DocumentBatchBar` + 批量处理：选中含解析中时显示停止；确认后调批量 API
- [x] 5.4 API client + i18n（`zh-CN` / `en-US`）：停止确认、成功/失败文案
- [x] 5.5 成功后刷新列表

## 6. 验证

- [x] 6.1 运行相关 Go 单测（interrupt、purge 白名单、stop handler/service）
- [ ] 6.2 手动确认：单篇停止后状态 failed、产物与 `image:multimodal`/`multimodal:pending:*` 已清；批量停止对每篇目标同等清理
