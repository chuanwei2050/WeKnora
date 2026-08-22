## Context

当前 `IndexingStrategy.GraphEnabled` 与 `ExtractConfig.Enabled` 共同控制抽取，`relations` 同时承担 few-shot 输出和检索白名单，配置更新只保存新值而不处理旧候选或旧图谱。前端还调用了不存在的 `rebuild-index` 后端接口。此次变更横跨配置、任务、审核、检索和 UI，必须保持已有规范化图来源及版本证据模型不变。

## Goals / Non-Goals

**Goals:**

- 用明确的 `general | template | custom` 模式表达合法配置状态。
- 以 `IndexingStrategy.GraphEnabled` 作为唯一启用来源。
- 将正式 Schema 与 few-shot 分离，并让通用抽取无需示例。
- 配置变化能够使旧候选失效，并可对已有文档重新建图。
- 保持文档删除、版本证据和图检索门禁行为一致。

**Non-Goals:**

- 不改变 Neo4j 的文档、分块、实体、关系及证据拓扑。
- 不增加问答时手动搜图开关。
- 不引入新的外部依赖或在线模板服务。

## Decisions

1. `ExtractConfig.Mode` 持久化三种模式；模板是一次性填充器，保存的是具体 Schema，`TemplateKey` 仅用于回显来源。`custom` 自动启用严格 Schema，`general` 清空正式 Schema 并关闭严格过滤。
2. `IndexingStrategy.GraphEnabled` 是唯一功能开关；`ExtractConfig.Enabled` 仅保留 JSON/数据库兼容并在边界同步，不再参与运行时判定。
3. 正式 Schema 使用结构化 `entity_schema` 与 `relation_schema`：实体定义包含类型编码和含义，关系定义包含关系编码、起点类型、终点类型和含义。`EntityTypes/Tags` 由结构化定义派生并保留为存量兼容字段；`Nodes/Relations` 只用于 few-shot。抽取提示使用完整结构化定义，Agent 检索继续读取派生后的 `Tags`，为空表示不限制关系类型。
4. 为影响建图结果的字段生成稳定配置指纹并写入审核候选。保存时指纹变化会 supersede 该知识库的 pending 候选；批准时再次校验当前指纹、chunk 和知识版本。
5. 新增真实 `POST /knowledge-bases/:id/rebuild-index`：复用现有文档处理队列重新入队有效文档，并先删除该知识库现有图来源，返回入队文档数。该接口保持幂等，前端仅在影响索引的配置变化时提示调用。
6. 高级参数继续保存在 `ExtractConfig`，但普通流程使用系统默认；few-shot 保存校验采用“全空合法、部分填写报错”的边界规则。

## Risks / Trade-offs

- [重建期间短暂无图结果] → 删除旧图后异步重建，UI 明确提示并展示入队数量；后续可升级为双 namespace 原子切换。
- [存量配置没有 mode] → 按字段推导：无 Schema 为 general，有 Schema 且能匹配模板为 template，否则 custom。
- [模板应用后继续编辑] → 自动切换为 custom，避免用户误以为仍受模板绑定。
- [待审核表已有旧记录] → 仅将 pending 标记 superseded，保留历史审计记录。

## Migration Plan

1. 增加向后兼容字段和默认/推导逻辑，无需数据库迁移（配置为 JSON）。
2. 先部署后端校验、检索和审核兼容逻辑，再部署新 UI。
3. 存量知识库读取时推导模式；保存后写入规范化模式。
4. 回滚时旧版本仍可忽略新增 JSON 字段，已 supersede 的审核记录保持终态。

## Open Questions

无。模板当前内置“软件测评”，后续模板只需增加本地定义，不改变运行时协议。
