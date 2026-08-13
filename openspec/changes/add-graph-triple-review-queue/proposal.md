## Why

立项要求知识入图有质量把关。现有能力是：`strict_schema` 自动过滤、知识**版本**治理审核、按需抽取。这些**不能**代替「人对抽取三元组的通过/驳回」。需要独立的图三元组审核队列：抽取先入 staging，审核通过后再写入 Neo4j，避免与知识版本治理或 schema 硬过滤重复建设。

## What Changes

- 新增图三元组候选（staging）模型与审核状态机：`pending` → `written` | `rejected`（approve 动作在写图成功后进入 `written`；写失败保持 `pending` 可重试）。
- 正式抽取写图路径：可配置「需审核」时只写 staging，不直接 `AddGraph`；通过后**复用**既有写图/canonical 接口。
- 管理 UI：待审列表、三元组详情、通过/驳回；交互可借鉴治理列表，**表/API 完全独立**。
- 顺序：schema 过滤 → staging（或直写）；审核不替代 schema。
- 同 chunk 再次抽取产生新候选时，MUST 将该 chunk 仍为 `pending` 的旧候选标为 superseded/取消，避免重复积压。

## Capabilities

### New Capabilities

- `graph-triple-review`: 抽取三元组人工审核队列、staging 持久化、通过后写入图库；与知识版本治理分离。

### Modified Capabilities

无。

## Impact

- 后端：staging 表/仓储、`extract` 分支、审核 API；写图只调用既有路径。
- 前端：独立「图三元组审核」页 + GraphSettings 开关。
- `ExtractConfig.require_triple_review` 默认 false。
- 兼容：关闭=现网直写。
