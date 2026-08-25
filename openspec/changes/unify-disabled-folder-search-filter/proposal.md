## Why

文件夹“搜索”开关目前在对话、悬浮窗、`@` 文件列表和 Integration RAG 中采用不同的隐式规则，调用方无法按单次请求决定是否过滤。普通文件夹新增二级关系后，现有仅检查直接文件夹开关的实现还会放行已关闭父文件夹下的子文件，无法满足一致、可预测的检索范围控制。

## What Changes

- 为对话、悬浮窗、`@` 文件搜索、单次 RAG 和批量 RAG 统一增加 `filter_disabled_folders` 布尔字段。
- 字段缺失或为 `false` 时不应用文件夹搜索开关过滤；字段为 `true` 时排除关闭搜索的文件夹及其全部后代中的文件。
- 批量 RAG 在每个 query 上独立携带该字段，允许同一批次内混合过滤与不过滤。
- 将过滤范围限制在本次请求已经授权且选定的知识库内，不因启用过滤而扩大知识库范围。
- 明确 `folder_ids` 与搜索开关同时存在时取交集；合法但被搜索开关排除的范围返回空结果或跳过相应文件，不误报为越权。
- 统一服务层的祖先感知判定，供向量检索、显式文件检索和 `@` 文件列表复用。
- **BREAKING**：此前对话、Agent、悬浮窗和 `@` 列表会隐式过滤；变更后调用方必须显式传 `filter_disabled_folders: true` 才会保持该行为。

## Capabilities

### New Capabilities

- `request-folder-search-filter`: 定义各检索入口按请求控制不可搜索文件夹过滤、祖先继承、默认值及范围组合规则。

### Modified Capabilities

- `integration-knowledge-api`: 为单次和批量 RAG 增加统一的 `filter_disabled_folders` 请求契约，并定义它与 `folder_ids`、`knowledge_ids` 的组合行为。
- `floating-chat-widget`: 悬浮窗发起知识检索时显式选择是否过滤不可搜索文件夹，不再依赖服务端入口的隐式默认值。

## Impact

- API：对话请求、`GET /knowledge/search`、`POST /api/integration/v1/rag/search` 与 `POST /api/integration/v1/rag/search-batch` 增加同名布尔字段或查询参数。
- 后端：会话请求类型、Integration handler、检索目标构建、文件夹范围解析和 `@` 文件搜索需要统一传递过滤策略。
- 数据：不新增数据库字段；复用 `knowledge_tags.search_enabled` 与普通文件夹 `parent_id`。
- 前端与客户端：主对话、悬浮窗和 `@` 列表若要保持现有隐藏效果，必须显式传 `true`；SDK 与外挂知识库文档需要同步字段定义。
- 测试：覆盖字段缺失、真假值、父子文件夹、显式文件、`folder_ids` 交集、共享知识库租户归属以及批内混合策略。
