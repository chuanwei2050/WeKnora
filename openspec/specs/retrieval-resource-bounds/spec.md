# retrieval-resource-bounds Specification

## Purpose
TBD - created by archiving change bound-retrieval-concurrency. Update Purpose after archive.
## Requirements
### Requirement: 单请求检索必须有并发上限
系统 MUST 对一个请求内的全部检索分支应用共享并发上限，并 MUST 在请求取消后停止等待和传播取消。
KB、Web 和查询扩展分支 MUST 共用该请求级上限；普通 RAG 和 Agent RAG 合并后的最终候选均不得超过固定预算。

#### Scenario: 多目标并行检索
- **WHEN** 请求包含超过并发上限的检索目标
- **THEN** 同时执行的检索调用不得超过上限，其余调用有界等待

#### Scenario: 等待期间取消
- **WHEN** 请求在分支等待并发额度时取消
- **THEN** 等待分支及时退出且不得发起新的后端调用

### Requirement: 候选预算必须全局有界且公平
系统 MUST 使用不随请求输入增长的固定上限限制请求级候选总数。系统 MUST 在边界拒绝超过候选预算的显式范围；在合法范围内，MUST 保留每个显式范围至少一个名额。
Agent 检索入口 MUST 在任何完整输入日志或范围解析前，同时限制查询和知识库 ID 的数量、单项字节数与请求内总字节数，且不得仅依赖工具 schema 提示。日志 MUST 仅记录有界摘要。

#### Scenario: 候选超过全局上限
- **WHEN** 多个目标返回的去重候选超过请求上限
- **THEN** 系统在治理过滤后截断候选，且最终数量不得超过固定上限
- **THEN** 系统在保留显式范围最低名额后按排序截断并记录截断数量

#### Scenario: 显式范围超过候选预算
- **WHEN** 请求携带的唯一显式知识范围数量超过固定候选预算
- **THEN** 系统在启动并发检索前拒绝请求

#### Scenario: 显式文件内容相同
- **WHEN** 两个显式文件各自唯一候选具有相同内容签名
- **THEN** 跨文件去重不得删除任一文件的保底候选，同一 Chunk 的重复召回保留最高分

#### Scenario: Agent 查询输入超过边界
- **WHEN** Agent 工具收到超过数量、单条长度或总字节预算的查询数组
- **THEN** 系统在创建检索 goroutine 和调用 embedding 前拒绝输入

