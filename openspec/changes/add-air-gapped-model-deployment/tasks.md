## 1. 前置基线与部署清单

- [x] 1.1 确认前六个变更已使用线上模型完成适用回归，并冻结模型、数据集、提示、阈值和报告快照（`evidence/task-1.1-online-baseline-freeze-confirm-20260812.md` + `evidence/frozen-inputs-v1.json`）
- [x] 1.2 盘点 Chat、Embedding、Rerank、VLM、ASR、TTS、验证/反思、评测裁判和可选解析/OCR 角色的内网模型、推理引擎、协议、许可与硬件需求
- [x] 1.3 定义私有化部署清单 schema，覆盖 `desktop-lite`、`compose-airgap`、`helm-airgap`、应用/数据组件/模型的服务端可信位置、镜像/桌面制品、架构、来源、许可证、校验和和导入步骤

## 2. 模型部署语义与能力

- [x] 2.1 扩展模型配置，分别保存 `protocol`、服务端判定的 `location`、`artifact_policy`、推理引擎信息和能力清单
- [x] 2.2 增加数据库迁移和兼容层，将旧 `source=local` 映射为同机 Ollama，并保留旧 API 一个迁移周期
- [x] 2.3 实现包含评测裁判与可选解析/OCR 的模型角色所需能力与实际能力匹配，并接入保存和启用流程
- [x] 2.4 更新模型管理界面，明确区分协议、运行位置、预加载策略和能力测试结果
- [x] 2.5 为旧 Ollama、公共 API 和由 GPUStack、vLLM 等引擎承载的内网 OpenAI-compatible 配置编写迁移测试

## 3. 私有端点与统一传输

- [x] 3.1 增加 `ApprovedEndpoint` 实体、管理员 CRUD、可穷举的模型/搜索/数据连接器/对象存储/遥测服务类别、允许用途和审计记录
- [x] 3.2 将批准端点注册表与部署级 `SSRF_WHITELIST` 的地址、服务类别和用途一致性校验接入各配置边界
- [x] 3.3 禁止普通租户连接未注册或用途不匹配的私网地址，并为 DNS rebinding、重定向和跨用途滥用编写安全测试
- [x] 3.4 实现共享模型 HTTP transport，统一超时、取消、TLS、连接池、受控 Header 和连接期 SSRF 校验
- [x] 3.5 将 Chat、Embedding、Rerank、VLM、ASR、TTS、验证/反思、评测裁判和可选解析/OCR 模型的 HTTP 适配器迁移到共享 transport
- [x] 3.6 从规范化端点、`ApprovedEndpoint` 及连接期 DNS/IP 解析结果派生运行位置，忽略普通用户伪造的 `location`
- [x] 3.7 将搜索、数据连接器、S3 兼容存储和自托管遥测配置改为引用用途匹配的 `ApprovedEndpoint`，并在每次连接和重定向时复验目标（搜索、连接器、Langfuse 遥测以及 MinIO/S3/TOS/OSS SDK 已统一绑定批准端点客户端；`TestApprovedStorageHTTPClientRejectsRedirectToDifferentPort`、已有连接器/传输回归通过）

## 4. 模型角色预检

- [x] 4.1 实现 Chat 流式、结构化输出和工具调用能力探针
- [x] 4.2 实现 Embedding 维度、Rerank 顺序和 VLM 图像输入能力探针
- [x] 4.3 实现 ASR 转写、TTS 音频合成、验证/反思与评测裁判结构化输出，以及启用时解析/OCR 输入能力探针
- [x] 4.4 实现整体预检命令/API，检查模型身份、延迟、并发、存储、数据库、证书和必要资源
- [x] 4.5 输出按模型角色区分通过、不支持、缺少资源和执行失败的可审计矩阵

## 5. 严格离线运行

- [x] 5.1 增加 `AIR_GAPPED_MODE` 配置并在启动时执行默认拒绝的外部依赖校验；任何已启用、未批准或解析到公网的依赖使预检失败
- [x] 5.2 在严格离线模式拒绝公共模型、公共搜索/数据同步/遥测/存储，同时允许并校验管理员批准的内网搜索、数据连接器、S3 和自托管遥测
- [x] 5.3 禁止内网模型失败时回退公共云，并只允许显式批准的内网备用模型
- [x] 5.4 修改 Ollama 模型检查，使 `preloaded-only` 缺失时失败且不调用 `PullModel`
- [x] 5.5 审计并禁止容器、解析器、前端和运行时依赖的隐式联网下载
- [x] 5.6 增加出站连接审计和测试，证明严格模式不产生成功公网连接
- [x] 5.7 在 `desktop-lite` 严格离线模式禁用 GitHub 自动更新与后台版本请求，并提供离线介质更新提示

## 6. 离线介质与部署配置

- [x] 6.1 创建固定镜像摘要/不可变版本的 Compose 离线 override 与 Helm 离线 values/lock，禁止正式离线清单使用 `latest` 且不改写通用开发默认值
- [x] 6.2 实现离线打包命令，导出桌面 Lite 制品、所选镜像、Chart、Compose override、Helm lock、迁移、静态资源、配置模板和文档
- [x] 6.3 为离线包生成架构、来源和 SHA-256 清单，并在导入前强制校验
- [x] 6.4 实现模型权重可再分发判断；不可再分发时只输出客户自行导入步骤和校验要求
- [x] 6.5 确保离线包和配置导出排除真实密钥，并支持 Compose secret file 与 Helm existing Secret 注入
- [x] 6.6 增加从本地 Docker 介质和内网镜像仓库导入部署的脚本与验证
- [x] 6.7 编写备份、升级、回滚、模型替换和 Embedding 维度变化重建索引的操作手册
- [x] 6.8 为桌面 Lite 编写本地数据备份、离线介质升级、回滚和自动更新禁用说明

## 7. 内网切换与最终验收

- [x] 7.1 在联网测试环境使用内网模型配置演练预检和切换，排除配置问题后再关闭公网
- [x] 7.2 在无公网出口同一主机部署 `desktop-lite`、全部必需数据组件和模型端点，验证其位置均为 `same-host` 后完成适用角色预检、核心端到端冒烟和 `single-node` 验收
- [x] 7.3 在无公网出口环境从离线介质部署 `compose-airgap`，完成全角色预检和核心端到端冒烟
- [x] 7.4 在无公网出口环境从内网镜像仓库部署 `helm-airgap`，完成全角色预检和核心端到端冒烟
- [x] 7.5 使用与线上基线相同的冻结套件，对三种 profile 重跑适用的文档导入、检索、问答、图谱、验证、语音和性能门禁，服务器负载仅由 `server-load` 结果判定（`evidence/acceptance-gates/*/offline-frozen-suite-live-20260812.json`，三 profile gate=passed）
- [x] 7.6 生成线上与内网差异报告、出站连接审计、桌面/镜像/模型校验和及最终验收材料（`evidence/online-offline-diff-20260812-final-live.json` gate=passed）
- [x] 7.7 演练模型绑定和部署配置回滚，确认失败运行与回滚操作可追踪
- [x] 7.8 使用批准的 `private-network` 模型运行 `desktop-lite` 适用离线功能测试，验证功能结果可保存但 `single-node` 门禁失败并列出非同机端点

## 8. 冻结套件全链路离线验收

- [x] 8.1 固化线上基线套件、提示、阈值、模型身份、路由 taxonomy 和报告快照，并让三种离线 profile 复用同一套输入（`evidence/frozen-inputs-v1.json`，三 profile dry-run 复用同一 freeze_sha256）
- [x] 8.2 使用同一端到端验收执行器覆盖适用的文档入库、检索、复杂度路由、图谱版本可见性、验证补检索、语音连续交互和性能门禁（`scripts/offline-frozen-suite.ps1` live，三 profile `frozen_e2e_suite=passed`）
- [x] 8.3 分别计算 `single-node` 与 `server-load` 门禁，保存组件位置、出站审计、失败案例、图谱/验证/语音遥测和完整性校验（desktop `single-node=passed`/`server-load=n/a`；compose/helm 两者均 `passed`）
- [x] 8.4 编写线上/离线冻结输入一致性、断网全链路、私网模型导致单机失败、服务器负载 profile 隔离和差异报告测试（`scripts/airgap-acceptance-contract.tests.ps1` 等通过）
