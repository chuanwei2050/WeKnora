# explicit-file-retrieval Specification

## Purpose
TBD - created by archiving change refine-explicit-file-retrieval. Update Purpose after archive.
## Requirements
### Requirement: 显式文件默认限定检索范围
系统 MUST 将所选文件作为混合检索范围，且不得因文件较小而把全部 Chunk 赋予满分。

#### Scenario: 针对小文件询问局部事实
- **WHEN** 用户选择小文件并提出普通事实问题
- **THEN** 系统仅返回文件范围内按相关性排序的候选

### Requirement: 全文与降级加载必须有界
系统 MUST 仅在明确全文意图或索引不可用时使用 DirectLoad，并 MUST 遵守上下文上限和记录触发原因。

#### Scenario: 明确请求全文总结
- **WHEN** 用户明确要求总结所选文件全文且内容未超过上限
- **THEN** 系统可按原顺序提供全文上下文而不伪造相关性满分

#### Scenario: 文件索引不可用
- **WHEN** 所选文件已可访问但索引暂不可查询
- **THEN** 系统可执行有界 DirectLoad 降级并记录索引不可用原因

