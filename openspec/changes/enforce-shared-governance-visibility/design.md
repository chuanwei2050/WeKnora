## Context

文本召回结果按请求租户批量读取 `Knowledge`。共享知识归属源租户时可能查询不到元数据；当前过滤把 `nil` 与真正无版本旧数据一起放行。图谱侧已对缺失元数据采取拒绝策略，两条路径不一致。

## Goals / Non-Goals

**Goals:** 在确认共享访问合法后解析源知识元数据，缺失时 fail closed，并统一当前版本门禁。

**Non-Goals:** 不改变共享授权模型，不迁移历史数据，不把所有无 `CurrentVersionID` 的记录判为非法。

## Decisions

- 元数据读取使用知识 ID 对应的真实归属或已有共享访问安全查询，不能只套请求租户过滤；授权检查仍在边界执行。
- 区分“查不到元数据”和“查到明确的 legacy 记录”：前者拒绝，后者保持兼容。
- 当前版本知识只允许 `knowledge_version_id == current_version_id` 且版本可检索、未失效的结果。
- 拒绝原因结构化，至少区分 metadata_missing、version_mismatch、not_retrievable 和 expired。

## Risks / Trade-offs

- [Risk] 历史脏数据被拒绝造成召回下降 → 指标先暴露具体原因，明确 legacy 记录继续兼容。
- [Risk] 绕过租户边界读取元数据 → 仅返回治理所需字段并要求调用前已验证共享访问。
- [Risk] 批量查询增加延迟 → 使用批量接口并按请求去重 ID。
