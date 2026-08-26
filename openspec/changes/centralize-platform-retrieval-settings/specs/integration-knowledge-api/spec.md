## MODIFIED Requirements

### Requirement: RAG 搜索必须支持多个授权知识库
系统 MUST 要求请求显式提交至少一个 `knowledge_base_id`，对所有授权知识库使用平台检索策略执行统一召回和统一重排，并返回全局 Top K；字段缺失、null 或空数组 MUST 返回 `400`，MUST NOT 被解释为全部知识库。系统 MUST 接受可选的 `folder_ids` 多选字段：字段缺失、null 或空数组时不得按文件夹筛选；字段为非空数组时，最终检索范围 MUST 为显式文件夹及其全部子文件夹，与这些显式文件夹所属知识库的公共子文件夹的并集。请求 `top_k` 只能缩小平台 `rerank_top_k` 所定义的最终响应上限，MUST NOT 放大或改变平台召回、融合和 rerank 预算。

#### Scenario: 搜索两个授权知识库
- **WHEN** 具有 `rag:search` scope 的主体提交两个授权知识库、query 和合法 `top_k`
- **THEN** 系统使用平台策略在两个知识库的候选中统一重排
- **AND** 返回结果不超过 `min(top_k, 平台 rerank_top_k)`

#### Scenario: 未指定 top_k
- **WHEN** 合法搜索请求省略 `top_k` 或提交 0
- **THEN** 系统使用平台 `rerank_top_k` 作为最终响应上限

#### Scenario: top_k 大于平台上限
- **WHEN** 调用方提交大于平台 `rerank_top_k` 的合法 `top_k`
- **THEN** 系统仍按平台最终上限返回
- **AND** 内部召回、融合和 rerank 候选预算不受请求值影响

#### Scenario: 搜索请求包含越权知识库
- **WHEN** `knowledge_base_ids` 包含任一不在最终有效范围的 ID
- **THEN** 系统整次返回 `403`，不执行部分搜索，并记录被拒绝 ID

#### Scenario: 搜索未选择知识库
- **WHEN** `knowledge_base_ids` 缺失、为 null 或为空数组
- **THEN** 系统返回 `400`，不执行搜索，也不自动使用全部授权知识库

#### Scenario: 未提供文件夹筛选
- **WHEN** `folder_ids` 缺失、为 null 或为空数组
- **THEN** 系统不增加标签过滤并检索所选知识库的全部可见内容

#### Scenario: 按多个文件夹搜索
- **WHEN** 主体提交属于所选知识库的非空 `folder_ids`
- **THEN** 系统检索显式文件夹及其全部子文件夹
- **AND** 系统检索这些显式文件夹所属知识库内的全部公共子文件夹
- **AND** 对重复的公共子文件夹 ID 去重

#### Scenario: 文件夹不属于所选知识库
- **WHEN** 任一 `folder_id` 不存在、属于其他租户或不属于本次 `knowledge_base_ids`
- **THEN** 系统整次返回 `400 invalid_folder_ids` 且不执行部分检索

#### Scenario: 显式选择未分类
- **WHEN** `folder_ids` 包含“未分类”标签 ID
- **THEN** 系统返回 `400 invalid_folder_ids`

#### Scenario: 文档筛选与文件夹筛选同时存在
- **WHEN** 请求同时提交非空 `knowledge_ids` 和非空 `folder_ids`
- **THEN** 系统仅允许显式文档属于最终文件夹并集
- **AND** 任一显式文档不在最终范围时整次返回 `400 invalid_knowledge_folder_scope`
- **AND** 直接 Chunk 加载不得绕过最终文件夹范围

### Requirement: RAG 搜索必须支持有界批量查询
系统 MUST 在 `POST /api/integration/v1/rag/search-batch` 接受共享的非空 `knowledge_base_ids` 和有界 `queries` 数组。每个查询 MUST 携带批内唯一稳定 ID、非空 query，并 MAY 携带该查询的 `knowledge_ids`、`folder_ids` 与 `top_k`。系统 MUST 在执行任何查询前校验整批参数、知识库授权和文件夹范围，以受控并发使用平台检索策略执行检索，按请求顺序返回逐查询状态和结果；每个查询的 `top_k` 只能缩小平台最终响应上限，批量请求 MUST 作为一次独立限流操作计费。系统 MUST 在逐查询截断后应用平台 `batch_max_results` 和 `batch_max_content_chars`，并在不改变逐 query 响应结构的前提下公平分配整批预算。

#### Scenario: 批量搜索多个业务目标
- **WHEN** 具有 `rag:search` scope 的主体提交多个唯一查询和已授权知识库
- **THEN** 系统以配置化并发和相同平台检索策略执行
- **AND** 按原顺序返回每个查询 ID、状态和不超过各自有效响应上限的结果

#### Scenario: 批量查询使用不同 top_k
- **WHEN** 批量内两个查询分别提交小于和大于平台 `rerank_top_k` 的 `top_k`
- **THEN** 两个查询分别使用请求缩小值和平台上限返回
- **AND** 两个查询的内部召回、融合和 rerank 候选预算相同

#### Scenario: 批量查询使用不同文件夹范围
- **WHEN** 批量内不同查询分别提交不同的非空 `folder_ids`
- **THEN** 系统分别使用各自显式文件夹及其全部子文件夹，与显式文件夹所属知识库内全部公共子文件夹的并集

#### Scenario: 批量查询未提供文件夹筛选
- **WHEN** 某个查询的 `folder_ids` 缺失、为 null 或为空数组
- **THEN** 该查询不增加标签过滤并检索共享知识库的全部可见内容

#### Scenario: 批量请求包含重复查询 ID
- **WHEN** 同一批次包含重复查询 ID、空查询或超过服务端查询数上限
- **THEN** 系统整批返回 `400`，不执行部分检索

#### Scenario: 批量请求包含越权知识库
- **WHEN** 批量请求的共享知识库范围包含任一越权 ID
- **THEN** 系统整批返回 `403`，不执行任何查询

#### Scenario: 批量请求包含无效文件夹
- **WHEN** 任一查询的 `folder_ids` 包含无效、未分类或不属于共享知识库范围的 ID
- **THEN** 系统整批返回 `400 invalid_folder_ids`，不执行任何查询

#### Scenario: 批量子查询的文档超出文件夹范围
- **WHEN** 任一子查询同时提交 `knowledge_ids` 和 `folder_ids` 且存在范围外文档
- **THEN** 系统整批返回 `400 invalid_knowledge_folder_scope`，不执行任何查询

#### Scenario: 批量结果超过平台总结果预算
- **WHEN** 多个查询的逐查询结果合计超过平台 `batch_max_results`
- **THEN** 系统以轮询方式按请求顺序分配名额并保持各查询内部顺序
- **AND** 整批结果数不得超过平台硬上限
- **AND** 返回结构仍按原查询 ID 和请求顺序组织

#### Scenario: 批量结果包含跨查询重复证据
- **WHEN** 多个查询的结果具有相同 `knowledge_version_id + chunk_id`
- **THEN** 系统在整批结果中仅保留首次选中的证据
- **AND** 其他查询保持原状态但其结果 MAY 因全局去重而为空

#### Scenario: 批量正文达到字符预算
- **WHEN** 候选正文无法放入平台 `batch_max_content_chars` 的剩余预算
- **THEN** 系统跳过该完整候选并继续检查其他候选，且不得截断证据正文
- **AND** 整批返回正文字符数不得超过平台硬上限

#### Scenario: 调用方为批量子查询指定 top_k
- **WHEN** query 提交 `top_k` 或省略该字段
- **THEN** 单条有效上限继续为 `min(top_k, 平台 rerank_top_k)` 或平台 `rerank_top_k`
- **AND** 系统不读取或提供独立的批量单条 rerank 配置
