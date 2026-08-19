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

  [项目介绍](#-项目介绍) • [架构设计](#-架构设计) • [核心特性](#-核心特性) • [快速开始](#-快速开始) • [文档](#-文档) • [开发指南](#-开发指南)

# 💡 WeKnora — 让文档活起来：RAG、Agent 推理与自动 Wiki 一体化的知识框架

## 📌 项目介绍

**[WeKnora（维娜拉）](https://weknora.weixin.qq.com)** 是一款开源的、基于大语言模型（LLM）的知识管理框架，专为企业级文档理解、语义检索与智能推理场景打造。

框架围绕三大核心能力构建：**RAG 快速问答**适合日常知识查询，**ReAct Agent 智能推理**自主编排知识检索、MCP 工具与网络搜索完成复杂多步任务，全新的 **Wiki 模式**则让 Agent 从原始文档中自治生成相互链接的 Markdown 知识库与可视化知识图谱。结合多源数据接入（飞书 / Notion / 语雀，更多持续接入中）、二十余家主流模型厂商集成、Langfuse 全链路可观测性，以及完全可私有化部署的模块化架构，WeKnora 帮助团队把分散文档沉淀为可查询、可推理、可持续演进的专属知识资产。

框架支持从飞书、Notion 及语雀等外部平台自动同步知识（更多数据源持续接入中），覆盖 PDF、Word、图片、Excel 等十余种文档格式，并可通过企业微信、飞书、Slack、Telegram 等 IM 频道直接提供问答服务。模型层面兼容 OpenAI、DeepSeek、Qwen（阿里云）、智谱、混元、Gemini、MiniMax、NVIDIA、Ollama 等主流厂商。全流程模块化设计，大模型、向量数据库、存储等组件均可灵活替换，支持本地与私有云部署，数据完全自主可控。WeKnora 还无缝集成了 **Langfuse**，为 Agent 运行、Token 使用及任务流水线提供了全面的可观测性追踪。


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
| 界面 | Web UI / RESTful API / Chrome Extension / 微信小程序 |
| 可观测性 | 集成 Langfuse 以追踪 ReAct 循环、Token 消耗、工具调用和任务流水线 |
| 任务管理 | MQ 异步任务，版本升级自动数据库迁移 |
| 模型管理 | 集中配置，知识库级别模型选择，多租户共享内置模型，WeKnora Cloud 托管模型与文档解析 |

## 🧩 Chrome 插件

[**WeKnora Chrome 插件**](https://chromewebstore.google.com/detail/jpemjbopikggjlmikmclgbmkhhopjdgd)支持在浏览器中直接将网页内容采集到 WeKnora 知识库。选中文本、图片或整个页面，一键保存为知识条目，无需复制粘贴或手动上传文件。


## 📱 微信小程序

[**WeKnora 微信小程序**](./miniprogram/README.md) 提供轻量移动端客户端，支持配置 WeKnora API、选择知识库、导入 URL，并在微信内向知识库提问。


## 🦞 ClawHub Skill

[**WeKnora ClawHub Skill**](https://clawhub.ai/lyingbug/weknora) 是 WeKnora 发布在 ClawHub 平台上的技能。安装后，可通过 WeKnora REST API 上传文档（文件 / URL / Markdown）、执行混合检索（向量 + 关键词）以及管理知识条目。

- **文档导入** — 通过 Agent 上传文件、导入网页或写入 Markdown 知识
- **混合检索** — 在单个或多个知识库中进行向量 + 关键词混合搜索
- **知识管理** — 以编程方式浏览、编辑和删除知识条目

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

## 文档知识图谱

WeKnora 支持将文档转化为知识图谱，展示文档中不同段落之间的关联关系。开启知识图谱功能后，系统会分析并构建文档内部的语义关联网络，不仅帮助用户理解文档内容，还为索引和检索提供结构化支撑，提升检索结果的相关性和广度。

具体配置请参考 [知识图谱配置说明](./docs/KnowledgeGraph.md) 进行相关配置。

## 配套MCP服务器

请参考 [MCP配置说明](./mcp-server/MCP_CONFIG.md) 进行相关配置。

## 🔌 使用微信对话开放平台

WeKnora 作为[微信对话开放平台](https://chatbot.weixin.qq.com)的核心技术框架，提供更简便的使用方式：

- **零代码部署**：只需上传知识，即可在微信生态中快速部署智能问答服务，实现"即问即答"的体验
- **高效问题管理**：支持高频问题的独立分类管理，提供丰富的数据工具，确保回答精准可靠且易于维护
- **微信生态覆盖**：通过微信对话开放平台，WeKnora 的智能问答能力可无缝集成到公众号、小程序等微信场景中，提升用户交互体验


## 📘 文档

常见问题排查：[常见问题排查](./docs/QA.md)

详细接口说明请参考：[API 文档](./docs/api/README.md)

产品规划与计划：[路线图 (Roadmap)](./docs/ROADMAP.md)

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
tmp/backend.pid
tmp/frontend.pid
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
