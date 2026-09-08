## Context

`document-preview.vue` 当前把原始 DOCX/PPTX 下载到浏览器，并由第三方库在主线程完成解压和整份 DOM 渲染。现有 Worker 只检查 ZIP 中央目录：解压总量 20 MiB、1000 个条目、20 倍压缩比；检查不通过便拒绝预览。此检查能挡住明显的高风险输入，但无法让大型合法文档可用，也无法中断已经进入 DOM 排版阶段的同步工作。

项目已有 Redis、Asynq、对象存储抽象、Lite 同步任务执行器以及文档处理镜像。异步预览应复用这些基础设施，并在任务进程内调用 LibreOffice Writer 将 Office 文档转换为 PDF。PDF 由浏览器原生查看器按需读取，不再为每个段落创建应用 DOM。

## Goals / Non-Goals

**Goals:**

- 复杂 DOCX/PPTX 的转换不占用浏览器主线程和 HTTP 请求生命周期。
- 页面刷新、多实例 API 和 Worker 重启后仍能查询同一个任务。
- 同一文档内容版本只生成一份可复用产物。
- 超时、内存超限、输出过大、恶意压缩包或转换器崩溃均被隔离，并有明确失败状态和文本降级。
- 预览产物与原文使用相同的租户、知识库和文档授权边界。

**Non-Goals:**

- 不保证 LibreOffice 与 Microsoft Office 的排版逐像素一致。
- 不允许超过硬上限的文档通过“仍要尝试”绕过保护。
- 首期不为所有格式建立统一转码服务；PDF、图片、文本和表格继续使用现有路径。

## Decisions

### 1. 风险分级决定即时渲染或异步转换

压缩后文件大小不作为唯一标准。DOCX 同时满足“原文件不超过 8 MiB、解压声明总量不超过 20 MiB、条目不超过 1000、压缩比不超过 20”时可继续浏览器即时预览；任一软阈值超出即进入异步预览。ZIP64、目录损坏、单条目声明异常或压缩比超过 100 直接拒绝转换并只提供文本降级。

选择 8 MiB 是为了限制网络传输和浏览器内存副本；保留现有 20 MiB/1000/20 作为已经验证的 DOM 风险界线。替代方案“仅提高前端阈值并加定时器”不可行，因为同步 DOM 排版期间事件循环无法执行取消逻辑。

### 2. Asynq 任务与内容版本去重

新增 `document:preview` 任务。任务 ID 由 `tenant_id + knowledge_id + content_revision` 的 SHA-256 摘要生成；提交接口使用 Asynq `TaskID` 去重。状态写入 Redis，包含 `queued/running/succeeded/failed/cancelled/expired`、阶段、进度、错误码、产物路径、页数、字节数和过期时间。状态 TTL 为 24 小时，成功产物 TTL 默认 24 小时；再次访问已完成且版本一致的任务时直接复用。

不使用 API 进程内 goroutine 或内存 map，因为它们无法跨实例、无法恢复且部署重启会丢失状态。

### 3. Worker 在受控子进程中生成单个 PDF

Worker 从知识库对应存储读取原文件到独立临时目录，使用 `exec.CommandContext` 启动 `soffice --headless --convert-to pdf`。异步任务分为两档：普通档默认 300 秒、200 MiB PDF、2000 页；原文件超过 256 MiB 时进入超大文档档，允许最大 1 GiB 输入、4 GiB 声明解压量、50000 个压缩条目、900 秒、500 MiB PDF 和 5000 页。超大档进入独立队列，每个 Worker 同时只执行 1 个任务，并配置 4–6 GiB 内存。转换后先验证 PDF 头、文件大小和页数，再写入原知识库对应对象存储。

选择 PDF 而不是服务端 HTML：PDF 由浏览器原生按需加载、DOM 数量稳定，且 LibreOffice 的 PDF 导出比 HTML 更能保持分页和版式。首期保存单个 PDF，依赖 HTTP Range；不提前拆成每页图片，避免显著增加存储与转换时间。

### 4. API 状态机与权限

- `POST /api/v1/knowledge/:id/preview-tasks`：校验查看权限并创建或复用任务，返回 `202` 或现有成功状态。
- `GET /api/v1/knowledge/:id/preview-tasks/:task_id`：返回当前状态。
- `DELETE /api/v1/knowledge/:id/preview-tasks/:task_id`：标记取消并撤销待执行任务。
- `GET /api/v1/knowledge/:id/preview-artifact`：仅当当前内容版本对应任务成功时流式返回 PDF，支持 Range。

每个接口都先按当前租户读取知识条目并复用知识库 Viewer 权限校验；不能仅凭 task ID 或对象路径读取产物。错误响应使用稳定错误码供前端展示和决定是否降级。

### 5. 前端只管理任务，不执行复杂文档排版

Office 安全检查超过软阈值后，前端不再下载完整原文用于渲染，而是提交任务并以退避间隔轮询。进行中展示阶段和进度，保留首批已解析文本；成功后将受鉴权的 PDF 产物作为 Blob URL 交给 iframe；失败/取消/过期时展示原因、重试和“查看完整文本”。组件卸载只停止轮询，不默认取消共享任务。

### 6. 下载链路独立修复

原文件下载不继承 Axios 的 30 秒默认超时。超过 256 MiB 时，API 在完成文档权限校验后返回短期签名 URL；不支持签名 URL 的存储实现由 API 流式代理，前端通过浏览器导航触发保存，不再使用 Axios `responseType: blob`。权限、对象不存在和网关错误仍由服务端控制并返回。

### 7. 超大上传与解析使用独立数据路径

不超过 256 MiB 的上传保留现有 multipart API。超过 256 MiB、最大 1 GiB 的文件先创建可恢复上传会话，由前端通过对象存储分片签名直接上传；完成接口重新校验租户、知识库权限、对象大小和客户端计算的文件哈希，再创建知识条目。上传会话和已完成分片记录写入 Redis 并设置 TTL，取消或过期对象由清理任务删除。

文档创建后按持久化的 `file_size` 路由：普通文件进入 `default`，超过 256 MiB 进入独立 `document-large` 单并发队列，任务超时 30 分钟。解析与预览转换使用不同任务类型和状态，不互相等待；解析完成后仍按现有流程进行切分和索引。Nginx 对兼容 multipart 路径设置明确的 `client_body_timeout`，大文件直传路径不经过 Nginx 请求体缓冲。

## Risks / Trade-offs

- [LibreOffice Writer 增加镜像体积] → 仅安装无推荐依赖的 Writer/PDF 导出所需包，并在启动健康检查中报告能力；缺失时任务返回 `converter_unavailable`。
- [转换器可能处理恶意文件] → 独立临时目录、非 root 用户、无网络、超时、容器内存/CPU 限制、输入预检和输出硬上限共同约束。
- [超大文档长期占用 Worker] → 超过 256 MiB 的输入进入独立单并发队列，不挤占普通预览任务；900 秒到期后强制终止。
- [大文件经 API/Nginx 双重落盘] → 超过 256 MiB 改为对象存储分片直传；兼容上传显式设置请求超时和临时磁盘监控。
- [前端 Blob 下载产生数倍内存占用] → 大文件使用短期签名 URL 或浏览器原生流式下载，JavaScript 不持有完整内容。
- [对象存储不支持 Range] → API 层实现 Range 转发或流式读取；不把整份 PDF 缓存在 API 内存。
- [Redis 状态过期但对象仍存在] → 产物路径包含内容版本，清理任务按 TTL 删除孤儿产物；读取必须由当前状态记录授权。
- [文档更新与旧任务竞态] → Worker 完成写状态前重新校验 `content_revision`；不一致则丢弃产物并标记过期。
- [Lite 模式无 Redis] → 使用现有同步执行器注册相同 handler；状态存储采用进程内实现，仅作为单实例降级并明确重启后可重试。

## Migration Plan

1. 先发布任务类型、状态存储、Worker 和 API，功能开关默认关闭。
2. 更新 Worker 镜像并验证 `soffice --headless --version`、超时终止和对象存储 Range。
3. 发布前端轮询流程，对 DOCX 灰度开启；保留旧即时预览作为小文件路径。
4. 观察任务时长、失败码、产物大小、队列深度和 Worker OOM 指标后再开启 PPTX。
5. 回滚时关闭功能开关，前端恢复即时预览/文本降级；已生成产物由 TTL 清理，不需数据库回滚。

## Open Questions

- 生产环境对象存储的 Range 支持需在部署验收中确认；不支持时启用 API Range 代理。
- Windows 原生部署无法强制 Unix `rlimit`，需依赖 Job Object 或仅提供超时保护；容器部署是资源隔离的基准环境。
