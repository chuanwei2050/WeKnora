## Why



立项要求「按需入图、按需搜图」。代码侧**门禁主干已存在**（`GraphEnabled`、`NeedsEntityRelation`、`shouldUseGraph`、无关系不写图、前端无搜图开关），但正式抽取路径未强制 schema 过滤、缺少 `strict_schema` / 实体类型白名单、测评预设一键加载不足，搜图跳过原因观测也不完整。本 change **硬化既有门禁与配置面**，不是从零重建入库/搜图决策。



## What Changes



- **硬化入库 schema**：正式与试抽取走同一过滤入口——`Tags` 非空时过滤未知关系；修复试抽取在空 Tags 下误丢全部关系；新增 `ExtractConfig.entity_types`；抽取 prompt/`ParseGraph` 须产出 `EntityType`；`strict_schema=true` 时按 `entity_types` 硬过滤；过滤后无合法三元组不写图。

- 在 `ExtractConfig` 增加 `strict_schema`（默认 false）与 `entity_types`；加载软件测评预设时：profile `relations`→`tags`，`concepts`→`entity_types`，并可默认打开 `strict_schema`。

- **复核既有 RAG 搜图门禁**（不重写 `shouldUseGraph`）：两层判定 + 精确跳过原因；旧 reason 保留或双写兼容；**不**新增搜图 UI 开关。

- 增强 `GraphSettings.vue`：**仅补**严格 schema、`entity_types` 编辑、规则说明、「加载软件测评预设」；示例 `nodes`/`relations` 仍作 few-shot，不作白名单。

- 补单元/集成测试；对已有门禁以回归测试固化。



## Capabilities



### New Capabilities



- `graph-ingest-retrieval-gates`: 正式/试抽取 schema 过滤对齐、`strict_schema` + `entity_types`、抽取规则配置增强、既有入图/搜图门禁契约化与可观测补齐。



### Modified Capabilities



无（不修改已冻结的 multihop / complexity-routing 核心需求）。



## Impact



- 后端：`ExtractConfig`（`strict_schema`、`entity_types`）、抽取 prompt 注入与 `ParseGraph` 填充 `EntityType`、统一过滤逻辑、正式与试抽取写图/返回路径；搜图遥测补齐。

- 前端：`GraphSettings.vue`、i18n；client SDK `ExtractConfig` 同步字段。

- 配置：与 `config/knowledge_profiles/software-testing.yaml` 对齐（concepts→`entity_types`，relations→`tags`）。

- 兼容性：缺省字段为空/`strict_schema=false`；仅影响新抽取。

