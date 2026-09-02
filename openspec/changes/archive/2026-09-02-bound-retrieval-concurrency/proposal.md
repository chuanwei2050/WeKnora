## Why

多知识库、多文件目标会并行放大检索调用和候选数量；现有目标预算虽保证每个显式范围至少一个名额，但缺少请求级并发与全局候选上限。

## What Changes

- 保留现有显式范围最低预算语义。
- 增加请求级检索并发上限、取消传播和全局候选上限。
- 为预算截断、排队和取消增加可观测信息及回归测试。

## Capabilities

### New Capabilities
- `retrieval-resource-bounds`: 规定单请求并发、候选预算、公平性与取消行为。

### Modified Capabilities

## Impact

影响并行搜索调度、搜索配置、候选合并、指标与压力/并发测试。
