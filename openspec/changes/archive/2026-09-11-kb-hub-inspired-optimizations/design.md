## Context

本变更从 knowledge-hub 借鉴产品与工程模式，落在 WeKnora 现有栈上：Go + asynq、多检索引擎、Vue 前端、治理版本机、Neo4j Graph-in-RAG。约束：默认行为兼容；不替换父子分块、多向量驱动、版本治理与图检索主路径；语言与产出用简体中文，代码标识保持英文。

当前缺口：
- references 等于召回全集，噪声大。
- 实体图谱缺独立浏览画布，且易与 Wiki 页面图谱混淆。
- 章节标题只在 merge 时临时补，不进 embedding。
- 行内驳回可能未强制意见。
- enable/tag 热更新已存在但 API 不统一。

## Goals / Non-Goals

**Goals:**

- 引用按模型实际使用过滤，可配置降级策略。
- 提供与 Wiki 图谱区分的实体图谱浏览（overview + 力导向）。
- Heading 继承进 chunk 正文，新建默认开启。
- 强化文档列表行内驳回必填意见（不做跨库工作台）。
- 统一索引元数据热更新，禁止误触向量重算。
- P3：实体图谱重建 UX；可选 ACL 热更新（默认关）。

**Non-Goals:**

- 不新增跨知识库审核工作台/导航角标。
- 不迁移到 NestJS / 单一 ES 向量方案。
- 不替换治理版本状态机为 Hub 四态文档机。
- 不取消 Graph-in-RAG 或改用「图不进问答」。
- 不强制存量文档全量重嵌。
- 不引入任意 Cypher 开放执行。
- 不把 Wiki 页面图谱与 Neo4j 实体图谱合并成同一入口。
## Decisions

### 1. 引用过滤：双路径、统一过滤器

- 新增 `cite_filter`：输入答案文本 + 候选 sources，输出过滤后列表。
- Knowledge QA：Prompt 增加 `[n]` 约定；解析 `\[(\d+)\]` 映射 `MergeResult`。
- Agent：解析已有 `<kb ... chunk_id="...">`，按 chunk_id 过滤 `KnowledgeRefs`。
- 模式：`off`（默认，兼容）| `cited_or_all`（有 cite 过滤，无 cite 回退全量）| `cited_only`。
- 挂点：`session_knowledge_qa` emit references 前；Agent `finalize` / stream final answer 前。

备选：只改前端展示 — 拒绝，因落库与 SSE 仍噪声。

### 2. 图谱浏览：只读 explore，与检索解耦

- API：`GET /api/v1/knowledge-bases/:id/graph/overview`（及可选 search/nodes），scope 用现有 tenant/KB/governance 可见知识。
- 节点：Knowledge（文档）+ Entity；边：mentions（evidence/instance）+ related（canonical relation）。不画 chunk 节点以免过碎。
- 前端：KB 下「图谱」页，ECharts force graph + 类型统计 + 筛选；复用 GraphScope 权限。
- 不改抽取 schema 与 SearchPaths。

备选：嵌入 Neo4j Browser — 拒绝，权限与产品体验不可控。

### 3. Heading 继承：切块开关，默认开

- `ChunkingConfig.InheritHeading`：**新建知识库默认 true**；存量 JSON 缺省字段仍为 Go zero（false），保持兼容。
- `heading_tracker` 维护 ATX 标题栈；新块无标题行则前缀路径；与表头 tracker 共存（先 heading 再 table header）。
- Parent-child：child 继承。

备选：仅增强 query-time StructuralContext — 不足以改善向量召回。

### 4. 审批：只强化行内驳回，不做独立工作台

- 文档列表已有 submit/approve/reject；跨库审批场景不存在，**不**做审核工作台/导航角标。
- 借鉴 Hub：驳回必须填写非空意见（后端已有则补齐前端 prompt/对话框）。

### 5. 元数据热更新：白名单 patch

- 接口：`BatchUpdateChunkMetadata`；白名单 `is_enabled`、`tag_id`。
- 委托现有 BatchUpdate enable/tag，禁止改 content/vector。

### 6. 实体图谱 vs Wiki 图谱

| | Wiki 图谱 | 实体图谱（本变更） |
|--|-----------|-------------------|
| 数据 | Wiki 页面与页面链接 | Neo4j CanonicalEntity / Relation / 文档提及 |
| 入口 | Wiki 模式下 tab「Wiki 图谱」 | 启用实体抽取后 tab「知识图谱」；设置页可浏览/重建 |
| 用途 | 浏览 Wiki 结构 | 浏览抽取实体关系；与 Graph-in-RAG 解耦 |

Wiki 开启且同时启用实体抽取时，两个 tab 并存，文案不得都叫「图谱」。

### 7. P3 重建与 C2 ACL

- 重建：UI 确认后 DeleteCanonical + 重入队 post_process。
- C2：`INDEX_ACL_HOT_UPDATE` 默认关。

## Risks / Trade-offs

- [模型漏标导致空引用] → 默认 `cited_or_all` 或先 `off` 灰度；监控无 cite 率。
- [Heading 改变 content hash/长度] → 默认关；仅新导入；文档说明需重嵌才生效。
- [多引擎 metadata 语义不一致] → 接口契约 + 各引擎测试；失败可观测。
- [图谱 overview 大图性能] → 节点/边上限、分页或采样；超时降级。
- [审核队列权限过宽] → 仅返回调用者有审核权的 KB 任务。
- [C2 mapping 迁移成本] → 独立开关，未开不影响现网。

## Migration Plan

1. 先合后端过滤器/API（flag 默认兼容）与单测。
2. 前端：引用展示、图谱页、审核台、切块开关、重建按钮分批上线。
3. Heading / C2 保持默认关；运维文档说明何时重嵌。
4. 回滚：关 flag / 隐藏路由即可，无需数据回滚（除已写新 heading 的新 chunk，可接受）。

## Open Questions

- 引用过滤默认值：上线初期用 `off` 还是 `cited_or_all`？（建议先 `off`，租户/会话可开）
- 图谱页是否支持跨 KB 联合浏览？（首版单 KB）
- C2 ACL 是否本迭代必须落地代码，还是仅预留接口与开关？（建议：接口预留 + 最小 stub，完整 ACL 字段按开关实现）
