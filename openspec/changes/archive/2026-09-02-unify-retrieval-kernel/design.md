## Context

普通 RAG 是固定流水线，Agent RAG 是工具驱动循环；重复点在底层检索，而不是回答控制流。

## Goals / Non-Goals

**Goals:** 共用检索请求规范化、调度、治理过滤、Rerank outcome 和遥测协议。

**Non-Goals:** 不合并回答生成、Agent ReAct、反思或流式事件编排。

## Decisions

- 提取无 UI/会话依赖的 retrieval kernel，输入为规范化请求，输出为类型化结果与诊断信息。
- 两个入口各自适配 kernel，保留原有上层状态机。
- 先以契约测试锁定行为，再逐入口迁移，避免一次性替换。

## Risks / Trade-offs

- [Risk] 抽象吞入上层差异 → kernel 只拥有检索语义，任何回答策略留在调用方。
- [Risk] 迁移期双路径漂移 → 共享测试夹具对比两入口的底层输出。
