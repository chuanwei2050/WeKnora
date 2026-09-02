## Context

`CHUNK_RERANK` 目前以空结果触发 `ErrSearchNothing`，会话层随后直接输出 fallback，无法区分低相关性、候选损坏和外部 Rerank 故障。

## Goals / Non-Goals

**Goals:** 让 Rerank 产出显式状态，由单一控制点决定继续、无答案或降级，并覆盖回归测试。

**Non-Goals:** 不改变 Rerank 算法、阈值或 Agent 的反思策略。

## Decisions

- 使用类型化 outcome 承载状态和候选，避免靠空 slice 或错误文本推断。
- `no_relevant_result` 正常进入无答案路径；`unavailable` 才使用原始召回，且保留降级标记。
- `invalid_candidate` 视为内部数据问题并停止，不将未经验证候选交给模型。

## Risks / Trade-offs

- [Risk] 旧调用方仍依赖 `ErrSearchNothing` → 集中迁移调用点并增加穷尽分支测试。
- [Risk] 降级导致质量下降 → 仅限基础设施故障并暴露指标。
