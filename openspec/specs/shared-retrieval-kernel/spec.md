# shared-retrieval-kernel Specification

## Purpose
TBD - created by archiving change unify-retrieval-kernel. Update Purpose after archive.
## Requirements
### Requirement: 普通与 Agent RAG 必须复用检索内核
系统 MUST 让普通 RAG 与 Agent RAG 通过同一内核执行范围规范化、检索路由前的知识库简称查询扩展、召回调度、治理过滤、重排结果分类和检索诊断。两条路径 MUST 使用相同的当前版本、有效期和可检索状态治理规则。

#### Scenario: 相同请求进入两类入口
- **WHEN** 普通 RAG 与 Agent RAG 使用相同租户、问题、知识库范围和简称配置检索
- **THEN** 两者在路由前得到相同的查询文本，且底层候选、过滤结果和 outcome 语义一致

#### Scenario: 简称配置未命中
- **WHEN** 问题未命中当前知识库范围内的简称配置
- **THEN** 普通 RAG 与 Agent RAG 均使用原问题继续检索，不调用模型猜测简称含义

### Requirement: 上层编排保持独立
共享内核 MUST NOT 决定回答生成、Agent 工具循环、回答后反思或流式响应终止。

#### Scenario: 检索无相关结果
- **WHEN** 共享内核返回无相关结果
- **THEN** 普通 RAG 与 Agent RAG 可按各自上层协议处理，但不得改变该检索状态的含义

