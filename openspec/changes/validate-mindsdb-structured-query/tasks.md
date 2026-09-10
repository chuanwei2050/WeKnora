## 1. 部署

- [ ] 1.1 固定官方 MindsDB 镜像并创建隔离 Compose、volume 和配置模板
- [ ] 1.2 启动服务并验证 HTTP/MySQL API 健康
- [ ] 1.3 安装并验证 PostgreSQL 与 OpenAI integration

## 2. 数据准备

- [ ] 2.1 幂等导入真实 XLSX 到 PostgreSQL 隔离 Schema
- [ ] 2.2 创建只读角色并验证 SELECT 成功、写操作失败
- [ ] 2.3 在 MindsDB 创建 PostgreSQL 数据源并验证 229 行可查询

## 3. Agent

- [ ] 3.1 使用现有 OpenAI-compatible 主模型创建仅绑定测试表的 SQL Agent
- [ ] 3.2 验证 MindsDB 是否自动提供 Schema、选表、SQL 和执行证据
- [ ] 3.3 记录模型调用行为与完整链路耗时

## 4. 五题验收

- [ ] 4.1 独立确认真实数据期望答案
- [ ] 4.2 执行五题并保存回答、SQL/证据、耗时和判定
- [ ] 4.3 检查 42/3/29、麦伟华、许乃汉及公司体系拒绝结果
- [ ] 4.4 与 LlamaIndex/Wren 结果比较并给出最终建议

## 5. 审查清理

- [ ] 5.1 检查无密钥、无答案硬编码且未修改主链路
- [ ] 5.2 运行 Compose、OpenSpec 和脚本校验
- [ ] 5.3 精确清理 Agent、数据源、Schema、角色、容器和 volume
