## Context

现有 WikiBoost 在 Rerank/多样性处理之后乘固定系数，原始分数被覆盖且先验无法参与正确的候选选择阶段。

## Goals / Non-Goals

**Goals:** 在 MMR 前应用有界来源先验，保留原始分数，并用离线评测校准。

**Non-Goals:** 不保证 Wiki/FAQ 永远高于其他来源，不用来源弥补低相关性。

## Decisions

- 分数结构同时保留 raw relevance、source prior 和 final ranking score。
- 在同一归一化域中以有界加性或凸组合应用内部类型化先验，再进入 MMR；忽略内容元数据中的同名值并拒绝非有限数。
- 默认先验只有在离线切片未显著伤害相关性、无答案和来源覆盖时才启用。

## Risks / Trade-offs

- [Risk] 来源偏置挤掉更相关证据 → 设置硬上限并报告 source displacement。
- [Risk] 历史消费者依赖 Score → 保持输出字段兼容并新增诊断字段。
