## Context



立项要求按需入图、按需搜图。代码侧**已具备门禁主干**，本 change 是 harden，不是 greenfield：



| 能力 | 现状 | 本 change |

|------|------|-----------|

| 未开图不抽 | `IsGraphEnabled` 已拦截 | 回归测试固化 |

| 无关系迹象不入队 | `NeedsEntityRelation(chunk)` 已用 | 回归测试固化 |

| 无关系不写图 | `len(Relation)==0` 已 skip | 在 **schema 过滤之后**再判断 |

| 关系过滤 | `RemoveUnknownRelation` 按 Tags；试抽取总是调用（Tags 空会丢光）；正式路径注释掉 | 正式恢复；**试抽取改为 Tags 非空才过滤** |

| 实体类型白名单 | 无；`nodes` 仅为 few-shot；`ParseGraph` 不填 `EntityType` | 新增 `entity_types`；prompt+解析产出类型；strict 时硬过滤 |

| RAG 搜图 | `shouldUseGraph` + 执行层 EntityKBIDs | 不改主干；补分层文档与遥测 |

| GraphSettings | 启用 / tags / nodes / relations / 试抽 | 加 strict_schema、entity_types、说明、测评预设 |



## Goals / Non-Goals



**Goals:**



- 正式与试抽取在关系类型过滤上一致（Tags 非空才过滤）。

- 抽取链路能产出 `EntityType`；`strict_schema=true` 时按 `entity_types` 拒绝未知实体类型；过滤后无合法关系不写图。

- UI 可配 strict / entity_types，并可加载软件测评预设。

- 搜图两层门禁可测、可审计；无搜图手动开关。



**Non-Goals:**



- 重写 `knowledge_post_process` / `shouldUseGraph`。

- 把「开图 KB」并入 `shouldUseGraph`。

- 用示例 `nodes[].name` 冒充实体类型白名单。

- 人工审图、事件抽取、路径重排、清洗存量图、改 L1–L4 默认关闭。



## Decisions



1. **`strict_schema` + `entity_types` 落在 `ExtractConfig`**  

   - `strict_schema` 默认 `false`。  

   - `entity_types` 为 `[]string`，与 `tags`（关系类型）对称。  

   - 加载测评预设：`concepts` → `entity_types`，`relations` → `tags`，并可设 `strict_schema=true`。



2. **关系白名单 = `Tags`；实体白名单 = `entity_types`**  

   示例 `nodes` / `relations` 仅 few-shot，不作为白名单来源。  

   *否决：用 nodes.name 存类型 — 与现 UI「实体示例」语义冲突。*



3. **先产出 EntityType，再过滤（正式 + 试抽取必须同改）**  

   - **现网缺口**：`ParseGraph` 只填 `Name`/`Attributes`，不填 `EntityType`；canonical 侧空类型会落成默认 `"entity"`，不在测评 `concepts` 白名单内。若只加过滤器不开产类型，strict+预设会几乎写不进图。  
   - **产出**：当 `entity_types` 非空（或 `strict_schema=true`）时，抽取 prompt MUST 注入实体类型白名单，并要求模型输出实体类型；`ParseGraph`（或等价解析）MUST 填充 `GraphNode.EntityType`。  
   - **关系过滤**：`Tags` 非空 → 正式与试抽取都丢弃未知关系（不依赖 strict）。`Tags` 空且 `strict=false` → 不按关系类型硬丢。  
   - **试抽取必改点**：现网试抽取在 Tags 为空时仍调用 `RemoveUnknownRelation`，会丢光全部关系。实现 MUST 改为走统一过滤入口：仅当 `Tags` 非空才做关系类型过滤；禁止「无条件调用旧过滤」导致空 Tags 丢光。正式路径恢复调用时也走同一入口，不得各写一套。  
   - **实体过滤**：仅 `strict_schema=true` 且 `entity_types` 非空 → 丢弃 `EntityType` 为空或不在白名单的节点及其相关关系（空类型不得靠事后默认 `"entity"` 逃过严格过滤）。  
   - `strict_schema=true` 且 `Tags` 为空 → 不写图。  
   - `strict_schema=true` 且 `entity_types` 为空 → 不写图（与空 Tags 对称）。  
   - 过滤后 `len(Relation)==0` → 正式跳过写入；试抽取返回空关系结果。

4. **搜图两层门禁（不重写）**  

   - 层 1 `shouldUseGraph`：关系需求 ∧（无路由或预算允许）。  
   - 层 2 执行层：开图 KB + 可检索实体。  
   - 端到端三条件 ≠ 改 `shouldUseGraph` 签名。

5. **观测补齐（精确 reason + 旧串兼容）**  

   - 无路由时 `graph.requested` 对齐层 1。  
   - 新/精确 reason 至少区分：`relation_not_needed`、`routing_budget_disabled`、`no_graph_kb`、`no_entities`、`no_graph_evidence`。  
   - **兼容策略**：不得直接删除现有验收可能依赖的旧串（如 `routing_disabled_or_relation_not_needed`）。优先在主字段写精确 reason，并对旧消费者**保留旧串或双写**（例如 `reason` + `reason_legacy`，或在文档化映射下继续输出可识别的旧值）。本 change 不要求强制更名打断验收脚本。



6. **UI**  

   严格 schema、`entity_types` 编辑（类似 tags）、规则说明、加载测评预设；禁止搜图开关。



## Risks / Trade-offs



- [空 Tags 试抽取行为变化] → 探索型 KB 试抽不再被误清空；与正式路径一致。  

- [strict 误杀] → 默认 false；预设可编辑。  

- [抽取未产 EntityType] → 本 change 必做 prompt+解析；未产出则 strict 下丢弃并打日志，不得默认成 `"entity"` 绕过。  

- [遥测更名] → 优先补齐/双写，避免打断验收。



## Migration Plan



1. 部署含新字段的后端（缺省兼容）。  

2. 正式+试抽取按新过滤语义上线；前端发布 GraphSettings。  

3. 回滚：关 strict / 回退版本；图数据无需回滚。



## Open Questions



无。

