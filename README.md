<p align="center">
  <picture>
    <img src="./docs/images/logo.png" alt="WeKnora Logo" height="120"/>
  </picture>
</p>

<p align="center">
  <picture>
    <a href="https://trendshift.io/repositories/15289" target="_blank">
      <img src="https://trendshift.io/api/badge/repositories/15289" alt="Tencent%2FWeKnora | Trendshift" style="width: 250px; height: 55px;" width="250" height="55"/>
    </a>
  </picture>
</p>
<p align="center">
    <a href="https://weknora.weixin.qq.com" target="_blank">
        <img alt="官方网站" src="https://img.shields.io/badge/官方网站-WeKnora-4e6b99">
    </a>
    <a href="https://chatbot.weixin.qq.com" target="_blank">
        <img alt="微信对话开放平台" src="https://img.shields.io/badge/微信对话开放平台-5ac725">
    </a>
    <a href="https://github.com/Tencent/WeKnora/blob/main/LICENSE">
        <img src="https://img.shields.io/badge/License-MIT-ffffff?labelColor=d4eaf7&color=2e6cc4" alt="License">
    </a>
    <a href="./CHANGELOG.md">
        <img alt="版本" src="https://img.shields.io/badge/version-0.5.0-2e6cc4?labelColor=d4eaf7">
    </a>
</p>

<p align="center">
  <h4 align="center">

  [项目介绍](#-项目介绍) • [功能概览](#-功能概览) • [快速开始](#-快速开始) • [集成方式](#-集成方式) • [开发指南](#-开发指南) • [文档](#-文档)

# 💡 WeKnora — 让文档活起来：RAG、Agent 推理与自动 Wiki 一体化的知识框架

## 📌 项目介绍

**[WeKnora（维娜拉）](https://weknora.weixin.qq.com)** 是一款开源的、基于大语言模型（LLM）的知识管理框架，专为企业级文档理解、语义检索与智能推理场景打造。

WeKnora 将 **RAG 快速问答、ReAct Agent 推理和自动 Wiki** 统一在一套知识工作流中：导入 PDF、Word、图片、网页或外部知识库后，系统完成解析、索引、检索和回答；Agent 可继续调用 MCP 工具与网络搜索处理多步任务。模型、向量数据库、对象存储和部署方式均可替换，支持本地及私有云部署。


## 📱 功能展示

<table>
  <tr>
    <td colspan="2" align="center"><b>💬 智能问答对话</b><br/><img src="./docs/images/qa.png" alt="智能问答对话" width="100%"></td>
  </tr>
  <tr>
    <td width="50%" align="center"><b>📖 Wiki 浏览器</b><br/><img src="./docs/images/wiki-browser.png" alt="Wiki 浏览器" width="100%"></td>
    <td width="50%" align="center"><b>🕸️ Wiki 知识图谱</b><br/><img src="./docs/images/wiki-graph.png" alt="Wiki 知识图谱" width="100%"></td>
  </tr>
  <tr>
    <td width="50%" align="center"><b>🤖 Agent 模式 · 工具调用过程</b><br/><img src="./docs/images/agent-qa.png" alt="Agent 模式工具调用过程" width="100%"></td>
    <td width="50%" align="center"><b>⚙️ 对话设置</b><br/><img src="./docs/images/settings.png" alt="对话设置" width="100%"></td>
  </tr>
  <tr>
    <td colspan="2" align="center"><b>🔭 监控可观测性 · Langfuse Tracing</b><br/><img src="./docs/images/langfuse.png" alt="Langfuse Tracing" width="100%"></td>
  </tr>
</table>

## 🏗️ 架构设计

![weknora-architecture.png](./docs/images/architecture.png)

从文档解析、向量化、检索到大模型推理，全流程模块化解耦，组件可灵活替换与扩展。支持本地 / 私有云部署，数据完全自主可控，零门槛 Web UI 快速上手。

## 🧩 功能概览

**智能对话**

| 能力 | 详情 |
|------|------|
| 智能推理 | ReACT 渐进式多步推理，自主编排知识检索、MCP 工具与网络搜索，支持自定义智能体 |
| 快速问答 | 基于知识库的 RAG 问答，快速准确地回答问题 |
| Wiki 模式 | Agent 驱动从原始文档中自动生成并维护结构化、相互链接的 Markdown Wiki 知识页面 |
| 工具调用 | 内置工具、MCP 工具、网络搜索 |
| 对话策略 | 在线 Prompt 编辑、检索阈值调节、多轮上下文感知 |
| 推荐问题 | 基于知识库内容自动生成推荐问题 |

**知识管理**

| 能力 | 详情 |
|------|------|
| 知识库类型 | FAQ / 文档 / Wiki，支持文件夹导入、URL 导入、标签管理、在线录入 |
| 数据源导入 | 飞书 / Notion / 语雀 知识库自动同步（更多数据源开发中），支持增量与全量同步 |
| 文档格式 | PDF / Word / Txt / Markdown / HTML / 图片 / CSV / Excel / PPT / JSON |
| 检索策略 | BM25 稀疏召回 / Dense 稠密召回 / GraphRAG 图谱增强 / 父子分块 / 多维度索引 |
| 端到端测试 | 检索+生成全链路可视化，评估召回命中率、BLEU / ROUGE 等指标 |

**集成与扩展**

| 能力 | 详情 |
|------|------|
| 模型厂商 | OpenAI / Azure OpenAI / DeepSeek / Qwen（阿里云）/ 智谱 / 混元 / 豆包（火山引擎）/ Gemini / MiniMax / NVIDIA / Novita AI / SiliconFlow / OpenRouter / Ollama |
| 向量数据库 | PostgreSQL (pgvector) / Elasticsearch / Milvus / Weaviate / Qdrant |
| 对象存储 | 本地 / 腾讯云COS / 火山引擎 TOS / MinIO / AWS S3 / 阿里云 OSS |
| IM 集成 | 企业微信 / 飞书 / Slack / Telegram / 钉钉 / Mattermost / 微信 |
| 网络搜索 | DuckDuckGo / Bing / Google / Tavily / Baidu / Ollama |


**平台能力**

| 能力 | 详情 |
|------|------|
| 部署 | 本地 / Docker / Kubernetes (Helm)，支持私有化离线部署 |
| 界面 | Web UI / RESTful API / Chrome Extension / 微信小程序 / 可嵌入聊天 Widget |
| 可观测性 | 集成 Langfuse 以追踪 ReAct 循环、Token 消耗、工具调用和任务流水线 |
| 任务管理 | MQ 异步任务，版本升级自动数据库迁移 |
| 模型管理 | 集中配置，知识库级别模型选择，多租户共享内置模型，WeKnora Cloud 托管模型与文档解析 |

## 🔗 集成方式

### Chrome 插件

[**WeKnora Chrome 插件**](https://chromewebstore.google.com/detail/jpemjbopikggjlmikmclgbmkhhopjdgd)支持在浏览器中直接将网页内容采集到 WeKnora 知识库。选中文本、图片或整个页面，一键保存为知识条目，无需复制粘贴或手动上传文件。


### 微信小程序

[**WeKnora 微信小程序**](./miniprogram/README.md) 提供轻量移动端客户端，支持配置 WeKnora API、选择知识库、导入 URL，并在微信内向知识库提问。


### ClawHub Skill

[**WeKnora ClawHub Skill**](https://clawhub.ai/lyingbug/weknora) 是 WeKnora 发布在 ClawHub 平台上的技能。安装后，可通过 WeKnora REST API 上传文档（文件 / URL / Markdown）、执行混合检索（向量 + 关键词）以及管理知识条目。

- **文档导入** — 通过 Agent 上传文件、导入网页或写入 Markdown 知识
- **混合检索** — 在单个或多个知识库中进行向量 + 关键词混合搜索
- **知识管理** — 以编程方式浏览、编辑和删除知识条目

### 可嵌入聊天 Widget

Widget 用于把 WeKnora 知识问答嵌入已有业务系统。宿主页面加载 `weknora-widget.iife.js` 后会显示悬浮入口，打开后通过隔离的 iframe 展示对话；支持固定知识库、用户可选知识库和全部授权知识库三种范围，并提供移动、缩放、最大化、会话切换及事件回调。

```bash
cd frontend
npm run build:widget
```

构建结果位于 `frontend/dist-widget/`。接入方需要由自己的后端申请短期 bootstrap ticket，浏览器不应持有 integration client secret。完整认证流程、初始化示例和 API 边界参见[外挂知识库对接指南](./docs/外挂知识库指南.md)。

## 🚀 快速开始

### 🛠 环境要求

- [Docker](https://www.docker.com/) & [Docker Compose](https://docs.docker.com/compose/)
- [Git](https://git-scm.com/)

### 📦 安装与启动

```bash
git clone https://github.com/Tencent/WeKnora.git
cd WeKnora
cp .env.example .env   # 按需编辑 .env，详见文件内注释
docker compose up -d   # 启动核心服务
```

启动成功后访问 **http://localhost** 即可使用。

如需部署到远程 Linux 服务器、从源码构建完整镜像、启用 `full` 服务集或规划升级回滚，请使用[生产环境 Docker Compose 部署指南](./docs/production-deployment.md)。

### 默认平台管理员

启动时若不存在配置的默认管理员账号，系统会自动创建平台管理员：

| 项目 | 默认值 |
|------|--------|
| 登录用户名 | `admin` |
| 登录密码 | `Admin@123456` |
| 账号邮箱 | `admin@weknora.local` |
| 角色 | 平台管理员（可管理和切换全部租户） |

请在首次登录后立即修改默认密码。生产部署应在 `.env` 中设置
`DEFAULT_ADMIN_EMAIL`、`DEFAULT_ADMIN_USERNAME` 和 `DEFAULT_ADMIN_PASSWORD`；已存在同名默认管理员时不会重置其密码。公开注册入口已关闭。

系统角色分为平台管理员、租户管理员和普通成员。平台管理员管理全部租户；租户管理员仅管理所属租户的配置、知识库与图谱；普通成员仅使用被授权的知识和问答能力。

> 如需使用本地 Ollama 模型，请先运行 `ollama serve > /dev/null 2>&1 &`

### Lite 与标准版

| 维度 | Lite | 标准版 |
|------|------|--------|
| 使用场景 | 个人或小团队本机使用 | 多团队协作与企业部署 |
| 账号与协作 | 单租户、无需注册，不提供共享空间 | 多租户、账号与组织管理、共享空间 |
| 依赖 | 单应用，本地存储 | Docker Compose 多服务架构 |
| 文档解析 | 内置 Simple，可接入 Cloud 等外部能力 | 完整文档处理与解析引擎配置 |

两种版本均可本地保存数据；是否对公网开放取决于实际监听地址、网关和网络策略。

### 🔧 可选服务（Docker Compose Profile）

按需添加 `--profile` 启动额外组件，多个 profile 可叠加使用：

| Profile | 说明 | 启动命令 |
|---------|------|----------|
| _(默认)_ | 核心服务 | `docker compose up -d` |
| `full` | 大部分可选功能（不含 Weaviate） | `docker compose --profile full up -d` |
| `neo4j` | 知识图谱 (Neo4j) | `docker compose --profile neo4j up -d` |
| `minio` | 对象存储 (MinIO) | `docker compose --profile minio up -d` |
| `jaeger` | OpenTelemetry 链路查看 (Jaeger) | `docker compose --profile jaeger up -d` |
| `qdrant` | 向量数据库 (Qdrant) | `docker compose --profile qdrant up -d` |
| `weaviate` | 向量数据库 (Weaviate) | `docker compose --profile weaviate up -d` |
| `dex` | 本地 OIDC 联调 (Dex) | `docker compose --profile dex up -d` |
| `langfuse` | 链路追踪 (Langfuse) | `docker compose --profile langfuse up -d` |

组合示例：`docker compose --profile neo4j --profile minio up -d`

停止服务：`docker compose down`

#### 本地 Langfuse 接入

1. 执行 `docker compose --profile langfuse up -d` 启动 Langfuse，首次启动需等待 ClickHouse 完成迁移。
2. 打开 `http://localhost:3000`，注册 Langfuse 管理员，并创建组织和项目。
3. 在项目的 `Settings → API Keys` 中生成 Public Key 和 Secret Key。
4. 将以下配置写入 `.env`，然后执行 `docker compose up -d app` 使配置生效：

```env
LANGFUSE_HOST=http://langfuse-web:3000
LANGFUSE_PUBLIC_KEY=pk-lf-xxxxxxxx
LANGFUSE_SECRET_KEY=sk-lf-xxxxxxxx
```

> 浏览器通过 `http://localhost:3000` 访问 Langfuse；Docker 中的 WeKnora 应用通过服务名 `http://langfuse-web:3000` 访问。如果后端使用宿主机上的 `go run` 启动，则将 `LANGFUSE_HOST` 改为 `http://localhost:3000`。请勿将真实密码或 API Key 提交到版本库。

发起一次知识问答或 Agent 对话后，可在 Langfuse 的 **Tracing** 页面查看模型调用、ReAct 轮次、工具调用、Token 使用量和异步任务链路。完整配置及故障排查参见 [Langfuse 集成说明](./docs/Langfuse集成.md)。

#### 其他可选能力接入索引

以下能力还需要启动额外服务或提供外部凭证。修改 `.env` 后需重启 `app`；使用外部服务时，还应确认容器网络、TLS 和防火墙允许访问对应地址。

| 能力 | 启动或接入方式 | `.env` 关键配置 | 验证方式 |
|------|----------------|-----------------|----------|
| 知识图谱 | 启动 `neo4j` profile | `ENABLE_GRAPH_RAG=true`、`NEO4J_ENABLE=true`、`NEO4J_*` | 打开 `http://localhost:7474`，并按[知识图谱指南](./docs/开启知识图谱功能.md)验证节点 |
| MinIO 对象存储 | 启动 `minio` profile | `STORAGE_TYPE=minio`、`MINIO_ENDPOINT`、`MINIO_ACCESS_KEY_ID`、`MINIO_SECRET_ACCESS_KEY`、`MINIO_BUCKET_NAME` | 打开 `http://localhost:9001`，上传文档后确认对象写入 |
| Qdrant 向量库 | 启动 `qdrant` profile | 在 `RETRIEVE_DRIVER` 中加入 `qdrant`，设置 `VECTOR_RETRIEVE_DRIVER=qdrant`；认证场景再填 `QDRANT_API_KEY` | 后端日志出现 `Register qdrant retrieve engine success` |
| Weaviate 向量库 | 启动 `weaviate` profile | 在 `RETRIEVE_DRIVER` 中加入 `weaviate`，设置 `VECTOR_RETRIEVE_DRIVER=weaviate`；认证场景再填 `WEAVIATE_*` | 后端日志出现 `Register weaviate retrieve engine success` |
| Jaeger 链路查看 | 启动 `jaeger` profile | Compose 部署已预设 `OTEL_EXPORTER_OTLP_ENDPOINT=jaeger:4317` | 打开 `http://localhost:16686` 查询 `WeKnora` 服务 |
| OIDC 单点登录 | 接入现有 OIDC Provider，或启动 `dex` profile 本地联调 | `OIDC_AUTH_ENABLE=true`、`OIDC_AUTH_CLIENT_ID`、`OIDC_AUTH_CLIENT_SECRET`、Discovery/Issuer 地址 | 登录页出现 OIDC 按钮；配置与回调要求见 [OIDC 认证流程](./docs/OIDC认证调用流程.md) |
| 外部对象存储 | 无需 Compose profile，在云平台创建 Bucket 和访问凭证 | `STORAGE_TYPE=cos` / `tos` / `s3`，并填写对应的 `COS_*` / `TOS_*` / `S3_*` | 上传文档后确认目标 Bucket 中出现对象 |
| 在线或内网模型 | 在「设置 → 模型设置」配置服务地址、模型名和 API Key；`MODEL_PROFILE` 与 `ONLINE_*` / `OFFLINE_*` 用于首次启动初始化 | 模型名必须与服务端公开的 model ID 一致，内网端点按需加入 `SSRF_WHITELIST` | 在模型设置中执行连接测试并切换在线/离线档位 |
| 网页搜索 | 在「设置 → 网页搜索」添加 Provider；Tavily、Google、Bing 等凭证保存在租户配置中 | 不依赖全局 `TAVILY_API_KEY`；按界面填写所选 Provider 的 API Key/Engine ID | 在设置页测试连接，再在 Agent 中启用网页搜索 |

> `full` profile 不包含 Weaviate；如需 Weaviate，请显式添加 `--profile weaviate`。标准 Docker Compose 只会把 `docker-compose.yml` 中 `app.environment` 列出的变量传入后端；使用 OIDC、TOS、S3 或 `ONLINE_*` / `OFFLINE_*` 等变量时，应确认它们已注入 `app` 容器。生产环境请使用 Secret 管理，不要提交真实凭证。

### 🌐 服务地址

| 服务 | 地址 |
|------|------|
| Web UI | `http://localhost` |
| 后端 API | `http://localhost:8080` |
| 链路追踪 (Langfuse) | `http://localhost:3000` |
| Neo4j Browser | `http://localhost:7474` |
| MinIO Console | `http://localhost:9001` |
| Jaeger UI | `http://localhost:16686` |

## 知识图谱

启用 `neo4j` profile 并在知识库设置中开启实体/关系抽取后，WeKnora 会从文档块提取实体和关系，并在检索时使用图谱补充上下文。抽取结果受实体数、关系数、置信度和 Schema 限制；同一文本块重新抽取时会替换旧的规范化图谱记录，避免保留过期关系。

配置与验证步骤参见[开启知识图谱功能](./docs/开启知识图谱功能.md)。

## MCP 工具

MCP（Model Context Protocol）用于向 Agent 接入外部工具和数据源。在「设置 → MCP 服务」中可添加 SSE、HTTP Streamable 或 Stdio 服务，配置认证、超时和重试策略，并执行连接测试、查看工具清单及启停服务。生产环境应使用最小权限凭证并定期轮换。

如果需要运行仓库自带的独立 MCP Server，请参考 [MCP Server 配置](./mcp-server/MCP_CONFIG.md)。

## 🔌 使用微信对话开放平台

WeKnora 作为[微信对话开放平台](https://chatbot.weixin.qq.com)的核心技术框架，提供更简便的使用方式：

- **零代码部署**：只需上传知识，即可在微信生态中快速部署智能问答服务，实现"即问即答"的体验
- **高效问题管理**：支持高频问题的独立分类管理，提供丰富的数据工具，确保回答精准可靠且易于维护
- **微信生态覆盖**：通过微信对话开放平台，WeKnora 的智能问答能力可无缝集成到公众号、小程序等微信场景中，提升用户交互体验


## 📘 文档

README 保留安装、配置和日常开发所需信息；以下专题内容篇幅较大或面向特定角色，保留为独立文档：

| 分类 | 文档 |
|------|------|
| 使用与运维 | [常见问题](./docs/QA.md) · [Langfuse 集成](./docs/Langfuse集成.md) · [共享空间](./docs/共享空间说明.md) |
| API 与嵌入 | [API 文档](./docs/api/README.md) · [外挂知识库](./docs/外挂知识库指南.md) · [OIDC 认证](./docs/OIDC认证调用流程.md) |
| 扩展开发 | [数据源导入](./docs/数据源导入开发文档.md) · [IM 集成](./docs/IM集成开发文档.md) · [向量数据库](./docs/使用其他向量数据库.md) · [网络搜索引擎](./docs/添加新的网络搜索引擎.md) |
| Agent 与平台配置 | [Agent Skills](./docs/agent-skills.md) |
| 离线与私有化 | [严格离线运行](./docs/airgap-operations.md) |
| 维护者文档 | [内置模型](./docs/maintainers/BUILTIN_MODELS.md) · [内置 MCP 服务](./docs/maintainers/BUILTIN_MCP_SERVICES.md) · [ASR/TTS 能力契约](./docs/voice-capabilities.md) |

## 🧭 开发指南

### ⚡ 快速开发模式（推荐）

如果你需要频繁修改代码，**不需要每次重新构建 Docker 镜像**！使用快速开发模式：

**macOS / Linux：**

```bash
# 启动基础设施
make dev-start

# 启动后端（新终端）
make dev-app

# 启动前端（新终端）
make dev-frontend
```

**Windows：** PowerShell 默认没有 `make`，且 Makefile 依赖 bash。请优先使用 **Git Bash**；只有在 Docker Desktop 已开启当前发行版 WSL Integration 时才直接使用 WSL：

```bash
# 启动基础设施
./scripts/dev.sh start

# 启动后端（新终端）
./scripts/dev.sh app

# 启动前端（新终端）
./scripts/dev.sh frontend
```

在 PowerShell 中不建议直接调用 `bash`，因为它可能被解析为 WSL；如果该 WSL 发行版没有 Docker Desktop 集成，依赖服务会启动失败。

如果希望一条命令启动完整开发环境，可使用一键脚本。

在 Windows PowerShell 中运行：

```powershell
.\scripts\quick-dev.ps1
```

该入口会自动调用 Git Bash，不依赖 PowerShell 中的 `bash` 是否指向 WSL。在 Windows/WSL 中，脚本会自动在 Docker 内运行 Linux Go + Air，规避 DuckDB/SQLite 的 Windows CGO 链接问题；代码仍挂载自本地目录，后端修改会自动重启。首次启动会构建一次开发镜像，后续会复用。

在 Git Bash 中也可以直接运行：

```bash
bash ./scripts/quick-dev.sh
```

如果从 WSL 直接运行，需要先在 Docker Desktop 的 Settings → Resources → WSL Integration 中开启当前发行版；否则 WSL 可能找不到可用的 Docker Compose。

脚本会先停止上一次由它启动的后端和前端进程，再启动新的进程；已经运行的 Docker 依赖服务会复用，不会重复重启。日志和 PID 文件分别位于：

```text
logs/backend.log
logs/frontend.log
logs/backend.pid
logs/frontend.pid
```

停止本地后端、前端和 Docker 依赖：

```bash
bash ./scripts/quick-dev.sh stop
```

**开发优势：**

- ✅ 前端修改自动热重载（无需重启）
- ✅ 后端修改快速重启（5-10秒，支持 Air 热重载）
- ✅ 一键模式首次构建后复用开发镜像
- ✅ 支持 IDE 断点调试

**详细文档：** [开发环境快速入门](./docs/开发指南.md)

### 📁 项目目录结构

```
WeKnora/
├── client/      # go客户端
├── cmd/         # 应用入口
├── config/      # 配置文件
├── docker/      # docker 镜像文件
├── docreader/   # 文档解析项目
├── docs/        # 项目文档
├── frontend/    # 前端项目
├── internal/    # 核心业务逻辑
├── mcp-server/  # MCP服务器
├── migrations/  # 数据库迁移脚本
└── scripts/     # 启动与工具脚本
```
