## MODIFIED Requirements

### Requirement: 既有入库门禁保持有效

系统 MUST 仅以知识库 `IndexingStrategy.GraphEnabled` 作为图谱功能启用来源，并在启用时对文档 chunk 执行图抽取。知识库内部继续支持 `all` 与 `signal` 两种入图范围，但普通设置 MUST 使用默认 `all` 且不要求用户配置；`signal` 仅作为高级兼容策略。

#### Scenario: 知识库未开图
- **WHEN** 知识库 `IndexingStrategy.GraphEnabled` 为 false
- **THEN** 文档后处理 MUST NOT 创建图抽取任务

#### Scenario: signal 模式下 chunk 无关系迹象
- **WHEN** 知识库已开图且高级兼容入图范围为 `signal`，但某文本 chunk 未通过关系迹象判定
- **THEN** 系统 MUST NOT 为该 chunk 入队图抽取任务

#### Scenario: all 模式覆盖非空文本
- **WHEN** 知识库已开图且某文本类 chunk 非空
- **THEN** 默认配置 MUST 为该 chunk 入队图抽取任务

### Requirement: 图谱抽取规则配置增强（不含搜图开关）

知识库图谱设置 MUST 使用通用抽取、使用模板和自定义 Schema 三种方式。通用抽取不得要求 Schema 或 few-shot；模板 MUST 填充可编辑的具体 Schema；自定义 Schema MUST 使用 `Tags` 作为关系类型、`entity_types` 作为实体类型并启用严格过滤。UI MUST NOT 提供抽取模型、入图范围或问答搜图开关。

#### Scenario: 配置自定义 Schema 并保存
- **WHEN** 管理员编辑实体类型和关系类型并保存
- **THEN** 后续入库抽取 MUST 按保存后的严格 Schema 执行过滤

#### Scenario: 配置严格 schema 与 entity_types 并保存
- **WHEN** 管理员在自定义 Schema 模式编辑关系类型和实体类型并保存知识库
- **THEN** 后续入库抽取 MUST 按保存后的严格 Schema 执行过滤

#### Scenario: 加载软件测评预设
- **WHEN** 管理员选择使用模板并加载软件测评模板
- **THEN** 关系类型与实体类型 MUST 被填充为模板默认集合，且可再编辑

#### Scenario: 通用抽取
- **WHEN** 管理员选择通用抽取
- **THEN** 系统 MUST 允许模型输出动态实体和关系类型且不得因空白名单清空结果

#### Scenario: 无搜图开关
- **WHEN** 管理员打开知识库图谱设置页
- **THEN** 页面 MUST NOT 出现控制问答是否查询图数据库的开关
