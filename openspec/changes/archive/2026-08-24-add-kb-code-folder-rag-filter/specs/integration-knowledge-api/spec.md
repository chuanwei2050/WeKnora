## ADDED Requirements

### Requirement: Integration API 必须支持按知识库编码列出文件夹
系统 MUST 在 `GET /api/integration/v1/knowledge-bases/by-code/{code}/folders` 为同时具有 `kb:list` 和 `knowledge:read` scope 的主体返回编码对应知识库内可用于筛选的真实文件夹，并继续执行租户、client allowlist 和用户权限校验。

#### Scenario: 按编码查询授权知识库文件夹
- **WHEN** 授权主体提交其租户内可访问知识库的有效编码
- **THEN** 系统只返回外层普通文件夹
- **AND** 每项只包含稳定字段 `id`、`name` 和 `sort_order`
- **AND** 结果保持持久化顺序

#### Scenario: 文件夹列表排除固定入口
- **WHEN** 编码对应知识库包含“未分类”、普通文件夹和公共子文件夹
- **THEN** 响应不包含“未分类”
- **AND** 响应不包含无真实标签 ID 的“公共文件”容器
- **AND** 响应不包含“公共文件”下的公共子文件夹

#### Scenario: 编码不存在或知识库不可访问
- **WHEN** 编码不存在或主体无权访问对应知识库
- **THEN** 系统统一返回 `404` 且不泄露知识库是否存在
- **AND** 系统在审计中记录实际拒绝原因

#### Scenario: 编码格式非法
- **WHEN** 路径中的编码不符合允许字符或长度限制
- **THEN** 系统返回稳定的 `400 invalid_knowledge_base_code`

#### Scenario: 客户端只有知识库列表权限
- **WHEN** 主体具有 `kb:list` 但不具有 `knowledge:read` scope
- **THEN** 系统返回 `403` 且不暴露文件夹名称或 ID

## MODIFIED Requirements

### Requirement: RAG 搜索必须支持多个授权知识库
系统 MUST 要求请求显式提交至少一个 `knowledge_base_id`，对所有授权知识库执行统一召回和统一重排，并返回全局 Top K；字段缺失、null 或空数组 MUST 返回 `400`，MUST NOT 被解释为全部知识库。系统 MUST 接受可选的 `folder_ids` 多选字段：字段缺失、null 或空数组时不得按文件夹筛选；字段为非空数组时，最终检索范围 MUST 为显式文件夹与所有所选知识库公共子文件夹的并集。

#### Scenario: 搜索两个授权知识库
- **WHEN** 具有 `rag:search` scope 的主体提交两个授权知识库、query 和合法 `top_k`
- **THEN** 系统在两个知识库的候选中统一重排并返回不超过 `top_k` 的结果

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
- **THEN** 系统检索显式文件夹以及每个所选知识库内全部公共子文件夹
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
系统 MUST 在 `POST /api/integration/v1/rag/search-batch` 接受共享的非空 `knowledge_base_ids` 和有界 `queries` 数组。每个查询 MUST 携带批内唯一稳定 ID、非空 query，并 MAY 携带该查询的 `knowledge_ids`、`folder_ids` 与 `top_k`。系统 MUST 在执行任何查询前校验整批参数、知识库授权和文件夹范围，以受控并发执行检索，按请求顺序返回逐查询状态和结果；批量请求 MUST 作为一次独立限流操作计费。

#### Scenario: 批量搜索多个业务目标
- **WHEN** 具有 `rag:search` scope 的主体提交多个唯一查询和已授权知识库
- **THEN** 系统以配置化并发执行，并按原顺序返回每个查询 ID、状态和全局 Top K 结果

#### Scenario: 批量查询使用不同文件夹范围
- **WHEN** 批量内不同查询分别提交不同的非空 `folder_ids`
- **THEN** 系统分别使用各自显式文件夹与共享知识库范围内全部公共子文件夹的并集

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
