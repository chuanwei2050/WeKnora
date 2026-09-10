# WeKnora Structured Query

独立的 Excel/CSV 入库、PostgreSQL/MySQL 零复制接入、Profile、Text-to-SQL 与安全 SQL 执行服务。

生产部署只运行同一个应用镜像的 `api` 和 `worker` 角色。PostgreSQL、Milvus、Redis、LLM 与 embedding 服务均通过配置注入，镜像不内置这些基础设施。

```powershell
Copy-Item .env.example .env
docker compose build
docker compose up -d
```

Compose 会先运行唯一的 `migrate` 服务执行 Alembic，再启动 API 与 Worker，避免多个实例并发建表。脱离
Compose 部署时也应先运行同一镜像的 `migrate` 角色，再扩容 API/Worker；不支持把无版本标记的旧库自动
假定为最新结构。

API 默认只在宿主机回环地址监听 `8090`，OpenAPI 文档位于 `/docs`。启动前必须将 `.env.example` 中的 `CHANGE_ME_` API key 替换为随机密钥；需要对外监听时再显式设置 `STRUCTURED_QUERY_BIND_ADDRESS` 并配合网络访问控制。

## 模型配置

服务不在环境变量中保存聊天模型或 embedding 模型的名称、地址和密钥。它按 tenant 调用 WeKnora 内部接口
`/api/internal/v1/structured-query/model-config` 获取当前默认模型配置，并按
`STRUCTURED_QUERY_MODEL_CONFIG_TTL_SECONDS` 做短时缓存。WeKnora 与本服务之间使用
`STRUCTURED_QUERY_WEKNORA_SERVICE_KEY` 鉴权；模型配置在管理端发生变化后会在缓存到期时自动生效。WeKnora 服务调用使用 `X-API-Key` 加当前实际 `X-Tenant-ID`，对应 API key 在 `STRUCTURED_QUERY_API_KEYS` 中映射为 `service`；直接调用方也可继续使用绑定单一 tenant 的 `key:tenant_id`。

外部 PostgreSQL/MySQL 数据源默认只保存 Schema 与无值统计，不持久化、向量化或发送字段原值。只有注册请求对具体字段显式设置 `persist_values: true` 时才建立值 Profile；未持久化字段在查询时使用限时、只读且只返回存在性的 Value Probe。

## 一次性重建 Profile 索引

首次上线、历史数据迁移或切换 embedding 模型后，在 API/Worker 使用的同一运行环境中执行：

```powershell
python scripts/rebuild_profile_index.py
```

只重建一个当前有效版本时可传入版本 UUID：

```powershell
python scripts/rebuild_profile_index.py --version <version-uuid>
```

## 回填 WeKnora 历史表格

在 WeKnora 项目根目录运行一次性命令，将尚无 `structured_dataset_id` 的历史
CSV/XLS/XLSX 提交到本服务。命令复用 WeKnora 当前存储配置和
`structured_query` 服务配置，不直接读取存储目录，也不覆盖已经提交的数据集。

```powershell
# 先查看候选文件
./scripts/backfill-structured-query.ps1 -Tenant 1 -DryRun

# 提交一个租户的全部历史表格，可重复执行并断点续跑
./scripts/backfill-structured-query.ps1 -Tenant 1 -Concurrency 4

# 仅重试一个知识文档
./scripts/backfill-structured-query.ps1 -Tenant 1 -Knowledge <knowledge-id>
```

线上全租户执行必须显式传入 `-AllTenants`。每个成功提交的文档会立即写回
dataset/version/job 标识；脚本若有任一失败会以非零状态退出，并逐项打印失败原因。

`scripts/deploy-development.sh` 使用根目录 Compose 中的正式镜像角色完成线上部署：先执行
`structured-query-migrate`，再启动并健康检查 `structured-query-api` 与
`structured-query-worker`，主应用就绪后运行镜像内的
`WeKnora-backfill-structured-query --all-tenants`。部署会等待本轮入库任务结束并在出现失败任务时回滚，
不会在服务器工作区生成或修改源码。

脚本以 PostgreSQL 中已持久化的有效 Profile 为事实来源，重新建立 Milvus 表、字段和列值索引，不复制外部
PostgreSQL/MySQL 的业务表数据。索引集合按 tenant 与 embedding 配置隔离，因此 embedding 配置切换后必须执行
一次全量重建，随后 API 和 Worker 才会查询到新向量空间中的 Profile。
