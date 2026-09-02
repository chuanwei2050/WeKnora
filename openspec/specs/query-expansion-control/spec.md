# query-expansion-control Specification

## Purpose
TBD - created by archiving change improve-query-expansion-control. Update Purpose after archive.
## Requirements
### Requirement: 查询扩展必须由质量和预算共同触发
系统 MUST 基于检索质量信号判断是否扩展，并 MUST 遵守路由决定与请求级调用预算。

#### Scenario: 少量高质量召回
- **WHEN** 候选数量少但顶部相关性满足质量门槛
- **THEN** 系统不得仅因数量不足触发查询扩展

#### Scenario: 路由禁用扩展
- **WHEN** 当前复杂度路由预算不允许扩展
- **THEN** 系统不得调用扩展模型或追加检索

### Requirement: 扩展规模与分词必须受控
系统 MUST 限制变体数、额外调用数和总候选数，并 MUST 使用与关键词索引兼容的查询分析语义。

#### Scenario: 扩展召回包含不可见版本
- **WHEN** 查询扩展召回到旧版、未来生效或已过期内容
- **THEN** 系统必须在合并和候选截断前使用与首轮召回相同的治理规则过滤这些内容

#### Scenario: 中文查询分析
- **WHEN** 中文查询需要关键词变体
- **THEN** 系统复用兼容的查询分析结果，不得默认逐字符拆分产生变体

