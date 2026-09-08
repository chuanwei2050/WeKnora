## Why

当前 DOCX/PPTX 预览在浏览器主线程中解压并一次性构建整份文档 DOM。复杂文档只能被前端直接拒绝；若简单放宽限制，即使设置 JavaScript 超时也无法中断已经阻塞的主线程，仍可能导致页面长时间无响应。

## What Changes

- 新增持久化、可去重的异步文档预览任务，由现有 Asynq Worker 执行耗时转换。
- DOCX 不超过 100 MiB 时沿用项目现有 `docx-preview`；超过 100 MiB 时仅异步生成前 30 页 PDF 部分预览并保存至当前知识库对应的对象存储。
- 新增创建任务、查询状态、取消任务和读取预览产物的 API；所有入口复用文档与知识库权限校验。
- 为转换子进程设置时限、输出大小、页数、并发和资源上限；失败或超限不会阻塞 API 进程或浏览器页面。
- 前端对不超过 100 MiB 的 DOCX 保留即时预览；更大的 DOCX 自动提交前 30 页后台任务，完成后展示部分 PDF，并明确引导用户下载原文件查看完整内容，不提供完整 PDF 生成入口。
- 对超过 256 MiB 的上传、解析、预览和下载统一分流：上传采用可恢复的对象存储分片路径，解析/预览使用独立单并发队列，下载使用签名 URL 或流式响应。
- 修复大文件下载继承 30 秒前端请求超时及整文件 Blob 缓冲问题，并展示可用的服务端错误信息。

## Capabilities

### New Capabilities

- `asynchronous-document-preview`: 定义复杂 Office 文档的异步转换、状态查询、取消、产物读取、资源隔离、缓存复用与前端降级行为。

### Modified Capabilities

无。

## Impact

- 后端：`internal/types/task.go`、Asynq 路由、知识服务/处理器、HTTP 路由、对象存储访问与 Redis 任务状态。
- 前端：文档预览组件、知识库 API、轮询与分页/流式预览状态、多语言文案。
- 部署：应用 Worker 镜像需包含 LibreOffice Writer；Lite 模式需注册同一任务处理器。
- API：新增异步预览端点，不改变既有 `/knowledge/:id/preview` 与 `/download` 的兼容行为。
