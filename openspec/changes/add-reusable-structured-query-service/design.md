## Context

WeKnora 需要从知识库 Excel/CSV 中回答精确计数、筛选和聚合问题，但文件解析、表选择、值链接和 SQL 生成不应占用每次查询的关键路径。此前验证的完整 NL2SQL 平台要么模型调用次数多、要么依赖复杂、要么缺少可靠列值链接，因此本变更将成熟组件组合成一个可独立部署的边界服务。

服务面向多个调用方和知识空间。文件在上传时异步导入，查询时访问已物化的服务 PostgreSQL 表；外部 PostgreSQL/MySQL 数据源只注册连接 Secret 引用与授权范围，查询时直接访问源库，不复制业务表。两类数据源均使用持久化且已发布的 Profile。WeKnora 在问题理解前启动结构化服务并与原有问题理解、ES/向量链路并行；结构化服务在同一次 Schema Linking/Text2SQL 调用中自主返回 `sql` 或 `none`，不依赖主链路的问题分类和 rerank。

## Goals / Non-Goals

**Goals:**

- 提供从 Excel/CSV 上传、PostgreSQL 入库、Profile 构建到 SQL 执行的完整独立服务。
- 提供外部 PostgreSQL/MySQL 数据源注册、异步 Profile 刷新和零复制只读查询。
- 查询正常路径只调用一次主模型，并返回 SQL、数据片段、来源和各阶段耗时。
- 默认选择一张表，存在可证明的跨表关系时才扩展到两至三张表。
- Profile 和列值索引由数据自动产生，不以题目或业务词硬编码保证准确率。
- 所有状态持久化，容器重启后无需重新下载或重新解析文件。
- 通过稳定 HTTP/OpenAPI 契约供不同项目复用，并提供租户/知识空间隔离。

**Non-Goals:**

- 不生成图表、分析报告或自然语言润色答案。
- 不提供任意 Python 执行、数据写回或数据库管理能力。
- 不兼容此前实验性的 MindsDB、DB-GPT、Vanna、SQLBot 或项目内旧 SQL 链路。
- 第一阶段不支持 PostgreSQL 和 MySQL 之外的执行数据库。
- 不实现完整 BI 指标建模、人工维护的语义层或多 Agent 自主探索。

## Decisions

### 1. 独立应用镜像，外部基础设施全部配置注入

`structured-query` 只维护和发布一个 Python 3.12 应用镜像，通过启动命令分别作为 `api` 或 `worker` 运行。PostgreSQL、Milvus、Redis、S3-compatible 对象存储、OpenAI-compatible LLM 和 embedding 服务均为外部依赖，通过环境变量或 secret 注入；应用镜像不得内置或隐式启动这些服务。

异步导入使用 Celery 与外部 Redis；不自行实现后台线程、重试队列和任务锁。上传文件暂存到外部 S3-compatible 对象存储供 Worker 读取，成功后按策略清理。外部 PostgreSQL 保存导入数据、任务元数据和 Profile；向量统一写入外部 Milvus，不再引入 pgvector 或 Chroma。其他项目只需运行应用镜像并配置依赖 endpoint。

仓库可提供用于本地测试的可选 infrastructure Compose，但它与生产部署清单分离，不能成为镜像运行或 API 契约的默认前提。

### 2. 上传期完成物化和画像

文件解析使用 pandas、openpyxl 和 python-calamine：CSV 流式探测编码与分隔符，Excel 按工作表导入。每个工作表物化成独立 PostgreSQL 表，保留稳定的 `dataset_id/table_id`，原始表头保存在元数据中，物理标识符由服务安全规范化。

导入采用 staging table 后原子切换。相同调用方幂等键和文件摘要不得产生重复数据集；重建生成新版本，成功后再切换 active version。查询永远只读取 active version。

外部数据库采用零复制注册：Dataset 保存 `postgresql` 或 `mysql` 来源类型、Secret 引用及授权 database/schema/table 范围，不保存连接明文或业务表副本。注册和刷新任务通过对应 Adapter 使用只读账号读取元数据及有界统计，成功后原子发布新 Profile 版本；刷新失败时继续使用上一已发布 Profile，但执行时若源库不可用则显式失败。

查询 API 不接受临时 DSN。数据源创建 API 仅接受受控 Secret 引用；部署边界必须对可访问主机、端口和网络范围实施 allowlist，连接测试不得成为 SSRF、内网扫描或凭据探测入口。

### 3. SQLAlchemy 画像并输出 M-Schema 兼容上下文

使用成熟的 SQLAlchemy Inspector 通过 DataSource Adapter 提取 PostgreSQL/MySQL 的表、列、类型、主外键和注释，使用源数据库聚合获取统计与示例值，并将统一结构化 Profile 持久化。模型上下文使用公开的 M-Schema 表示约定，但不依赖其无法稳定构建 wheel 的官方仓库，也不复制其数据库访问实现。

低基数字符串列在字段策略允许时完整保存 distinct 值及频次；高基数字符串列按频次、覆盖度和长度限制采样。敏感字段可禁止保存候选值，仅保留允许的统计摘要。数值/日期列保存空值率、最小值、最大值等统计，不逐值向量化。任何采样阈值均为资源配置，不包含业务术语。

### 4. Milvus 承载表、字段和列值检索

使用官方 Milvus SDK。表 Profile、字段 Profile 和列值写入独立 collection 或带类型分区的统一 collection，并通过 `tenant_id/namespace/dataset_version` 标量条件过滤。Milvus endpoint 与 collection 前缀由配置提供，服务不负责 Milvus 生命周期。查询采用 Milvus 向量相似度与 PostgreSQL lexical 相似度融合，输出可审计的候选和分数。

表集合先检索并选择一张最高相关表。只有问题包含跨实体需求且 Profile 中存在外键或已确认关系路径时，才扩展候选；最终最多三张表。不得依赖旧 rerank 分数。

### 5. 单次结构化模型调用完成选表确认和值链接后的 SQL 生成

确定性检索先生成最多三张表的 M-Schema 和真实候选值 evidence，再通过 OpenAI-compatible API 调用主模型一次。响应使用 JSON Schema 约束，包含 `sql`、使用表、使用值和置信信息。模型不负责执行、计算或生成展示答案。

正常路径不得额外调用相关性、选表、SQL critic 或结果总结模型。仅当 SQLGlot/`EXPLAIN`/执行返回可修复错误时，携带原 SQL 和边界化错误再调用同一模型一次；禁止循环修复。

### 6. 数据源适配器隔离方言差异

定义统一 DataSource Adapter，仅暴露连接校验、Inspector、Profile 聚合、EXPLAIN 和只读执行能力。托管文件表和外部 PostgreSQL 复用 PostgreSQL Adapter；外部 MySQL 使用独立 Adapter。Adapter 返回统一领域类型，不让 SQLAlchemy dialect 对象、连接对象或供应商错误越过边界。

SQL 生成必须携带数据源 dialect。SQLGlot 分别按 `postgres` 或 `mysql` 解析和规范化；标识符引用、分页、日期函数和聚合语法由模型上下文及 Adapter 方言决定，禁止在执行前做字符串替换式方言转换。

### 7. 安全能力集中在执行边界

SQLGlot 按数据源方言解析，只允许单条 `SELECT` 或只读 `WITH ... SELECT`。AST 中引用的 database/schema/table 必须属于请求允许的 active dataset version，且最多三张。PostgreSQL 使用 read-only transaction、`statement_timeout` 和 `lock_timeout`；MySQL 使用只读事务、`MAX_EXECUTION_TIME` 及受控锁等待超时。两者均使用源库只读账号和服务端行数上限。

执行前由 Adapter 分别运行 PostgreSQL `EXPLAIN (FORMAT JSON)` 或 MySQL `EXPLAIN FORMAT=JSON`，拒绝解析失败、未知对象、写操作和超预算计划。数据库错误归一为稳定分类后才可进入修复 Prompt，不暴露连接串、主机、用户名或内部堆栈。

### 8. 版本化 API 与可观测性

对外提供 `/v1/datasets`、`/v1/jobs`、`/v1/query`、`/v1/health`。每个请求要求调用方身份和 namespace；第一阶段使用静态 API key 到 tenant 的映射，密钥只从环境或 secrets 注入。

数据源注册扩展 `/v1/datasets` 的判别联合契约：文件上传使用 multipart，外部数据库使用 JSON 并只提交 `dialect`、Secret 引用与授权范围。响应继续返回 dataset/job/version 标识，使 Profile 首建和刷新沿用统一任务查询协议。

查询响应固定返回 `route`、`sql`、`rows`、`columns`、`evidence`、`sources`、`timings`、`model_calls` 和错误码。Prometheus 指标和结构化日志仅记录标识符、阶段耗时和计数，不记录原始单元格值或模型密钥。

### 9. 不完整 Profile 使用表限定证据与按需存在性探测

Profile 字面值证据使用 `database/schema/table/column` 范围内的稳定限定键；无限定列只有在候选表内唯一时才可绑定，禁止多表同名列互相提供证据。完整且新鲜的低基数 Profile 可直接证明值存在或不存在；高基数采样、禁止持久化值和过期外部 Profile 只能证明已观察值，不能据此证明其他值不存在。

对上述不完整 Profile，执行边界可在已选授权表字段上执行一次参数化 `LIKE` 存在性 Probe。Probe 使用只读连接、数据库超时和 `LIMIT 1`，只返回布尔结果，不保存、不向量化、不记录匹配原值。外部数据源 Engine 按方言与已解析 Secret 复用有界连接池，Adapter 仍按请求持有独立授权表白名单。

### 10. 检索与模型上下文按置信度和规模分层

Profile PostgreSQL 增加 trigram 名称/值索引，查询先以索引缩小候选，再执行有界融合排序。是否跳过 Milvus 由标题、字段和值证据强度共同决定，不再以“任意 lexical 结果非空”为充分条件。

字段不超过预算时继续使用完整已发布 M-Schema；宽表首次调用只发送命中列、键列和有界补充列，若首次失败，唯一一次修复自动恢复完整 M-Schema。文件导入增加表/行/列上限，外部字段画像使用配置化有界并发。

### 11. 入库优先保证画像覆盖与有界资源

入库不作为在线延迟路径，不引入额外流式处理服务。高基数字段的有界候选由高频值和全域均匀样本共同组成，避免大量同频值时只保留文件前部记录；低基数字段仍保存完整 distinct 集合。Profile 向量按固定上限分批生成和写入，单版本 Profile 文档总量显式受限，文件表批量写入行数按字段数动态收缩，防止大画像或宽表造成峰值内存、数据库参数上限错误。

查询编排只调用一个全局 Profile 候选检索入口；该入口内部并发检索表、字段和值，并仅在证据不足时共享一次问题向量进行兜底。选表后的目标表值精检继续作为该检索阶段的范围收敛动作，不增加模型调用。

## Risks / Trade-offs

- [列值基数过高导致索引膨胀] → 低基数完整索引，高基数受配置限制采样，并记录覆盖统计。
- [随机/有限样本遗漏同义值] → 不使用随机三值作为唯一依据；使用完整低基数 distinct 值与融合检索。
- [一次模型调用准确率不足] → 将表和候选值检索前移为确定性步骤，并只允许一次基于实际错误的修复。
- [自动推断表关系可能产生错误 JOIN] → 第一阶段只信任数据库外键和导入时显式确认的关系，不凭列名自动建立可执行 JOIN。
- [M-Schema 表示约定变化] → 内部 Profile 保持独立结构化契约，只由纯格式化适配器输出模型上下文并以快照测试校验。
- [共享 PostgreSQL 或 Milvus 中租户越权] → 每次检索和执行都绑定 tenant/namespace，Milvus 搜索强制标量过滤，物理表不直接接受客户端名称，执行前再次校验 AST。
- [外部数据库连接被用于 SSRF 或内网探测] → 查询 API 禁止提交 DSN，Secret 和网络目标由服务端 allowlist 管理，错误响应不泄露目标是否存在。
- [外部数据库权限过大] → 注册时验证只读能力，运行时仍开启只读事务并执行对象白名单校验；文档要求使用专用最小权限账号。
- [PostgreSQL/MySQL 方言行为不一致] → Adapter 隔离 Inspector、EXPLAIN、超时和执行，使用双数据库契约及容器化集成测试，不进行字符串方言改写。
- [Excel 类型推断错误] → 保存原始文本与推断报告；混合类型列降级为 text，不静默丢值。

## Migration Plan

1. 在 `structured-query/` 创建独立服务、Compose、迁移和契约测试，不接入 WeKnora 主流程。
2. 使用可选本地测试基础设施和匿名化 Excel/CSV 验证导入、持久化、Profile 与只读执行。
3. 实现 PostgreSQL DataSource Adapter，并验证托管文件表和外部 PostgreSQL 零复制查询。
4. 实现 MySQL DataSource Adapter，通过同一领域契约验证 Profile、方言校验和安全执行。
5. 使用五道真实问题建立端到端验收基线，记录 SQL、答案和分阶段耗时。
6. 服务达到准确性和延迟门槛后，再新增 WeKnora HTTP 客户端并与 ES/向量并行调用。
7. 删除不再使用的实验性 SQL 运行时代码；评测证据是否保留单独决定，不作为兼容层。

回滚时停止结构化服务调用即可，不影响原有 RAG；数据库版本通过 active version 指针回切，不进行破坏性覆盖。

## Open Questions

- 生产环境主模型和 embedding 模型的具体标识由部署配置确定，开发环境使用 OpenAI-compatible mock 完成确定性测试。
- 五道真实题所对应的最终原始文件必须确认完整版本；“硕士 41 人”将作为验收期望，不以当前不完整样本的 30 人覆盖。
