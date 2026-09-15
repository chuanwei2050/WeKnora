## ADDED Requirements

### Requirement: 解析中文档显示停止而非重建

当文档 `parse_status` 为 `pending` 或 `processing` 时，知识库文档列表操作列与卡片更多菜单 MUST 显示「停止」操作，且 MUST NOT 显示「重建」操作。当文档不在解析中时，MUST 继续按既有规则显示「重建」（审核中等既有例外保持不变）。

#### Scenario: 解析中显示停止

- **WHEN** 文档处于 `pending` 或 `processing`，且用户具备编辑权限
- **THEN** UI 展示「停止」操作，不展示「重建」操作

#### Scenario: 非解析中仍显示重建

- **WHEN** 文档已完成或已失败等非解析中状态，且按既有规则允许重建
- **THEN** UI 展示「重建」操作，不展示「停止」操作

### Requirement: 批量栏可停止解析中文档

当用户多选文档且选中集合中至少存在一篇 `pending` 或 `processing` 文档时，批量操作栏 MUST 提供「停止」操作。停止仅作用于选中集合中仍为解析中的文档。

#### Scenario: 选中含解析中文档时显示批量停止

- **WHEN** 用户选中至少一篇解析中文档
- **THEN** 批量栏展示「停止」操作

#### Scenario: 批量停止只影响解析中文档

- **WHEN** 用户批量停止，且选中集合同时包含解析中与已完成文档
- **THEN** 仅解析中文档被中断为失败；已完成文档保持不变

### Requirement: 停止前二次确认

用户点击单文档或批量「停止」后，系统 MUST 弹出确认对话框；仅在用户确认后才发起停止请求。取消确认 MUST NOT 改变文档状态或清理队列/Redis。

#### Scenario: 确认后才停止

- **WHEN** 用户点击「停止」并在确认框中确认
- **THEN** 前端调用对应的停止解析 API

#### Scenario: 取消确认不产生副作用

- **WHEN** 用户点击「停止」后取消确认
- **THEN** 文档状态与队列/Redis 保持不变

### Requirement: 单文档停止解析 API

系统 MUST 提供 `POST /knowledge/{id}/stop-parse`。调用方 MUST 具备与重新解析相当的编辑权限。仅当目标文档为 `pending` 或 `processing` 时，系统 MUST 将其标记为 `failed` 并写入用户停止中断文案；MUST NOT 删除该知识库文档记录。对非解析中文档的停止请求 MUST 被拒绝。

#### Scenario: 停止处理中文档

- **WHEN** 用户对 `processing` 文档成功调用停止解析
- **THEN** 该文档变为 `failed`，错误信息表明用户停止，文档记录仍保留

#### Scenario: 停止等待中文档

- **WHEN** 用户对 `pending` 文档成功调用停止解析
- **THEN** 该文档变为 `failed`，错误信息表明用户停止，文档记录仍保留

#### Scenario: 非解析中拒绝停止

- **WHEN** 用户对已 `completed` 或已 `failed` 的文档调用停止解析
- **THEN** 系统拒绝该请求且不修改文档状态

### Requirement: 批量停止解析 API

系统 MUST 提供批量停止解析 API，接受知识 ID 列表。调用方 MUST 具备编辑权限。系统 MUST 仅中断列表中仍为 `pending`/`processing` 的文档；MUST NOT 删除这些文档记录。若列表中无一可停止文档，系统 MUST 拒绝该请求。

#### Scenario: 批量停止多篇解析中文档

- **WHEN** 用户对多篇 `pending`/`processing` 文档调用批量停止
- **THEN** 这些文档均变为 `failed`（用户停止文案），记录仍保留

#### Scenario: 批量请求全无可停止目标

- **WHEN** 批量停止请求中的 ID 均非解析中
- **THEN** 系统拒绝该请求且不修改任何文档

### Requirement: 停止时清理解析产物与任务（保留原文档）

单文档与批量停止的成功路径 MUST 在中断解析后清理该文档的解析派生产物与在途任务，且 MUST NOT 删除知识库文档记录及其源文件（`FilePath`）。清理范围 MUST 至少包括：

1. asynq：`document:process` / `image:multimodal` / `knowledge:post_process`（payload 命中目标 ID）
2. Redis：`multimodal:pending:{id}` 与 `multimodal:pending:{id}:*`
3. 派生数据：分块、向量索引、知识图谱、分块提取图片、生成预览文件
4. wiki（若启用）：清理 pending ingest，避免残留任务命中空分块

MUST NOT 清理 wiki/数据源同步/会话等无关业务队列。单文档与批量 MUST 走同一清理编排。

#### Scenario: 单文档停止清理产物与任务

- **WHEN** 用户成功停止单篇解析中文档
- **THEN** 系统删除该文档对应的管线 asynq 任务、`multimodal:pending:*`、分块/向量/图谱等派生产物，知识行与源文件仍保留且状态为失败中断

#### Scenario: 批量停止对每篇目标同等清理

- **WHEN** 用户成功批量停止多篇解析中文档
- **THEN** 系统对每一被中断文档执行与单文档相同的产物与任务清理

#### Scenario: 不误伤其他业务队列

- **WHEN** 系统执行单文档或批量停止清理
- **THEN** wiki 内容页数据本身不被整库删除；数据源同步等非文档管线 asynq 任务不被删除

### Requirement: 残留 worker 不得复活已停止文档

当文档 `parse_status` 为 `failed` 且错误信息为单文档用户停止中断文案时，文档处理相关 worker（含 `image:multimodal` / post_process）MUST 跳过该任务，MUST NOT 将其状态改回 `processing`。

#### Scenario: 停止后残留任务跳过

- **WHEN** 队列中仍有针对某文档的处理任务（含 `image:multimodal`），且该文档已因用户停止被标记为失败中断
- **THEN** worker 跳过处理并保持该文档为失败中断状态
