# structured-data-profiling Specification

## Purpose
TBD - created by archiving change add-reusable-structured-query-service. Update Purpose after archive.
## Requirements
### Requirement: 生成并持久化结构化 Profile
服务 SHALL 在上传阶段使用 SQLAlchemy Inspector 和数据库聚合生成并持久化数据库、表、字段、类型、主外键、注释、统计和示例值，并 SHALL 输出 M-Schema 兼容模型上下文；查询阶段 MUST NOT 重新解析原文件。

#### Scenario: 容器重启后查询
- **WHEN** 已完成导入的服务容器重启
- **THEN** 服务直接从 PostgreSQL 恢复 active Profile 和索引，不要求重新上传文件

### Requirement: 列值 Profile 必须来自真实数据
服务 SHALL 对低基数字符串字段索引全部非空 distinct 值及频次，对高基数字段执行有界采样，对数值和日期字段保存统计摘要；实现 MUST NOT 包含面向验收问题的词语硬编码。

#### Scenario: 证书字段存在近义真实值
- **WHEN** 字段数据同时包含“软件测评师”和“软件评测师”等真实值
- **THEN** 两个值分别进入列值 Profile 并可通过语义或 lexical 检索返回

### Requirement: Profile 向量必须使用 Milvus 并保持租户隔离
所有表、字段和列值向量 SHALL 持久化在 Milvus，并 MUST 绑定 tenant、namespace 和 dataset version 标量过滤条件；WeKnora 部署 MUST 支持复用现有 Milvus。

#### Scenario: 两个租户有同名表
- **WHEN** tenant A 与 tenant B 均上传同名工作表
- **THEN** tenant A 的 Profile 检索结果不包含 tenant B 的任何记录

#### Scenario: 复用现有 Milvus
- **WHEN** 部署配置提供现有 Milvus endpoint 和 collection 前缀
- **THEN** 服务不启动 bundled Milvus，并将结构化 Profile 写入隔离命名的 collection

### Requirement: 有界画像必须覆盖高频与分布范围
高基数字符串字段的有限候选 SHALL 同时保留高频值和来自完整 distinct 分布的确定性分散样本，不得在大量同频值时退化为只选择源文件前部记录。向量生成与 Milvus 写入 MUST 使用有界批次，单版本 Profile 文档总量 MUST 具有配置上限，不得为整个版本同时持有全部向量。

#### Scenario: 高基数字段多数值频次相同
- **WHEN** 字段 distinct 数超过画像上限且大量值频次均为一
- **THEN** 候选同时覆盖排序前部和后续分布位置，并且每次 embedding 与写入数量不超过批次上限

### Requirement: 外部数据库画像必须使用方言适配器
服务 SHALL 通过 PostgreSQL 或 MySQL DataSource Adapter 使用 SQLAlchemy Inspector 读取授权对象，并将供应商类型、主外键、注释和统计归一为统一 Profile；不得读取白名单外对象。

#### Scenario: MySQL database 中只授权两张表
- **WHEN** Profile Job 检查该数据源
- **THEN** 仅两张授权表及其字段进入 Profile 和 Milvus

### Requirement: 敏感字段画像必须服从持久化策略
外部数据源字段 SHALL 支持禁止保存 distinct 候选值；被标记敏感的字段 MUST NOT 将原值写入 PostgreSQL、Milvus、日志或错误信息。
外部画像聚合 MUST 在只读、限时事务中执行；禁止保存值或数据库类型不支持比较时，MUST 跳过 minimum、maximum、distinct 分组等可能泄露原值或不合法的统计。

外部数据库字段 MUST 默认禁止持久化和向量化原值，只有字段策略显式授权时才可保存有界候选值；未授权字段只能保存不含原值的行数与非空计数。

#### Scenario: 身份证字段禁止候选值采样
- **WHEN** Profile 刷新处理该字段
- **THEN** 仅保存允许的类型和统计摘要，不保存或向量化任何身份证值

