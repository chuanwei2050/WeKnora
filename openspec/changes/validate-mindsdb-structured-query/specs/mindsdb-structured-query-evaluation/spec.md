## ADDED Requirements

### Requirement: MindsDB 隔离部署
评测 SHALL 使用官方 MindsDB Docker 镜像和独立持久卷启动服务，并提供健康检查和精确清理。

#### Scenario: 服务启动
- **WHEN** 启动评测 Compose
- **THEN** HTTP SQL API 可用且不修改主服务容器

### Requirement: 表格数据源连接
评测 SHALL 在准备阶段将真实 XLSX 幂等写入 PostgreSQL 隔离 Schema，并让 MindsDB 以只读角色连接；提问阶段不得读取源文件。

#### Scenario: 重复准备
- **WHEN** 同一文件准备两次
- **THEN** 数据保持 229 行且不产生重复对象

### Requirement: 一体化 SQL Agent
评测 SHALL 使用 MindsDB Agent 绑定测试表并接入现有 OpenAI-compatible 主模型，由 MindsDB负责 Schema 上下文、SQL 生成和执行。

#### Scenario: 自然语言查询
- **WHEN** 提交可由表格回答的问题
- **THEN** Agent 返回基于实际执行结果的答案并提供可审查的 SQL 或执行证据

### Requirement: 五题验收
评测 SHALL 执行既定五题并记录回答、SQL/证据、耗时和判定，不得在 Prompt 中提供答案。

#### Scenario: 真实问题集合
- **WHEN** 完成五题评测
- **THEN** 三类人数为 42、3、29，两道人名为麦伟华和许乃汉，公司体系问题被拒绝

### Requirement: 安全与清理
数据源角色 MUST 只有 SELECT 权限，且实验结束 SHALL 删除 MindsDB 与 PostgreSQL 隔离资源。

#### Scenario: 写操作
- **WHEN** MindsDB 数据源账户尝试修改测试表
- **THEN** PostgreSQL 拒绝操作
