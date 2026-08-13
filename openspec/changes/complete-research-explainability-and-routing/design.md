## Context



complexity-routing 已有 L1–L4 与 `FewShot` 字段未注入 prompt；multihop 返回 `Paths` 且 `GraphContext` 已供 LLM，但聊天 UI 无结构化路径面板；verified 在**生成之后**才有置信分。本 change 只接线与展示。



## Goals / Non-Goals



**Goals:**



- Few-shot 进入分类 prompt；Agent 可编。

- 用户可展开查看路径摘要（来自 `Paths`，非新检索）。

- 验证完成后，对**展示用**路径列表做稳定重排。



**Non-Goals:**



- 不重建路由器、多跳、验证框架；不做三元组审核（见另一 change）。

- **不**在检索阶段用验证分重排后再注入上下文（时序不可用）。

- 不做多异构 validator 并行（可选后续增量，本 change 不做）。

- 不做路径大屏/Cypher 暴露。



## Decisions



1. **Few-shot**  

   分类 prompt 追加 `ComplexityFewShot`（question/level/subtype）；默认上限例如 20 条或每 level 5 条，超出截断并打日志。空=现状。Agent 复杂度路由区表格编辑。



2. **路径透出数据源**  

   唯一结构化来源：`chatManage.GraphSearchResult.Paths`（及已有 score/节点/边）。`GraphContext` 继续只服务 LLM，UI **不得**靠解析该字符串。references/telemetry `extra.graph.paths` 为摘要数组。



3. **验证分只调展示序**  

   管线：检索/融合 → 生成 → 验证。因此：  

   - 检索时融合与 `GraphContext` 渲染：**不改**。  

   - 验证完成后：置信取自既有 `VerifiedAnswer.Confidence`；Paths>1 时对**展示副本**稳定排序（如 `path.Score * (0.5 + 0.5*confidence)`，同分按原下标）。  

   - 不得回写 `GraphContext`。

4. **Agent UI**  

   在**已有** `complexity_routing` 配置区扩展 FewShot 表格，不新建第二套路由设置页。



## Risks / Trade-offs



- [Few-shot 过长] → 截断上限。  

- [用户以为重排影响了答案] → 文案/实现只动展示序；答案上下文仍为检索时序。  

- [与 GraphContext 重复感] → UI 用结构化 Paths，更清晰。



## Migration Plan



仅接线发布；回滚清空 FewShot 或藏 UI。



## Open Questions



无。

