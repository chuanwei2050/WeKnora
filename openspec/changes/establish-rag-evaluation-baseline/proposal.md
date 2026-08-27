## Why

查询扩展、来源加权和阈值目前缺少稳定的离线基线，直接调参无法判断收益是否来自真实相关性改善，或只是增加调用量和上下文。

## What Changes

- 建立分层离线数据集和可复现评测入口。
- 同时度量召回、排序、无答案、引用、回答质量、延迟、Token 和调用次数。
- 保存基线与候选配置对比结果，作为后续调参门禁。

## Capabilities

### New Capabilities
- `rag-offline-evaluation`: 规定 RAG 离线评测数据、指标、执行和结果对比。

### Modified Capabilities

## Impact

新增评测数据契约、运行脚本或测试入口及报告产物，不改变线上默认行为。
