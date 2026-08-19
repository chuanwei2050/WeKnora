## Why

立项可感知缺口中，Few-shot 分类未接线、推理路径未在对话侧结构化展示、验证分未反馈到路径**展示序**。后端已有 `ComplexityFewShot`、`GraphSearchResult.Paths`、`VerifiedResult`。本 change **只补接线与展示**，不重建路由/多跳/验证。

## What Changes

- 将 `FewShot` 注入既有复杂度分类 prompt；Agent 编辑器可编辑。
- 基于已有 `GraphSearchResult.Paths` 向引用/遥测透出路径摘要；聊天 UI 可折叠展示（不解析 `GraphContext` 字符串另造数据源）。
- **验证分仅影响展示/遥测中的路径顺序**（验证完成之后）；**不得**用验证分回改检索阶段已注入 LLM 的上下文顺序（管线时序上验证在生成之后）。

## Capabilities

### New Capabilities

- `research-explainability-routing`: Few-shot 接线、路径可解释展示、验证后展示序轻量重排。

### Modified Capabilities

无。

## Impact

- 后端：classify prompt；references extra 的 `graph.paths`；验证完成后对 Paths 副本重排供展示。
- 前端：Agent FewShot 编辑；回答侧路径面板。
- 兼容：FewShot 空 / 无 Paths / 无验证 = 现网行为。
