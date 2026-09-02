## ADDED Requirements

### Requirement: 检索请求显式控制不可搜索文件夹过滤

系统 MUST 在对话、Agent、悬浮窗、`@` 文件搜索和直接 RAG 检索入口接受统一的 `filter_disabled_folders` 布尔策略。字段缺失或为 `false` 时 MUST 不应用 `knowledge_tags.search_enabled` 过滤；仅当字段为 `true` 时 MUST 应用该过滤。

#### Scenario: 字段缺失时不进行过滤
- **WHEN** 调用方未提交 `filter_disabled_folders`
- **THEN** 系统不因文件夹 `search_enabled=false` 排除文件

#### Scenario: 字段为 false 时不进行过滤
- **WHEN** 调用方提交 `filter_disabled_folders=false`
- **THEN** 系统不应用文件夹搜索开关过滤

#### Scenario: 字段为 true 时进行过滤
- **WHEN** 调用方提交 `filter_disabled_folders=true`
- **THEN** 系统只允许处于可搜索文件夹范围内的文件参与检索或 `@` 列表展示

### Requirement: 关闭父文件夹向全部后代继承

当过滤策略启用时，系统 MUST 仅允许自身及全部祖先均为 `search_enabled=true` 的真实文件夹参与搜索。任一祖先关闭时，系统 MUST 排除该文件夹及其全部后代中的文件，即使后代自身开关为开启。

#### Scenario: 父级关闭而子级开启
- **WHEN** 普通一级文件夹为 `search_enabled=false`，其二级文件夹为 `search_enabled=true`
- **THEN** 系统排除一级文件夹和二级文件夹中的全部文件

#### Scenario: 父子级全部开启
- **WHEN** 普通一级文件夹及其二级文件夹均为 `search_enabled=true`
- **THEN** 系统允许二级文件夹中的文件参与检索

#### Scenario: 子级自身关闭
- **WHEN** 父文件夹开启但子文件夹为 `search_enabled=false`
- **THEN** 系统排除子文件夹中的文件且不影响父文件夹中的文件

### Requirement: 过滤不得扩大授权检索范围

系统 MUST 只在本次请求已经授权且已经选定的知识库和文件范围内应用文件夹过滤，MUST NOT 因 `filter_disabled_folders=true` 自动增加当前租户的其他知识库。共享知识库的文件夹状态 MUST 按该知识库所属租户解析。

#### Scenario: 请求只选择一个知识库
- **WHEN** 当前租户拥有多个知识库但请求只选择其中一个并启用过滤
- **THEN** 系统只检索所选知识库，不查询其他知识库内容作为结果

#### Scenario: 过滤共享知识库
- **WHEN** 请求包含已授权的其他租户共享知识库并启用过滤
- **THEN** 系统使用共享知识库所属租户的文件夹状态收窄该知识库范围

### Requirement: 显式范围与搜索开关组合

当请求同时提供显式 `folder_ids` 和 `filter_disabled_folders=true` 时，最终标签范围 MUST 为既有合法文件夹范围与可搜索文件夹范围的交集。合法范围被开关排除时 MUST 返回空结果而不是参数或权限错误；显式文件落入被搜索开关排除的文件夹时 MUST 跳过该文件。

#### Scenario: 显式文件夹处于关闭范围
- **WHEN** 请求提交合法 `folder_ids` 且这些文件夹被自身或祖先搜索开关关闭
- **THEN** 系统返回空检索结果且不返回 `invalid_folder_ids`

#### Scenario: 显式文件被搜索开关排除
- **WHEN** 请求提交合法 `knowledge_ids`，其中部分文件位于不可搜索文件夹并启用过滤
- **THEN** 系统跳过被排除文件并继续检索其他合法文件

#### Scenario: 未启用过滤时保留显式范围
- **WHEN** 请求提交合法 `folder_ids` 且 `filter_disabled_folders` 缺失或为 `false`
- **THEN** 系统仅应用既有 `folder_ids` 范围而不检查搜索开关
