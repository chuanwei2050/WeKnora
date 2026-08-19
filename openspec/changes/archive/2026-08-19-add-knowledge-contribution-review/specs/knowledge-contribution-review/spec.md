## ADDED Requirements

### Requirement: 知识库必须定义投稿策略
系统 MUST 为每个知识库保存 `contribution_mode`，其值只能是 `closed`、`members` 或 `allowlist`；新建请求缺省和历史迁移值 MUST 为 `closed`。

#### Scenario: 新建知识库未指定投稿策略
- **WHEN** 管理员创建知识库但未提供 `contribution_mode`
- **THEN** 系统将其保存为 `closed`

#### Scenario: 成员向关闭投稿的知识库上传
- **WHEN** 普通成员向 `closed` 知识库提交文档
- **THEN** 系统拒绝请求且不创建文档或版本

### Requirement: 成员投稿必须满足策略和治理条件
系统 MUST 仅在用户属于同一租户、`contribution_mode` 允许该用户且知识库已启用治理与审批时授予普通成员投稿权限。

#### Scenario: 同租户成员向 members 知识库投稿
- **WHEN** 同租户普通成员向启用治理的 `members` 知识库上传文档
- **THEN** 系统创建归属于该成员的未发布草稿

#### Scenario: 未启用治理时成员投稿
- **WHEN** `members` 或 `allowlist` 知识库未启用治理与审批
- **THEN** 系统拒绝普通成员投稿，但不影响管理员的现有直接上传流程

### Requirement: 文档和版本必须记录贡献者
系统 MUST 在文档上记录最初贡献者，在每个版本上记录该版本提交者，并 MUST 使用这些字段执行 draft 和 rejected 内容的编辑和删除授权。

#### Scenario: 贡献者编辑自己的草稿
- **WHEN** 用户编辑由自己创建且处于 draft 或 rejected 状态的文档或版本
- **THEN** 系统允许编辑并保留贡献者归属

#### Scenario: 成员编辑他人内容
- **WHEN** 普通成员尝试编辑或删除其他用户创建的文档或版本
- **THEN** 系统拒绝操作，即使双方属于同一租户

### Requirement: 已提交版本必须冻结并显式撤回
系统 MUST 禁止直接修改 pending_review、approved、indexing 和 active 版本；系统 MUST 允许 pending_review 的提交者在审核产生决定前执行显式 withdraw，将该版本原子转换回 draft，并 MUST 记录撤回审计。withdraw 与审核决定并发时，系统 MUST 只允许一个状态转换成功。

#### Scenario: 提交者修改待审版本
- **WHEN** 提交者尝试直接编辑或删除 pending_review 版本内容
- **THEN** 系统拒绝修改并提示先执行 withdraw

#### Scenario: 提交者撤回待审版本
- **WHEN** 提交者在审核决定前撤回自己的 pending_review 版本
- **THEN** 系统将版本转换为 draft、记录撤回人和时间，并允许后续编辑与重新提交

#### Scenario: 审核完成后尝试撤回
- **WHEN** 提交者尝试撤回已进入 approved、indexing、active 或 rejected 的版本
- **THEN** 系统拒绝撤回并保持当前状态

#### Scenario: 撤回与审核并发
- **WHEN** 提交者撤回和审核员审批同一 pending_review 版本发生竞争
- **THEN** 系统只提交一个合法状态转换，失败操作收到冲突响应且不能覆盖成功结果

### Requirement: 投稿、审核和管理权限必须分离
系统 MUST 分别校验贡献自己的内容、审核知识库内容和管理知识库的权限；审核员授权 MUST 作用于明确知识库，提交人 MUST NOT 审核自己的提交。

#### Scenario: 审核员审批他人提交
- **WHEN** 知识库级审核员审批其他用户的 pending_review 版本
- **THEN** 系统允许审批并记录审核人、时间、结论和意见

#### Scenario: 提交人尝试自审
- **WHEN** 版本提交人同时拥有审核员或管理员身份并尝试审批自己的版本
- **THEN** 系统拒绝审批且版本状态保持不变

### Requirement: 治理文档必须遵循审批发布状态流
系统 MUST 支持 `draft → pending_review → approved → indexing → active`、`pending_review → rejected` 和 `pending_review → draft` 撤回状态流，并 MUST 在审批通过后自动索引及发布。

#### Scenario: 首次投稿成功发布
- **WHEN** 审核员批准版本且索引成功
- **THEN** 系统将版本设为 active、更新 `current_version_id` 并使其可被检索

#### Scenario: 投稿被驳回后重提
- **WHEN** 审核员驳回版本且原提交者修订后再次提交
- **THEN** 系统将修订版本重新置为 pending_review，并保留上一轮审核记录

### Requirement: 新版本发布必须原子切换
系统 MUST 在新版本审批和索引成功前持续提供旧 active 版本，并 MUST 原子更新 active 状态和 `current_version_id`。

#### Scenario: 新版本索引成功
- **WHEN** active 文档的新版本完成审批和索引
- **THEN** 系统在单个一致性操作中激活新版本并停用旧版本

#### Scenario: 新版本索引失败
- **WHEN** 新版本审批通过但索引失败
- **THEN** 系统保留旧 active 版本可检索，并将失败状态暴露给有权限的维护者

### Requirement: 未发布治理内容不得进入正式检索
系统 MUST 仅检索治理知识当前 active 且处于有效期内的版本，并 MUST 排除 draft、pending_review、approved、indexing、rejected、失效或未来生效版本。

#### Scenario: 搜索包含待审内容的知识库
- **WHEN** RAG 或聊天检索治理知识库
- **THEN** 结果只可能来自当前有效 active 版本

### Requirement: 历史与非治理知识必须保持兼容
系统 MUST 对未启用治理或 `current_version_id` 为空的历史知识继续使用现有 `parse_status`、`enable_status` 和索引可见性规则，MUST NOT 强制补造 active 版本。

#### Scenario: 升级后搜索历史知识库
- **WHEN** 历史知识库未启用治理且原知识处于现有可检索状态
- **THEN** 系统继续返回该知识，不因缺少 active 版本而隐藏结果

### Requirement: 历史所有权迁移必须可审计
系统 MUST 将历史知识库的 `contribution_mode` 回填为 `closed`；无法确定创建者的历史文档 MUST 归属租户管理员并生成迁移审计记录。

#### Scenario: 历史文档缺失创建者
- **WHEN** 迁移发现文档没有可恢复的贡献者
- **THEN** 系统将其归属指定租户管理员，并记录原记录 ID、回填主体和迁移时间
