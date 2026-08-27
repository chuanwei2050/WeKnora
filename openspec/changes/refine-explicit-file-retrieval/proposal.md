## Why

显式选择小文件时将全部 Chunk 以满分直接注入，会绕过相关性排序、挤占上下文，并把“选择文件范围”错误等同于“要求全文阅读”。

## What Changes

- 普通问题在所选文件范围内执行混合检索，不再把全部 Chunk 设为满分。
- 仅对明确全文意图保留受控的全文上下文路径。
- 索引暂不可用时保留有界的 DirectLoad 降级，并记录原因。

## Capabilities

### New Capabilities
- `explicit-file-retrieval`: 规定显式文件范围检索、全文意图和 DirectLoad 降级语义。

### Modified Capabilities

## Impact

影响搜索编排、文件过滤条件、DirectLoad 标记、上下文预算和检索测试。
