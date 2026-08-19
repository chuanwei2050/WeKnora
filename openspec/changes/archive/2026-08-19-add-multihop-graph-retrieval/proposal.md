## Why

立项材料要求跨文档、跨实体的链式检索、多跳推理和可解释推理路径。当前 Neo4j 检索只返回命中实体的一跳邻居，`query_knowledge_graph` 工具实际调用的是文本 `HybridSearch`，返回的可视化边为空且明确提示完整图查询仍未实现，因此尚不能满足 RAG-Graph 的核心要求。

## What Changes

- 将当前按单文档隔离的实体升级为由知识库、实体类型和归一名称共同标识的规范实体或显式跨文档对齐关系，并为节点、关系保留 knowledge、chunk 和可选 version 证据来源。
- 新增受限的图检索服务，从规范化问题实体出发执行可配置深度的有界多跳遍历，不接受用户直接提交任意 Cypher。
- 支持跨文档路径、关系类型白名单、方向、最大深度、分支数、时间预算和最小路径分数。
- 综合实体匹配、关系权重、路径长度和文本相关性进行路径排序，并去除重复/环路结果。
- 返回真实节点、边、推理路径、证据 chunk 与文档引用；无有效路径时明确回退到现有混合检索。
- 将多跳结果接入普通 chat pipeline 和 `query_knowledge_graph` Agent 工具，并在前端展示可核验的路径。
- 增加图查询隔离、超时、跨租户权限和降级测试。

## Capabilities

### New Capabilities

- `multihop-graph-retrieval`: 有界多跳图遍历、路径排序、证据回溯、混合检索融合与可视化输出。

### Modified Capabilities

无。

## Impact

- 后端：图谱抽取/写入与重建任务、Neo4j repository 接口、knowledge base service、chat pipeline entity search、Agent graph tool 和类型定义。
- 前端：`GraphQueryResults` 与答案引用展示。
- 配置：图检索预算、深度、关系过滤和融合权重。
- 数据迁移：既有图没有跨文档规范身份和关系证据，需要按知识库重新抽取并重建；只有存在权威抽取产物时才允许按其来源做可验证回填，不从节点属性猜测关系证据。
- 依赖：沿用现有 Neo4j/APOC 部署；未启用 Neo4j 时保持当前降级路径。
