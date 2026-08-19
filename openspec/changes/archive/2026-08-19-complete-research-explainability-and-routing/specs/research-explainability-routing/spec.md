## ADDED Requirements

### Requirement: Few-shot 注入复杂度分类
当复杂度路由启用且 `ComplexityRoutingConfig.FewShot` 非空时，系统 MUST 将这些示例注入既有复杂度分类 prompt（可按配置上限截断）。FewShot 为空时分类行为 MUST 与现网一致。Agent 编辑 UI MUST 允许查看与编辑 FewShot（question、level、subtype）。

#### Scenario: 有示例时注入
- **WHEN** 路由启用且配置了至少一条 FewShot
- **THEN** 分类模型输入 MUST 包含这些示例内容（或截断后的前缀）

#### Scenario: 无示例时不变
- **WHEN** FewShot 为空
- **THEN** 分类行为 MUST 与未配置示例时一致

### Requirement: 对话侧推理路径展示
当本轮 `GraphSearchResult.Paths` 非空时，系统 MUST 在引用遥测或流式附加信息中提供路径摘要（节点名序列、关系类型、分数），数据 MUST 来自结构化 Paths，MUST NOT 通过解析 `GraphContext` 字符串重建。前端 MUST 提供可折叠展示。无路径时 MUST NOT 展示空面板。系统 MUST NOT 为此重建图检索逻辑。

#### Scenario: 有路径可展开
- **WHEN** `GraphSearchResult.Paths` 非空
- **THEN** 用户 MUST 能展开查看路径摘要

#### Scenario: 无路径不展示
- **WHEN** 无图路径
- **THEN** UI MUST NOT 显示推理路径面板

### Requirement: 验证后仅重排展示路径序
当本轮验证完成后存在可用置信或等价分数，且 Paths 条数大于 1 时，系统 MUST 对**展示/遥测用的路径列表**按稳定可测规则重排。当无验证分时，展示路径顺序 MUST 与检索融合后的既有顺序一致。系统 MUST NOT 因验证分回写或重算已注入生成阶段的 `GraphContext` 内容。

#### Scenario: 无验证不改展示序
- **WHEN** 无验证结果
- **THEN** 展示路径顺序 MUST 与检索融合结果一致

#### Scenario: 有验证只改展示序
- **WHEN** 验证完成且有多条路径
- **THEN** 展示/遥测路径顺序 MUST 按规则稳定重排，且生成阶段已用的 GraphContext MUST 保持不变
