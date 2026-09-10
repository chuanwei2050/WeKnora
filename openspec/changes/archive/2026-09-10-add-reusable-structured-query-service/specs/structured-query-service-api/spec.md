## ADDED Requirements

### Requirement: 提供版本化可复用 HTTP API
服务 SHALL 提供 `/v1/datasets`、`/v1/jobs`、`/v1/query` 和 `/v1/health` 的 OpenAPI 契约，且不得要求调用方使用 MCP。

#### Scenario: 其他项目执行结构化查询
- **WHEN** 任意授权项目通过 `/v1/query` 提交 namespace、问题和候选数据集范围
- **THEN** 服务返回稳定 JSON 响应，不依赖 WeKnora 内部类型或进程

### Requirement: 查询响应必须可审计
查询响应 SHALL 包含路由结论、最终 SQL、列与行、Profile evidence、数据来源、分阶段耗时、模型调用次数和稳定错误码。
Profile 检索耗时 SHALL 至少区分元数据范围、lexical、embedding、向量、选表和目标表 Value 精检，且保留汇总检索耗时以兼容调用方。

#### Scenario: 查询成功
- **WHEN** SQL 成功执行
- **THEN** 调用方可以从响应中区分检索、模型、校验和数据库执行耗时

#### Scenario: 定位检索延迟
- **WHEN** 一次查询完成 Profile 检索
- **THEN** 响应和审计轨迹可区分各检索子阶段，不需要依赖敏感查询日志

### Requirement: 执行准确率必须可离线门禁
项目 SHALL 提供不进入在线请求链路的通用评测命令，按外部题库比较最终结果集、来源、模型调用次数和总耗时；评测逻辑不得包含特定业务问题的运行时分支。

#### Scenario: SQL 可执行但结果口径错误
- **WHEN** 评测题期望 42 而查询实际返回 43
- **THEN** 门禁以结果集不一致失败，即使 SQL 已成功执行

### Requirement: API 必须隔离调用方数据
所有数据和查询接口 MUST 校验 API key 所属 tenant，并要求显式 namespace；日志 MUST NOT 记录模型密钥、数据库凭据或完整敏感单元格内容。
受信服务 API key MUST 显式携带正整数 tenant 标识；绑定单租户的 API key 收到不同 tenant 标识时 MUST 拒绝请求。

#### Scenario: 跨租户访问数据集
- **WHEN** tenant A 请求 tenant B 的 dataset id
- **THEN** 服务返回未找到或无权访问，且不泄露该数据集是否存在

### Requirement: 应用镜像必须通过配置复用外部依赖
服务 SHALL 发布一个可分别运行 API 和 Worker 的应用镜像；PostgreSQL DSN、Milvus endpoint、Redis URL、S3-compatible endpoint、LLM endpoint 和 embedding endpoint MUST 通过环境变量或 secret 注入。生产部署 MUST NOT 要求镜像内置这些基础设施。

#### Scenario: 重启应用容器
- **WHEN** 运维人员停止并重新启动 API 和 Worker 容器
- **THEN** 已完成数据集、Profile、任务状态和向量索引保持可用

#### Scenario: WeKnora 复用现有基础设施
- **WHEN** 运维人员配置现有 PostgreSQL、Milvus、Redis、S3-compatible 对象存储、LLM 和 embedding endpoint
- **THEN** 只需运行 structured-query 应用镜像，不创建任何重复基础设施实例

#### Scenario: 本地开发环境
- **WHEN** 开发者需要在没有现有基础设施的机器上测试
- **THEN** 可显式使用独立的测试 Compose 启动依赖，且该 Compose 不改变生产镜像契约

### Requirement: 数据集 API 必须支持受控外部数据源注册
`/v1/datasets` SHALL 使用可辨识联合契约支持文件上传及 PostgreSQL/MySQL 数据源注册。数据库注册只能提交方言、Secret 引用、授权范围和字段策略，MUST 拒绝明文 DSN、密码及查询时临时连接参数。

#### Scenario: 注册 PostgreSQL Secret 引用
- **WHEN** tenant A 提交其有权使用的 Secret 引用和 schema/table 白名单
- **THEN** API 返回 dataset/version/job 标识并异步建立 Profile

#### Scenario: tenant A 引用 tenant B 的数据源
- **WHEN** tenant A 查询或刷新不属于自己的数据源
- **THEN** API 返回不可枚举的 404，且日志不记录连接凭据
