## Why

知识库中的 Excel/CSV 和已有业务数据库目前缺少一条可复用、低延迟且可验证的结构化查询链路。需要将文件解析和画像构建前移到上传阶段，并允许对已授权数据库进行零复制画像与只读查询，把查询阶段收敛为轻量检索、单次 Text2SQL、安全执行，从而同时提高准确性、速度和跨项目复用能力。

## What Changes

- 新增独立 Docker 应用镜像，提供文件导入、数据画像、语义检索、Text2SQL、安全执行和运维 API；同一镜像可分别以 API 和 Worker 角色运行。
- 上传 Excel/CSV 后一次性规范化并写入服务 PostgreSQL；查询时不再下载或重新解析原文件。
- 支持注册外部 PostgreSQL 和 MySQL 数据源，以零数据复制方式读取授权 database/schema/table、构建有界 Profile，并在源库直接执行受限只读查询；不得复制外部业务表。
- 使用 SQLAlchemy Inspector 和数据库聚合生成表、字段、类型、主外键、注释、统计与示例值画像，并输出 M-Schema 兼容的模型上下文。
- PostgreSQL、Milvus、Redis、S3-compatible 对象存储、LLM 和 embedding 服务均通过配置注入并复用部署环境现有基础设施；应用镜像不内置这些有状态或模型服务。低基数字段保存完整 distinct 值，高基数字段受限采样。
- 查询时通过检索选择最小充分表集合，默认 1 张，有明确关系需求时扩展到 2～3 张，最多 3 张。
- 将检索得到的 M-Schema、真实候选值、数据源方言和用户问题一次性交给 OpenAI-compatible 主模型生成对应方言 SQL；仅在执行或校验失败时允许一次修复调用。
- 使用按数据源隔离的 PostgreSQL/MySQL Adapter、SQLGlot 方言校验、对应数据库 `EXPLAIN`、只读事务、超时和行数上限形成执行安全边界。
- API 返回生成 SQL、命中 Profile、精确结果片段和分阶段耗时，不生成图表或额外润色答案。
- 提供稳定的 HTTP/OpenAPI 接口，使 WeKnora 和其他项目无需依赖内部实现即可复用。
- **BREAKING**：新服务不保留此前 MindsDB、DB-GPT、Vanna、SQLBot 或项目内旧 SQL 生成链路的兼容适配。

## Capabilities

### New Capabilities

- `structured-file-ingestion`: Excel/CSV 校验、规范化、PostgreSQL 入库、任务状态与幂等重建，以及外部 PostgreSQL/MySQL 数据源的零复制注册和 Profile 刷新。
- `structured-data-profiling`: 基于 SQLAlchemy、PostgreSQL/MySQL 与 Milvus 的表字段画像、M-Schema 兼容上下文和真实列值索引。
- `structured-query-generation`: 最小表集合检索、值链接和单次主模型 Text2SQL 生成。
- `safe-sql-execution`: SQL AST 校验、只读执行、资源限制、单次失败修复与可观测结果。
- `structured-query-service-api`: 可独立部署并供多个项目复用的版本化 HTTP/OpenAPI 契约。

### Modified Capabilities

- `integration-knowledge-api`: 知识库表格上传完成后触发独立结构化服务的异步导入，并在问答时并行调用结构化查询 API。

## Impact

- 新增 `structured-query/` 独立 Python 服务及 Docker Compose 部署文件。
- 主要依赖包括 FastAPI、SQLAlchemy、Alembic、pandas/openpyxl、SQLGlot、PostgreSQL/MySQL 驱动、Milvus SDK 和 Celery 客户端。
- 部署环境需要提供 PostgreSQL DSN、Milvus endpoint、Redis URL、S3-compatible endpoint、OpenAI-compatible LLM endpoint 和 embedding endpoint；开发用基础设施 Compose 仅作为可选工具。
- 需要一个 OpenAI-compatible 对话模型和兼容的 embedding 接口；模型由部署配置注入，不与特定厂商绑定。
- WeKnora 主流程后续仅通过 HTTP 调用服务；结构化服务不依赖 WeKnora 的 ES、Milvus、rerank 或内部数据库模型。
- 外部数据库连接只允许引用服务端预配置或受控创建的 Secret，不允许在查询 API 临时提交 DSN；源库账号必须为只读账号，并通过授权表白名单限制访问。
- 外部数据库不复制业务表，仅持久化连接 Secret 引用、授权范围、Schema Profile、有界统计和按字段敏感策略允许保存的候选值。
- 既有未提交的候选方案评测目录不纳入运行时依赖，也不随本变更修改。
