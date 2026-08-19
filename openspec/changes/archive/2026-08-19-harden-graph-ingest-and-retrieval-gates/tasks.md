## 1. 严格 schema 与抽取路径过滤（净新增）



- [x] 1.1 在 `ExtractConfig` 增加 `strict_schema`（默认 false）与 `entity_types`（`[]string`）；JSON/YAML 兼容；client SDK 同步字段

- [x] 1.2 抽取产出 `EntityType`：`entity_types` 非空或 `strict_schema=true` 时，prompt 注入实体类型白名单；扩展 `ParseGraph`（或等价）填充 `GraphNode.EntityType`；禁止仅靠事后默认 `"entity"` 冒充已分类

- [x] 1.3 实现统一过滤入口：`Tags` 非空 → 丢弃未知关系；`strict_schema=true` 且 `entity_types` 非空 → 丢弃空/未知 `EntityType` 及其关系；`strict_schema=true` 且（Tags 空或 entity_types 空）→ 不写图/试抽无合法关系；`Tags` 空且非严格 → 不因空白名单丢光关系

- [x] 1.4 接线两处调用方：正式入库写图路径恢复过滤；试抽取路径改掉「Tags 为空仍调 `RemoveUnknownRelation` 丢光」——两处均先产出类型再走 1.3 统一入口；正式路径过滤后无合法关系则跳过 Neo4j 写入并打日志



## 2. 既有门禁复核与观测补齐（不重写）



- [x] 2.1 为既有入库门禁补回归测试：`!IsGraphEnabled` / `!NeedsEntityRelation(chunk)` 不入队抽取

- [x] 2.2 保持 `shouldUseGraph` 仅表达层 1；补齐遥测：无路由时 `graph.requested` 与层 1 对齐；输出精确 reason（`relation_not_needed` / `routing_budget_disabled` / `no_graph_kb` / `no_entities` / `no_graph_evidence`）；对旧串（如 `routing_disabled_or_relation_not_needed`）保留或双写，禁止只更名打断验收

- [x] 2.3 并入手工验收：知识库设置与普通问答 UI 无「是否搜索图库」开关（见 4.3；发现则删除，禁止新增）



## 3. 前端 GraphSettings 增量



- [x] 3.1 增加严格 schema 开关、`entity_types` 编辑控件与规则说明（Tags / entity_types / 搜图自动判断）

- [x] 3.2 「加载软件测评预设」：relations→`tags`，concepts→`entity_types`；加载时可默认打开 `strict_schema`（不把 concepts 写入示例 nodes 充当白名单）

- [x] 3.3 保存链路透传 `strict_schema` 与 `entity_types`；补齐 zh-CN / en-US i18n



## 4. 测试与验收



- [x] 4.1 单元测试：ParseGraph/过滤产出并识别 EntityType；Tags 非空丢弃未知关系；空 Tags+非严格不丢光（含试抽取）；strict 丢弃空/未知实体类型；strict+空 Tags 或空 entity_types 不写图；过滤后空关系不写图；正式与试抽取同一入口

- [x] 4.2 单元/集成测试：非关系问题不搜图且有跳过原因；层 1 允许但无开图 KB 记 `no_graph_kb`（或等价）；精确 reason 与旧串兼容/双写；关系问题且开图时搜图

- [x] 4.3 手工核对：可配 tags/entity_types/严格模式/预设；页面与普通问答无搜图开关

