# research-acceptance-benchmark Specification

## Purpose
TBD - created by archiving change add-research-acceptance-benchmarks. Update Purpose after archive.
## Requirements
### Requirement: 评测运行端到端执行真实问答
正式验收运行 MUST 从冻结套件读取案例并调用真实问答入口，采集回答、引用、路由决策、图谱使用情况、验证执行路径、TTFT 和错误，再执行配置的评估器并持久化案例结果；不得仅通过创建运行记录或提交外部结果将门禁置为通过。

#### Scenario: 执行一组冻结案例
- **WHEN** 操作者对已冻结套件创建正式运行
- **THEN** 执行器按快照中的模型、知识版本和 Agent 配置逐例调用问答链
- **AND** 每个案例保存原始回答、证据、路由/图谱/验证遥测和评估结果

#### Scenario: 问答执行失败
- **WHEN** 某案例因超时、模型错误或能力缺失未得到有效回答
- **THEN** 案例按冻结口径计入失败或未完成状态
- **AND** 运行不得因调用方提交通过字段而绕过该结果

### Requirement: 路由与图谱使用指标可审计
正式验收结果 MUST 记录期望与实际的复杂度等级、子类型、`needs_entity_relation`、实际路由动作、图谱使用或跳过原因及验证执行路径，并分别报告分类混淆、图谱误用/漏用和验证路径分布。

#### Scenario: 非关系问题未调用图谱
- **WHEN** 案例标签表明问题不需要实体关系
- **THEN** 结果记录图谱跳过原因
- **AND** 报告不得把该案例计为图谱能力成功调用

