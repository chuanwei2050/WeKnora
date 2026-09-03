# 生产环境 Docker Compose 部署指南

本文面向将 KnowledgeMesh 标准版部署到远程 Linux 服务器的场景。快速本地体验请直接参考项目根目录的 [README](../README.md#快速开始)；Kubernetes 和严格离线环境分别参考 [Helm Chart](../helm/README.md) 与[严格离线运行](./airgap-operations.md)。

## 1. 部署结果

本文使用 `/opt/weknora` 作为安装目录，从当前源码构建 KnowledgeMesh 镜像，并由 Docker Compose 管理服务和持久化卷。

`COMPOSE_PROFILES=full` 会启动：

- KnowledgeMesh App、Web UI、DocReader
- PostgreSQL、Redis、Elasticsearch、Milvus
- MinIO、Qdrant、Neo4j、Jaeger、Dex
- Langfuse Web/Worker、ClickHouse 和 Langfuse 专用 MinIO
- Sandbox 镜像准备容器

Sandbox 容器执行 `true` 后退出是正常行为。App 执行 Skill 时会基于该镜像创建临时容器。

> `full` 不包含 Weaviate。确需 Weaviate 时，将 `COMPOSE_PROFILES` 设置为 `full,weaviate`。

## 2. 上线前检查

建议至少准备 4 核 CPU、16 GB 内存和 40 GB 可用磁盘。完整服务集和源码构建会占用更多磁盘，实际容量还要计入文档、数据库、模型和日志增长。

```bash
docker --version
docker compose version
df -h /
free -h
```

检查常用端口是否被占用：

```bash
sudo ss -lntup
```

完整服务集默认使用以下宿主机端口：

| 服务 | 默认端口 | 可配置变量 |
|------|----------|------------|
| Web UI | `80` | `FRONTEND_PORT` |
| App API | `8080` | `APP_PORT` |
| DocReader | `50051` | `DOCREADER_PORT` |
| PostgreSQL | `127.0.0.1:5432` | `DB_PORT` |
| Redis | `127.0.0.1:6379` | `REDIS_PORT` |
| Elasticsearch | `127.0.0.1:9200` | `ELASTICSEARCH_PORT` |
| MinIO API / Console | `9000` / `9001` | `MINIO_PORT` / `MINIO_CONSOLE_PORT` |
| Qdrant REST / gRPC | `6333` / `6334` | `QDRANT_REST_PORT` / `QDRANT_PORT` |
| Langfuse | `3000` | `LANGFUSE_WEB_PORT` |
| Langfuse MinIO | `9100` / `9101` | `LANGFUSE_MINIO_S3_PORT` / `LANGFUSE_MINIO_CONSOLE_PORT` |
| Neo4j Browser / Bolt | `7474` / `7687` | 当前 Compose 未提供变量 |
| Jaeger UI | `16686` | 当前 Compose 未提供变量 |
| Milvus | `19530` / `9091` | 当前 Compose 未提供变量 |
| Dex | `5556` | 当前 Compose 未提供变量 |

数据库、Redis 和 Elasticsearch 默认只绑定到 `127.0.0.1`。其余端口可能监听所有网卡；生产环境应通过云安全组或主机防火墙只开放 Web/网关所需端口。

## 3. 安装程序

```bash
sudo mkdir -p /opt/weknora
sudo chown "$(id -u):$(id -g)" /opt/weknora
cd /opt/weknora
cp .env.example .env
chmod 600 .env
```

将交付包解压到 `/opt/weknora`，再执行后续步骤。不要把本地 `.git`、`node_modules`、构建缓存或包含无关文件的整个工作目录上传到服务器。

## 4. 配置生产环境

编辑 `/opt/weknora/.env`：

```bash
cd /opt/weknora
nano .env
```

至少检查以下配置：

```env
GIN_MODE=release
COMPOSE_PROFILES=full
FRONTEND_PORT=8089
APP_PORT=18080
DOCREADER_PORT=15051
DB_PORT=15432
REDIS_PORT=16379

DEFAULT_ADMIN_ENABLED=true
DEFAULT_ADMIN_USERNAME=admin
DEFAULT_ADMIN_EMAIL=admin@example.com
DEFAULT_ADMIN_PASSWORD=<随机强密码>

DB_PASSWORD=<随机强密码>
REDIS_PASSWORD=<随机强密码>
ELASTICSEARCH_PASSWORD=<随机强密码>
JWT_SECRET=<随机值>
TENANT_AES_KEY=<随机值>
SYSTEM_AES_KEY=<随机值>
WEKNORA_DEX_CLIENT_SECRET=<随机值>
```

可使用 OpenSSL 生成随机值：

```bash
openssl rand -hex 32
openssl rand -base64 32
```

如果启用自建 Langfuse，还应设置 `LANGFUSE_SALT`、`LANGFUSE_ENCRYPTION_KEY`、`LANGFUSE_NEXTAUTH_SECRET`、ClickHouse 和 Langfuse MinIO 密码。不要提交真实 `.env`，也不要在工单、聊天或构建日志中输出其内容。

服务容器启动并不代表 KnowledgeMesh 已选择该后端。以下变量决定实际使用的存储和检索引擎：

```env
STORAGE_TYPE=minio
RETRIEVE_DRIVER=elasticsearch_v8,milvus
KEYWORD_RETRIEVE_DRIVER=elasticsearch_v8
VECTOR_RETRIEVE_DRIVER=milvus
ENABLE_GRAPH_RAG=true
NEO4J_ENABLE=true
```

修改驱动后，应同时确认对应服务已启动、凭证正确，并在管理界面执行连接测试。

## 5. 校验并构建镜像

先让 Compose 完整解析配置。该命令失败时不要继续启动：

```bash
docker compose config --services
docker compose config --quiet
```

从当前源码构建 App、UI、DocReader 和 Sandbox 镜像：

```bash
docker compose build --pull app frontend docreader sandbox
```

依赖镜像较多，首次构建和拉取可能需要较长时间。构建前后可用以下命令观察空间：

```bash
docker system df
df -h /
```

不要在共享服务器上直接运行 `docker system prune -a`；它可能删除其他项目仍需使用的缓存或镜像。

## 6. 启动与验证

```bash
docker compose up -d
docker compose ps
```

首次启动时，PostgreSQL、Elasticsearch、Milvus、ClickHouse 和 Langfuse 可能需要数分钟完成初始化。持续查看状态：

```bash
docker compose ps
docker compose logs --tail=100 app
docker compose logs --tail=100 langfuse-web
```

验证 App 和 Web UI：

```bash
curl --fail http://127.0.0.1:18080/health
curl --head http://127.0.0.1:8089
```

如果修改了 `APP_PORT` 或 `FRONTEND_PORT`，请同步替换命令中的端口。远程访问地址为：

```text
http://<服务器地址>:<FRONTEND_PORT>
```

验收至少应满足：

1. `docker compose ps` 中常驻服务为 `running` 或 `healthy`。
2. App `/health` 返回成功。
3. Web UI 可以登录，且已修改默认管理员密码。
4. 可以上传一份测试文档并完成解析、检索和问答。
5. MinIO 中出现对象，Elasticsearch/Milvus 日志没有连接错误。
6. 发起一次问答后，可在 Langfuse Tracing 页面看到调用链。

## 7. 初始化 Langfuse

1. 打开 `http://<服务器地址>:3000`。
2. 创建管理员、组织和项目。
3. 在项目的 `Settings → API Keys` 中生成 Public Key 和 Secret Key。
4. 写入 `.env`：

```env
LANGFUSE_HOST=http://langfuse-web:3000
LANGFUSE_PUBLIC_KEY=pk-lf-xxxxxxxx
LANGFUSE_SECRET_KEY=sk-lf-xxxxxxxx
LANGFUSE_DEBUG=false
```

5. 重建 App 容器并发起一次问答：

```bash
docker compose up -d --force-recreate app
docker compose logs --tail=100 app
```

生产环境只应在排障期间临时使用 `LANGFUSE_DEBUG=true`。

## 8. 日常运维

查看状态和日志：

```bash
cd /opt/weknora
docker compose ps
docker compose logs -f --tail=200 app
```

停止或重新启动：

```bash
docker compose stop
docker compose start
```

`docker compose down` 会删除容器和网络，但默认保留命名卷。不要使用 `docker compose down -v`，除非明确要删除数据库、对象和索引数据。

## 9. 升级与回滚

升级前记录当前版本并备份数据库：

```bash
cd /opt/weknora
git rev-parse HEAD
mkdir -p backups
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"' > "backups/postgres-$(date +%F-%H%M%S).sql"
```

然后更新并重新构建：

```bash
git fetch --all --tags
git checkout <目标版本或提交>
docker compose config --quiet
docker compose build --pull app frontend docreader sandbox
docker compose up -d
```

升级后重新执行第 6 节的健康检查和端到端验证。

回滚应用代码时，切回升级前记录的提交并重新构建。数据库迁移不一定支持直接降级；如果新版本已执行不兼容迁移，应先停止写入，再依据升级前备份恢复数据库。生产环境应在维护窗口内演练升级和恢复，不要把“容器能够启动”视为回滚成功。

## 10. 常见问题

### Compose 提示必填变量为空

根据错误信息补齐 `.env`。完整服务集至少会校验 Elasticsearch 和 Dex 的密码变量。补齐后再次运行：

```bash
docker compose config --quiet
```

### 容器名称或端口冲突

先用 `docker ps -a` 和 `ss -lntup` 确认冲突来源。端口可通过 `.env` 中对应变量调整；当前 Compose 固定的端口需要修改 Compose override，或停止明确不再需要的冲突服务。不要盲目删除未知容器。

### Langfuse 页面可访问但没有 Trace

确认 App 容器内使用 `http://langfuse-web:3000`，Public/Secret Key 来自当前项目，并在修改 `.env` 后重建了 App 容器。可临时启用 `LANGFUSE_DEBUG=true` 查看上报错误，详细说明见 [Langfuse 集成](./Langfuse集成.md)。

### 服务已启动但 MinIO/Qdrant 未被使用

Profile 只决定容器是否启动，`STORAGE_TYPE`、`RETRIEVE_DRIVER`、`KEYWORD_RETRIEVE_DRIVER` 和 `VECTOR_RETRIEVE_DRIVER` 才决定 App 实际使用哪个后端。
