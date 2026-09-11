## ADDED Requirements

### Requirement: 驳回必须填写意见（行内审批）
系统 MUST 在文档列表等现有审批入口执行 reject 时要求非空审核意见；空意见 MUST 被拒绝且不改变版本状态。系统 MUST NOT 因此新增跨知识库审核工作台。

#### Scenario: 无意见驳回失败
- **WHEN** 审核员在知识库文档列表对 pending_review 版本执行 reject 且 comment 为空或仅空白
- **THEN** 系统返回校验错误，版本保持 `pending_review`

#### Scenario: 有意见驳回成功
- **WHEN** 审核员提交 reject 且 comment 非空
- **THEN** 系统将版本转为 rejected 并记录意见
