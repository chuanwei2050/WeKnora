## ADDED Requirements

### Requirement: 表格上传完成后异步触发结构化导入
知识库 API SHALL 在 Excel/CSV 原始文件可读取后调用独立结构化服务创建数据集，并保存外部任务与数据集标识；导入失败不得阻塞普通文档 RAG 索引。

#### Scenario: 上传 Excel 到知识库
- **WHEN** Excel 文件上传并持久化成功
- **THEN** 系统异步提交结构化导入，同时继续原有文档解析流程

### Requirement: 历史表格必须支持幂等回填
系统 SHALL 提供一次性运维命令，扫描历史 CSV/XLS/XLSX，跳过已经绑定结构化数据集的文档，并通过当前文件存储适配器提交结构化导入；命令 SHALL 支持租户范围、dry-run、并发限制、单文档重试和失败退出码。

#### Scenario: 回填中断后重新运行
- **WHEN** 运维人员再次执行同一租户的历史表格回填
- **THEN** 已有 `structured_dataset_id` 的文档被跳过，未成功提交的文档继续处理且不创建重复数据集

### Requirement: 结构化查询与 RAG 检索并行
知识库问答 SHALL 在问题理解前启动结构化服务调用。结构化服务 SHALL 自主返回 `sql` 或 `none`，并与问题理解及原有 ES/向量检索并行；结构化路径不得依赖问题理解中的 SQL 分类或等待 rerank 结果。

#### Scenario: 同一问题同时需要原文和表格证据
- **WHEN** 一个启用结构化服务的知识库问题进入问答流水线
- **THEN** 两条路径并发执行并在汇总阶段合并来源，不互相作为启动前置条件

结构化查询请求 MUST 携带目标知识库实际所属 tenant，并 MUST 将当前请求范围内启用且已绑定的结构化 dataset 标识作为白名单；整库检索也不得省略该白名单。单次请求并发访问 namespace 的数量 MUST 有界。

### Requirement: 非数据事实问题不得强制 SQL
结构化服务 SHALL 在 Schema Linking/Text2SQL 调用中区分表格数据计算/筛选与普通事实性说明；前者返回 `sql` 并执行，后者返回 `none` 且不得执行 SQL。

#### Scenario: 询问公司是否具有质量体系
- **WHEN** 用户询问公司是否具有 GJB9001C 或 ISO9001 体系且问题未要求统计表格记录
- **THEN** 系统使用文档 RAG 路径，不因人员证书表存在相似文字而强制调用 SQL
