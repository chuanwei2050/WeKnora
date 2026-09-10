## Context

目标是判断完整 MindsDB Engine 是否比嵌入式组件更接近开箱即用的表格 Text-to-SQL。真实文件只在准备阶段处理，问题阶段通过已连接的 PostgreSQL 表执行。

## Goals / Non-Goals

**Goals:**

- 验证官方 Docker 服务、PostgreSQL connector 和 SQL Agent。
- 复用现有 OpenAI-compatible 主模型，避免部署新模型。
- 用五题测准确率、拒绝行为和端到端耗时。
- 明确 MindsDB 是否自动完成表选择、Schema 上下文和 SQL 执行。

**Non-Goals:**

- 不改生产主链路，不保留旧实验兼容层。
- 不把测试题答案写入 Agent Prompt。
- 不为通过测试另建自定义 Profile 检索器。

## Decisions

1. 使用官方 `mindsdb/mindsdb` Docker 镜像和独立 volume，开放 HTTP/MySQL API。
2. 表格一次性写入 PostgreSQL 隔离 Schema，由只读角色暴露给 MindsDB。
3. Agent 仅绑定测试数据源的表，模型使用现有 OpenAI-compatible `base_url`。
4. 通过 HTTP SQL API创建对象和提问，记录服务端响应时间；不增加外部纠错模型。
5. 结束后删除 Agent、MindsDB 数据源、数据库隔离对象、容器和 volume。

## Risks / Trade-offs

- [镜像和集成依赖较大] → 单独记录首次部署体积和时间。
- [Agent 可能多次调用模型且调用数不透明] → 结合日志和响应元数据判断，无法证明时明确标注。
- [结构化表没有值级 Profile] → 不自行补建索引，以真实开箱能力作为结论。
- [OpenAI-compatible 适配差异] → 优先使用官方 `openai` provider 的 `base_url`。

## Migration Plan

仅当五题准确率与延迟通过后再提出生产接入；本变更只做隔离验证，可完整删除。
