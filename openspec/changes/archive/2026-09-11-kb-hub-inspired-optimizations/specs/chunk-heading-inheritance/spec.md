## ADDED Requirements

### Requirement: 切块默认可继承章节标题
系统 MUST 在知识库切块配置中提供 `InheritHeading`（或等价字段）。新建知识库时 MUST 默认开启。开启后，系统 MUST 在切块时维护 Markdown ATX 标题路径，并对不以标题行开头的后续块将当前标题路径前缀写入可索引正文。

#### Scenario: 新建知识库默认开启
- **WHEN** 管理员创建知识库且未显式关闭 InheritHeading
- **THEN** 切块配置中 InheritHeading 为 true

#### Scenario: 显式关闭时内容不加标题前缀
- **WHEN** `InheritHeading` 为 false 且导入 Markdown 文档
- **THEN** 生成的 chunk 正文不因标题继承而增加章节前缀

#### Scenario: 开启后跨块继承标题
- **WHEN** `InheritHeading` 为 true，文档含 `## 部署` 后跟无标题的段落被切到下一块
- **THEN** 该后续块的可索引正文以当前章节标题路径为前缀

#### Scenario: 块内已有标题不重复前缀
- **WHEN** 某块正文本身以 ATX 标题行开头
- **THEN** 系统 MUST NOT 再叠加同一标题前缀

### Requirement: 标题继承与表头继承共存
开启标题继承时，系统 MUST 继续支持现有 Markdown 表头继承行为，并按「先章节标题路径、后表头」的顺序组装前缀。

#### Scenario: 表格块同时带章节与表头
- **WHEN** 开启 InheritHeading 的文档在某章节下切出延续表头的表格块
- **THEN** 该块正文同时包含章节路径前缀与表头前缀，且章节前缀在前

### Requirement: 存量数据不强制重嵌
切换 InheritHeading MUST 仅影响新处理的文档或显式重建任务；系统 MUST NOT 在仅切换开关时自动对全部存量文档重算向量。

#### Scenario: 仅改配置不触发全库重嵌
- **WHEN** 管理员更改 InheritHeading 并保存
- **THEN** 系统保存配置成功，且 MUST NOT 自动对库内全部已有知识启动全量 re-embed
