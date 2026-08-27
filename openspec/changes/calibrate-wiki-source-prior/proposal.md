## Why

Wiki/FAQ 当前以固定倍数在 Rerank 后加分，可能破坏分数尺度，且发生在 MMR 之后时无法正确表达来源先验。

## What Changes

- 将来源偏好建模为有界、可配置且可观测的先验。
- 保留原始相关性分数，并在 MMR 前应用统一归一化后的先验。
- 通过离线评测检查来源置换、相关性和无答案指标后确定默认值。

## Capabilities

### New Capabilities
- `retrieval-source-prior`: 规定 Wiki/FAQ 等来源先验的应用阶段、分数边界和评测门禁。

### Modified Capabilities

## Impact

影响检索插件顺序、排序分数结构、MMR 输入、配置和排序测试。
