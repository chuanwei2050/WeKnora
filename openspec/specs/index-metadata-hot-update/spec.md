# index-metadata-hot-update Specification

## Purpose
统一检索引擎白名单元数据热更新（不重嵌），供 FAQ 启停与标签/目录同步等路径委托。

## Requirements

### Requirement: 检索引擎支持元数据热更新
系统 MUST 提供按 chunk 或按 knowledge 批量更新索引元数据的能力，且 MUST 仅允许白名单字段（至少包含 `is_enabled` 与 `tag_id`）。热更新 MUST NOT 重新计算或覆盖向量内容。

#### Scenario: 按 knowledge 更新 tag 不重嵌
- **WHEN** 调用方对某 knowledge 发起 tag_id 热更新
- **THEN** 各已启用检索引擎更新对应元数据，且 embedding/向量字段保持不变

#### Scenario: 非法字段被拒绝
- **WHEN** 热更新请求包含非白名单字段（如 content 或 embedding）
- **THEN** 系统拒绝该请求且不部分写入非法字段

### Requirement: 现有 enable/tag 路径可委托热更新
FAQ 启用状态与标签/目录移动等已有「不重嵌」路径 MUST 继续正确工作，并可委托统一热更新接口实现，对外行为保持兼容。

#### Scenario: FAQ 禁用立即影响检索
- **WHEN** 管理员禁用某 FAQ chunk
- **THEN** 后续检索不再返回该 chunk（或等价过滤），且过程不触发该 chunk 的重新 embedding
