## ADDED Requirements

### Requirement: Integration RAG 按请求控制不可搜索文件夹过滤

系统 MUST 在 `POST /api/integration/v1/rag/search` 接受可选布尔字段 `filter_disabled_folders`。字段缺失或为 `false` 时 MUST 不应用文件夹搜索开关；字段为 `true` 时 MUST 在已授权的 `knowledge_base_ids` 范围内排除不可搜索文件夹及其后代文件。

#### Scenario: 单次 RAG 缺省不过滤
- **WHEN** 主体提交合法单次 RAG 请求但未提交 `filter_disabled_folders`
- **THEN** 系统不按文件夹搜索开关过滤结果

#### Scenario: 单次 RAG 启用过滤
- **WHEN** 主体提交 `filter_disabled_folders=true`
- **THEN** 系统在本次所选知识库内排除关闭搜索的文件夹及其后代文件

#### Scenario: 单次 RAG 与 folder_ids 组合
- **WHEN** 主体同时提交合法非空 `folder_ids` 和 `filter_disabled_folders=true`
- **THEN** 系统使用既有文件夹范围与可搜索文件夹范围的交集执行检索

### Requirement: 批量 RAG 每个查询独立控制过滤

系统 MUST 在 `POST /api/integration/v1/rag/search-batch` 的每个 query 中接受可选布尔字段 `filter_disabled_folders`。每个 query 的缺省值 MUST 为 `false`，且其策略 MUST 独立应用，不得由同批其他 query 继承或覆盖。

#### Scenario: 同批查询采用不同策略
- **WHEN** 同一批次一个 query 提交 `filter_disabled_folders=true`，另一个 query 提交 `false` 或缺失该字段
- **THEN** 系统仅对前一个 query 应用不可搜索文件夹过滤，并按原请求顺序返回两者结果

#### Scenario: 批量查询分别组合 folder_ids
- **WHEN** 多个 query 分别提交不同 `folder_ids` 和不同 `filter_disabled_folders` 值
- **THEN** 系统为每个 query 独立计算其最终文件夹范围

#### Scenario: 交集为空不使整批失败
- **WHEN** 某个 query 的合法 `folder_ids` 与可搜索文件夹范围交集为空
- **THEN** 该 query 以 completed 状态返回空结果且其他 query 继续执行
