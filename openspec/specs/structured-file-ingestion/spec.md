# structured-file-ingestion Specification

## Purpose
TBD - created by archiving change add-reusable-structured-query-service. Update Purpose after archive.
## Requirements
### Requirement: 上传文件异步物化为 PostgreSQL 数据集
服务 SHALL 接受 CSV、XLS、XLSX 文件，并将每个有效工作表物化为独立 PostgreSQL 表；上传请求 SHALL 快速返回可查询的任务标识。

#### Scenario: 上传多工作表 Excel
- **WHEN** 调用方上传包含三个有效工作表的 Excel
- **THEN** 服务创建一个数据集版本、三个逻辑表和一个异步导入任务，并可通过任务接口查询进度

### Requirement: 导入必须幂等且可原子切换
服务 MUST 使用调用方幂等键和文件摘要避免重复导入，并 SHALL 仅在整个版本导入与 Profile 构建成功后将其标记为 active。

#### Scenario: 重复提交相同文件
- **WHEN** 同一 tenant、namespace 和幂等键再次提交内容相同的文件
- **THEN** 服务返回原任务或原数据集版本，不创建重复物理表

#### Scenario: 重建中途失败
- **WHEN** 新版本任一工作表导入或画像失败
- **THEN** 原 active version 保持可查询且失败版本不会进入查询候选

### Requirement: 导入不得静默丢失原始语义
服务 SHALL 保存原始文件名、工作表名、原始表头、规范化字段映射、行数和类型推断报告；混合类型字段 SHALL 降级为 text 或明确失败。

#### Scenario: 中文和特殊字符表头
- **WHEN** 表格包含中文、空格或重复表头
- **THEN** 服务生成安全唯一的物理列名，并保留到原始表头的可逆映射

### Requirement: 文件和外部画像必须具有资源边界
文件导入 SHALL 对文件大小、工作表数量、单表行数和列数设置配置化上限并显式失败。外部数据库画像 MAY 并发统计字段，但并发数 MUST 有界且复用同一数据源连接池。
XLSX 解压大小、归档条目和工作表维度，以及 CSV 行列边界，MUST 在构造完整 DataFrame 前检查。

#### Scenario: 工作簿超过部署限制
- **WHEN** 工作表数、行数或列数超过配置上限
- **THEN** 导入任务以稳定失败状态终止，不继续物化超限数据

#### Scenario: 宽表批量写入
- **WHEN** 工作表字段数使固定批量行数可能超过数据库绑定参数上限
- **THEN** 服务按字段数降低单批行数并继续导入，而不是形成超限 SQL 或无限重试

### Requirement: 外部数据库必须零复制注册
服务 SHALL 支持注册 PostgreSQL 和 MySQL 数据源，只保存 Secret 引用、方言和授权 database/schema/table 范围，并 MUST NOT 将外部业务表复制到服务数据库；查询 API MUST NOT 接受临时 DSN。

#### Scenario: 注册外部 MySQL 数据库
- **WHEN** 授权调用方提交 MySQL 方言、服务端 Secret 引用和表白名单
- **THEN** 服务创建数据集及异步 Profile 任务，不复制源表数据

#### Scenario: 查询时提交临时连接串
- **WHEN** 调用方向查询 API 提交 DSN 或连接凭据
- **THEN** 服务按严格契约拒绝请求，不尝试连接目标地址

### Requirement: 数据库 Profile 刷新必须版本化
外部数据库首次注册和后续刷新 SHALL 使用统一异步 Job 构建新 Profile 版本，并 SHALL 仅在完整成功后原子发布；失败不得替换上一 active Profile。

#### Scenario: Profile 刷新中途失败
- **WHEN** Inspector 或统计采集在刷新过程中失败
- **THEN** Job 标记失败且上一 active Profile 继续用于检索

