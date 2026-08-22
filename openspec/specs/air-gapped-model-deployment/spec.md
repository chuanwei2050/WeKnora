# air-gapped-model-deployment Specification

## Purpose
TBD - created by archiving change add-air-gapped-model-deployment. Update Purpose after archive.
## Requirements
### Requirement: 离线验收复用线上冻结套件
三种离线 profile MUST 使用与线上基线相同的冻结套件、提示、阈值、模型身份和评估口径，并通过同一端到端执行器验证适用的文档入库、检索、复杂度路由、图谱版本可见性、验证、语音和性能能力。

#### Scenario: 离线 profile 完成适用验收
- **WHEN** `desktop-lite`、`compose-airgap` 或 `helm-airgap` 在无公网环境启动正式验收
- **THEN** 系统使用线上冻结套件执行适用案例和性能门禁
- **AND** 报告保留 profile、组件位置、模型身份、出站审计和每个案例结果

### Requirement: 离线 profile 结果独立判定
离线验收 MUST 分别计算 `single-node` 与 `server-load` 门禁，不得用一个 profile 的通过结果替代另一个；使用 `private-network` 必需组件的运行 MUST 将 `single-node` 标记为失败并列出位置不符项。

#### Scenario: 内网模型不在同机
- **WHEN** `desktop-lite` 使用批准但位置为 `private-network` 的必需模型
- **THEN** 适用功能和质量结果可以保存
- **AND** `single-node` 门禁失败且报告列出该模型端点

