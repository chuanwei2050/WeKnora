## Why

LlamaIndex 与 Wren SDK 均要求项目自行补齐 Profile、值采样和上下文编排，未达到“不造轮子”的目标。需要用同一真实数据验证 MindsDB Engine 是否能通过完整服务直接完成数据源连接、表选择、自然语言生成 SQL 和执行。

## What Changes

- 启动隔离的官方 MindsDB Docker 服务并持久化其元数据。
- 将真实 XLSX 在上传阶段写入 PostgreSQL 隔离 Schema，MindsDB 直接连接该数据源。
- 配置现有 OpenAI-compatible 主模型，创建仅绑定测试表的 SQL Agent。
- 使用同一五题记录生成 SQL、执行结果、调用行为与端到端耗时。
- 验证只读权限、错误拒绝和精确清理，不修改主 RAG/SQL 链路。

## Capabilities

### New Capabilities

- `mindsdb-structured-query-evaluation`: 验证 MindsDB 完整服务对知识库表格的自然语言 SQL 查询能力。

### Modified Capabilities

无。

## Impact

- 新增 `tools/mindsdb-evaluation/` 隔离评测配置和报告。
- 临时创建 MindsDB 容器、数据卷、PostgreSQL 隔离 Schema/角色与 MindsDB Agent。
- 不修改生产代码或现有数据库对象。
