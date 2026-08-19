## 1. Few-shot 接线



- [x] 1.1 分类 prompt 注入 `FewShot`（空跳过；上限截断）

- [x] 1.2 在 Agent 编辑器**已有** complexity_routing 区块扩展 FewShot 表格（question/level/subtype）；保存透传既有 `complexity_routing` 字段

- [x] 1.3 单测：有/无 FewShot 的 prompt 断言



## 2. 路径展示（复用 Paths）



- [x] 2.1 references/telemetry `extra.graph.paths` 来自 `GraphSearchResult.Paths` 摘要

- [x] 2.2 聊天 UI 折叠面板；无路径不渲染；禁止解析 GraphContext 字符串

- [x] 2.3 i18n



## 3. 验证后展示序重排



- [x] 3.1 验证完成后用 `VerifiedAnswer.Confidence` 对展示用 Paths 副本稳定重排；无验证/置信保持原序（单测）

- [x] 3.2 断言：不修改已生成用的 GraphContext / 检索期融合结果



## 4. 验收



- [x] 4.1 路由+FewShot 仍可降级、可观测

- [x] 4.2 有 Paths 可展开；无 Paths 无面板
