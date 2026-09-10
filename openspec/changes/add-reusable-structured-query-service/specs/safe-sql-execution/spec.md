## ADDED Requirements

### Requirement: 只允许受限只读 PostgreSQL SQL
服务 MUST 使用 SQLGlot 按数据源的 PostgreSQL 或 MySQL 方言解析 SQL，并只允许引用当前 active dataset version 中最多三张授权 database/schema/table 的单条 SELECT 或只读 WITH 查询。

#### Scenario: 模型生成写操作
- **WHEN** 模型返回 INSERT、UPDATE、DELETE、DDL、多语句或未授权表引用
- **THEN** 服务拒绝执行并返回稳定安全错误码

#### Scenario: 只读语句调用数据库主机能力
- **WHEN** SELECT 调用文件读取、目录枚举、跨库连接或延时消耗函数
- **THEN** AST 安全边界以危险函数错误拒绝执行，即使语句只引用授权表

### Requirement: 执行必须具有数据库级资源边界
服务 SHALL 使用源库只读账号和只读事务。PostgreSQL Adapter MUST 设置 statement timeout、lock timeout 并执行 `EXPLAIN (FORMAT JSON)`；MySQL Adapter MUST 设置受控执行/锁等待超时并执行 `EXPLAIN FORMAT=JSON`；两者均 MUST 限制返回行数和计划预算。

#### Scenario: 查询超过执行时间限制
- **WHEN** PostgreSQL 查询运行超过配置的 statement timeout
- **THEN** 数据库中止查询且服务返回超时错误，不遗留运行中事务

### Requirement: 失败最多修复一次
SQL 在解析、EXPLAIN 或执行阶段发生可修复错误时，服务 MAY 使用相同主模型进行一次修复；第二次失败 MUST 终止链路。

#### Scenario: 首次 SQL 字段不存在
- **WHEN** 首次 SQL 引用不存在字段且错误被分类为可修复
- **THEN** 服务最多再调用一次模型，并在响应中报告两次模型调用和两份 SQL 轨迹

### Requirement: 返回精确结果而非模型计算值
服务 SHALL 直接返回数据库列、行和来源信息；任何计数、聚合和筛选结果 MUST 来自已执行 SQL。

服务 MUST 同时限制结果行数、单元格字节数和响应总字节数，超限时拒绝返回结果。
合法执行产生的空结果或零聚合 SHALL 直接返回，不得仅依据结果值触发模型放宽条件。

#### Scenario: 人员计数问题
- **WHEN** SQL 返回聚合计数
- **THEN** API 返回数据库计数值、对应 SQL 和来源表，不要求模型再次解释或重算

### Requirement: SQL 过滤字面值必须由字段 Profile 支持
服务 MUST 在执行边界按 SQL AST 将字符串过滤字面值绑定到对应字段，并验证其连续有效片段可由该字段已发布 Profile 的真实值支持；不得执行模型凭空组合的筛选值。校验失败时，服务 MAY 向唯一一次修复调用提供该字段的有界 Profile 证据，但不得把完整字段值集合写入错误响应或查询轨迹。

多表查询 MUST 使用表限定字段绑定 Profile，禁止同名字段跨表共享证据。对于高基数采样字段、禁止持久化值字段或已超过新鲜度门槛的外部 Profile，服务 MAY 在已选授权表上执行参数化、只读、限时的存在性 Value Probe；Probe 结果不得返回或持久化原值。

#### Scenario: 模型组合出数据中不存在的证书名称
- **WHEN** SQL 的 WHERE、HAVING 或条件聚合使用一个无法由对应字段 Profile 支持的字符串字面值
- **THEN** 服务以稳定错误码拒绝执行，并最多携带有界真实值证据修复一次

#### Scenario: 完整 Profile 可证明 OR 分支恒为假
- **WHEN** 低基数字段的完整 distinct Profile 可证明一个 OR 分支永远无法命中、且另一分支受 Profile 支持
- **THEN** 服务可使用 SQL AST 删除恒假分支而不调用模型；采样 Profile 不得执行此化简

#### Scenario: 两张表存在同名字段
- **WHEN** SQL 使用表别名引用两张表的同名字段
- **THEN** 每个字面值仅由其所属表字段的 Profile 支持，另一张表的值不得放行该条件

#### Scenario: 查询高基数字段中的稀有值
- **WHEN** SQL 字面值未出现在有界 Profile 样本中但目标字段允许实时查询
- **THEN** 服务仅在授权目标字段执行存在性 Probe；真实存在时继续校验，不存在时进入最多一次修复

### Requirement: 数据源连接和错误必须保持边界安全
查询只能使用数据集已绑定的服务端 Secret 引用；数据库错误 MUST 归一为稳定错误码，不得返回 DSN、主机、用户名或供应商堆栈。

#### Scenario: 外部数据库连接失败
- **WHEN** 已注册数据源在查询时不可达
- **THEN** 服务返回稳定不可用错误且不泄露连接目标，不回退到其他数据库或数据副本
