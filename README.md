# KnowledgeMesh

KnowledgeMesh 将 **RAG 快速问答、ReAct Agent 推理和自动 Wiki** 统一在一套知识工作流中。导入 PDF、Word、图片、网页或外部知识库后，系统可完成解析、索引、检索和回答，并通过 MCP 工具与网络搜索处理多步任务。

模型、向量数据库、对象存储和部署方式均可替换，支持本地及私有云部署。

## 适用场景

- 基于内部资料构建知识问答系统
- 统一检索和分析分散的业务文档
- 从原始资料生成并维护结构化 Wiki

## 功能

- **智能对话**：RAG 问答、ReAct 推理、多轮上下文、推荐问题和在线 Prompt 调整
- **知识管理**：FAQ、文档和 Wiki 知识库，支持多种文件格式、URL、文件夹及外部知识库导入
- **检索与评估**：BM25、Dense、GraphRAG、父子分块、多维索引和端到端评估
- **集成扩展**：支持主流模型、向量数据库、对象存储、IM、MCP 和网络搜索服务
- **部署运维**：Web UI、RESTful API、Docker、Kubernetes、多租户和链路追踪

## 快速开始

需要安装 [Docker](https://www.docker.com/) 和 [Docker Compose](https://docs.docker.com/compose/)。

```bash
cp .env.example .env
docker compose up -d
```

启动后访问 `http://localhost`，使用默认管理员账号 `admin` / `Admin@123456` 登录，并立即修改密码。生产环境还应在 `.env` 中设置 `DEFAULT_ADMIN_EMAIL`、`DEFAULT_ADMIN_USERNAME` 和 `DEFAULT_ADMIN_PASSWORD`。

### 首次使用

1. 在「设置 → 模型设置」中配置模型并测试连接
2. 创建知识库并上传文档
3. 等待索引完成后开始提问

运行 `docker compose ps` 检查服务状态。能够打开 Web UI、登录并通过模型连接测试，即表示基础环境可用。

> 首次使用必须配置可用模型。知识图谱、外部存储、单点登录和链路追踪等能力还需要启用对应 profile 或提供外部凭证。

远程部署、镜像构建和升级回滚参见[生产环境部署指南](./docs/production-deployment.md)。

### 可选服务

通过 Docker Compose profile 启用额外组件：

| Profile | 用途 |
|---------|------|
| `full` | 大部分可选功能（不含 Weaviate） |
| `neo4j` | 知识图谱 |
| `minio` | 对象存储 |
| `qdrant` / `weaviate` | 向量数据库 |
| `jaeger` / `langfuse` | 链路追踪 |
| `dex` | 本地 OIDC 联调 |

```bash
docker compose --profile neo4j --profile minio up -d
```

各服务的配置和验证方式参见[文档索引](#文档)。停止服务运行 `docker compose down`。

### 生产环境检查

- 修改默认管理员账号和密码，不向公网暴露默认凭证
- 使用 Secret 管理模型、数据库和对象存储凭证，不将真实密钥提交到仓库
- 按实际访问范围配置监听地址、反向代理、TLS 和防火墙
- 部署前确认数据备份、数据库迁移、升级和回滚方案

### Lite 与标准版

| 维度 | Lite | 标准版 |
|------|------|--------|
| 场景 | 个人或小团队本机使用 | 多团队协作与企业部署 |
| 协作 | 单租户，无需注册 | 多租户、组织管理和共享空间 |
| 依赖 | 单应用、本地存储 | Docker Compose 多服务架构 |
| 文档解析 | 内置 Simple，可接外部能力 | 完整解析引擎配置 |

## 集成

- [微信小程序](./miniprogram/README.md)：在微信内导入 URL 和进行知识问答
- [聊天 Widget](./docs/外挂知识库指南.md)：将知识问答嵌入现有业务系统
- [MCP Server](./mcp-server/MCP_CONFIG.md)：向 Agent 接入外部工具和数据源

## 开发

macOS / Linux：

```bash
make dev-start
make dev-app       # 新终端
make dev-frontend  # 新终端
```

Windows 推荐在 PowerShell 中运行：

```powershell
.\scripts\quick-dev.ps1
```

也可在 Git Bash 中运行

```
bash ./scripts/quick-dev.sh
```

停止环境运行

```
bash ./scripts/quick-dev.sh stop
```

完整说明参见[开发环境快速入门](./docs/开发指南.md)。

提交代码前运行相关检查：

```bash
make test
make lint
cd frontend
npm run test:unit
npm run type-check
```

## 文档

| 分类 | 文档 |
|------|------|
| 使用与运维 | [常见问题](./docs/QA.md) · [生产部署](./docs/production-deployment.md) · [Helm 部署](./helm/README.md) · [严格离线运行](./docs/airgap-operations.md) · [Langfuse](./docs/Langfuse集成.md) |
| 平台能力 | [知识图谱](./docs/开启知识图谱功能.md) · [共享空间](./docs/共享空间说明.md) · [OIDC](./docs/OIDC认证调用流程.md) |
| API 与集成 | [API](./docs/api/README.md) · [外挂知识库](./docs/外挂知识库指南.md) · [数据源导入](./docs/数据源导入开发文档.md) · [IM 集成](./docs/IM集成开发文档.md) |
| 扩展开发 | [Agent Skills](./docs/agent-skills.md) · [向量数据库](./docs/使用其他向量数据库.md) · [网络搜索引擎](./docs/添加新的网络搜索引擎.md) |
| 维护者 | [内置模型](./docs/maintainers/BUILTIN_MODELS.md) · [内置 MCP 服务](./docs/maintainers/BUILTIN_MCP_SERVICES.md) · [语音能力](./docs/voice-capabilities.md) |
