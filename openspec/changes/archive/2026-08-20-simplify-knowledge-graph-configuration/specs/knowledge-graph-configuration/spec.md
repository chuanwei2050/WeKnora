## ADDED Requirements

### Requirement: 图谱配置采用三种互斥抽取方式
系统 MUST 支持 `general`、`template`、`custom` 三种抽取方式。普通设置 MUST 只展示启用开关、抽取方式和三元组人工审核；专业参数、few-shot 与试运行 MUST 位于独立的“调试与高级选项”。

#### Scenario: 通用抽取零配置保存
- **WHEN** 管理员启用图谱并选择通用抽取，且未配置示例或 Schema
- **THEN** 配置 MUST 保存成功，页面 MUST 不显示模板、Schema 或应用按钮

#### Scenario: 使用模板
- **WHEN** 管理员选择使用模板
- **THEN** 页面 MUST 显示模板下拉框和应用按钮，应用后 MUST 填充该模板的正式 Schema

#### Scenario: 自定义 Schema
- **WHEN** 管理员选择自定义 Schema
- **THEN** 页面 MUST 在普通区域显示结构化实体类型和关系类型编辑器；实体定义包含类型编码和含义，关系定义包含关系编码、起点实体类型、终点实体类型和含义

#### Scenario: 模板的正式 Schema
- **WHEN** 管理员应用模板
- **THEN** 页面 MUST 展示模板实际提供的结构化实体与关系定义，而不是只展示 few-shot 实例

#### Scenario: 审核顺序
- **WHEN** 管理员打开知识图谱设置
- **THEN** 三元组人工审核 MUST 位于抽取方式之前

#### Scenario: Schema 与 Few-shot 编辑格式一致
- **WHEN** 管理员展开正式 Schema 或 Few-shot 示例
- **THEN** 两者的实体编辑器 MUST 统一使用“名称、类型、说明”，关系编辑器 MUST 统一使用“起点、关系类型、终点、说明”，并且关系实体类型下拉 MUST 忽略空白实体类型编码

#### Scenario: 示例置信度由全局质量边界控制
- **WHEN** 管理员编辑 Few-shot 示例关系
- **THEN** 页面 MUST NOT 要求填写单条关系置信度，抽取过滤 MUST 使用高级设置中的最低关系置信度

### Requirement: few-shot 可选且与正式 Schema 分离
系统 MUST 允许示例文本、示例实体和示例关系全部为空。只要任一 few-shot 字段被填写，系统 MUST 校验示例完整性。示例关系 MUST NOT 作为正式关系白名单或问答检索关系来源。UI MUST 明确称其为可选 Few-shot 示例，不得使用容易与正式 Schema 混淆的“期望实体/期望关系”标题。

#### Scenario: 不配置 few-shot
- **WHEN** 管理员清空示例文本、实体和关系后保存
- **THEN** 系统 MUST 接受配置且正式抽取 MUST 不注入 few-shot

#### Scenario: 示例不完整
- **WHEN** 管理员仅填写示例关系而没有对应示例文本和实体
- **THEN** 系统 MUST 拒绝保存并返回可理解的校验错误

### Requirement: 配置变化要求重建已有图谱
系统 MUST 检测影响建图结果的配置变化。知识库已有文档时，UI MUST 提示重建；重建 API MUST 按当前配置重新处理有效文档并返回入队数量。

#### Scenario: Schema 变化
- **WHEN** 已有文档知识库的抽取方式、实体类型、关系类型或抽取策略变化
- **THEN** 保存成功后 MUST 提示管理员重建图谱

#### Scenario: 执行重建
- **WHEN** 管理员确认重建
- **THEN** 后端 MUST 清理旧图来源并为当前有效文档创建重新处理任务
