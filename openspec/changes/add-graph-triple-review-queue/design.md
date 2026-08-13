## Context

harden 已提供 schema 过滤与按需入图；governance 审核的是**知识版本**，不是抽取三元组。本 change 新增 staging + 审核；写图复用 `AddGraph`/canonical，不重做抽取或 Neo4j 访问层。

## Goals / Non-Goals

**Goals:**

- KB 可开「三元组需审核」；开启后新抽取入 staging，**写图成功后**才算入图完成。
- 独立审核 UI/API；与版本治理分离。
- schema 过滤在入 staging 前；无合法关系不入队。
- 同 chunk 重复抽取时收敛 pending，避免重复单。

**Non-Goals:**

- 不替代 `strict_schema`、不清洗存量图、不做事件抽取/Ontology。
- 不把三元组塞进 `KnowledgeVersion` 表或状态机。
- 不重写 `knowledge_post_process` 入队启发式。
- 首版不做批量通过（可后续加）。

## Decisions

1. **`ExtractConfig.require_triple_review`（bool，默认 false）**  
   false=过滤后直写；true=过滤后只 staging。

2. **Staging 字段**  
   tenant、kb、knowledge、chunk、过滤后 GraphData JSON、status、reviewer_id、comment、created_at、reviewed_at、written_at（可空）、superseded_by（可空）。

3. **状态机（去掉含糊的 approved+written）**  
   - `pending`：待审  
   - `written`：审核通过且写图成功（终态）  
   - `rejected`：驳回（终态）  
   - `superseded`：被同 chunk 更新候选替代（终态）  
   - 动作 `approve`：仅 `pending` → 调既有写图 → 成功则 `written`；失败保持 `pending` 并返回错误（可重试）。  
   - 动作 `reject`：仅 `pending` → `rejected` + comment。  
   - 对 `written`/`rejected`/`superseded` 再 approve/reject MUST 幂等拒绝或 no-op（不得重复写图）。

4. **重复抽取**  
   同一 `(tenant, chunk_id)` 新建 pending 前，将该 chunk 上其它 `pending` 标为 `superseded`。

5. **UI**  
   独立页面/路由；文案标明「图三元组审核」；权限沿用租户管理员（与治理页同级，不新造角色体系）。

6. **试抽取**  
   不入 staging。

## Risks / Trade-offs

- [入图延迟] → 默认 false。  
- [积压] → 首版单条审核；列表按 kb/status 筛选。  
- [与版本治理混淆] → 独立 API 前缀与菜单文案。

## Migration Plan

部署表与 API，默认关；需要时对测评 KB 打开。回滚关开关；staging 可保留。

## Open Questions

无。
