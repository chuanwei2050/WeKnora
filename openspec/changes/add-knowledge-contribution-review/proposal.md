## Why

当前普通成员通常需要知识库管理权限才能上传或治理内容，无法表达“向他人创建的知识库贡献自己的文档，审批后才入库”。需要拆分知识库管理权、文档贡献权和审核权，同时保持现有非治理知识库的行为兼容。

## What Changes

- 为知识库增加 `closed`、`members`、`allowlist` 三种 `contribution_mode`，历史知识库默认 `closed`。
- 记录文档最初贡献者，并允许符合贡献策略的同租户成员管理自己的 draft 和 rejected 内容；pending_review 提交后不可直接修改，只能在审核决定前显式撤回到 draft。
- 将贡献、审核和知识库管理拆成独立权限；提交人不得审核自己的版本。
- 定义草稿、提交、审批、索引、激活、驳回及新旧 active 版本原子切换流程。
- 治理知识仅检索当前 active 且处于有效期内的版本；未启用治理和历史知识继续沿用现有可见性规则。

## Capabilities

### New Capabilities

- `knowledge-contribution-review`: 定义知识库投稿策略、文档所有权、审批职责分离、版本发布以及历史数据兼容要求。

### Modified Capabilities

无。

## Impact

- 影响知识库、知识和知识版本数据模型，以及上传、编辑、删除、审批、索引和检索授权。
- 需要历史 `created_by` 与 `contribution_mode` 迁移策略及相应审计记录。
- 不放宽现有知识库管理权限，不要求未启用治理的历史数据补造 active 版本。
