## Why

查询扩展目前主要由召回数量触发，无法区分“数量少但质量高”和“数量足但质量低”，还可能放大候选和模型调用成本。

## What Changes

- 改为基于检索质量信号和路由预算触发扩展。
- 限制扩展变体数、总候选数和额外调用次数。
- 使用与关键词索引一致的分词语义，并通过离线评测决定默认值。

## Capabilities

### New Capabilities
- `query-expansion-control`: 规定查询扩展触发、预算、分词一致性和可观测性。

### Modified Capabilities

## Impact

影响查询扩展中间件、检索配置、候选去重和离线/单元测试。
