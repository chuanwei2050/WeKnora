## Why

对照内部参考项目 knowledge-hub 后，WeKnora 在 RAG 深度、治理版本与 Graph-in-RAG 上已更强，但仍有几处产品与工程质量缺口：引用面板噪声大、实体图谱缺独立浏览画布（且需与 Wiki 页面图谱区分）、切块未默认把章节标题写入可检索文本、索引元数据热更新不统一。现在一并补齐。

## What Changes

- **引用过滤（P0）**：Knowledge QA / Agent 在落库与推送 references 前，按模型实际引用（`[n]` 或 `<kb chunk_id>`）过滤来源；可配置 `off | cited_or_all | cited_only`（默认 `off`）。
- **实体图谱可视化（P0）**：新增 Neo4j 实体/关系浏览（`graph/overview` + 力导向画布）。与 **Wiki 页面图谱**明确区分：Wiki 图谱展示 Wiki 页面链接；实体图谱展示文档抽取的实体与关系。二者入口文案与 tab 分离。
- **Heading 继承（P1）**：切块将 Markdown 章节标题路径前缀写入 chunk 正文；**默认开启**（新建知识库默认 true；存量未配置保持原行为直至显式保存）。
- **审批流程（P1，不做独立工作台）**：不新增跨库审核工作台。文档列表内已有审批操作；仅借鉴 Hub 的「驳回必填意见」强化现有行内驳回 UX。后端 reject comment 门禁已存在则对齐前端。
- **元数据热更新 API（P2）**：统一按 chunk 热更新索引白名单元数据（不重算向量）。
- **图谱链路小优化 / C2 ACL（P3）**：实体图谱一键重建；可选索引 ACL 热更新（默认关）。

无 **BREAKING** 变更。独立「审核工作台」从范围中移除（跨知识库审批场景当前不存在，与文档列表审批重复）。

## Capabilities

### New Capabilities

- `citation-source-filter`: 按模型实际引用过滤问答/Agent 来源列表与 SSE references。
- `knowledge-graph-explore`: Neo4j 实体图谱浏览 API 与画布（与 Wiki 页面图谱分离）。
- `chunk-heading-inheritance`: 切块时继承 Markdown 标题路径并写入可嵌入文本（默认开启）。
- `index-metadata-hot-update`: 检索引擎统一元数据热更新接口（不重 embed）。

### Modified Capabilities

- `knowledge-contribution-review`: 仅补充「驳回必须非空意见」在现有文档列表审批路径上的前端对齐（不新增跨库工作台）。
- `knowledge-graph-configuration`: 补充实体图谱浏览入口、与 Wiki 图谱的产品区分、一键重建。

## Impact

- 后端：`chat_pipeline`、`session_knowledge_qa`、`agent`、`chunker`、`retriever`、Neo4j overview、图谱重建路由。
- 前端：引用过滤、KB 切块默认、实体图谱 tab/文案（区别于 Wiki 图谱）、行内驳回意见、图谱设置重建。
- 不替换多向量引擎、父子分块、治理版本机、Graph-in-RAG、现有文档列表审批入口。
