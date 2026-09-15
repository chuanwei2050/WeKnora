## ADDED Requirements

### Requirement: 停止文档管线维护时立即中断在途目标文档

系统在用户停止 `reparse` 或 `rechunk` 维护任务时，MUST 将本轮 `TargetKnowledgeIDs` 中仍处于 `pending` 或 `processing` 的文档标记为 `failed`，并写入用户停止中断文案；MUST NOT 删除这些知识库文档记录。已是 `completed` 或 `failed` 的目标文档 MUST 保持不变。

#### Scenario: 停止全部重建时中断处理中与等待中的目标文档

- **WHEN** 用户对正在 `running` 的 `reparse` 维护任务请求停止
- **THEN** 本轮目标中 `pending`/`processing` 的文档变为 `failed` 且错误信息表明用户停止重建，文档记录仍保留在知识库中

#### Scenario: 已完成目标文档不受停止影响

- **WHEN** 用户停止维护时部分目标文档已是 `completed`
- **THEN** 这些已完成文档的解析状态保持 `completed`

### Requirement: 停止后立即释放维护锁

对文档管线维护（`reparse` / `rechunk`），停止成功后系统 MUST 将维护进度状态设为 `canceled` 并释放维护锁，MUST NOT 为等待在途文档自然收尾而长时间保持 `canceling` 锁定。非文档管线维护的立即取消行为 MUST 保持不变。

#### Scenario: 停止后可立刻启动其他维护

- **WHEN** 用户成功停止正在运行的 `reparse` 或 `rechunk` 任务
- **THEN** 维护状态为 `canceled`，且用户可以立即启动下一次维护操作

#### Scenario: 非文档管线停止语义不变

- **WHEN** 用户停止正在运行的 `vector`（或同类非文档管线）维护任务
- **THEN** 系统仍立即将状态设为 `canceled` 并释放锁

### Requirement: 残留 worker 不得复活已中断文档

当文档 `parse_status` 为 `failed` 且错误信息为用户停止中断文案或全部重建中断文案时，`ProcessDocument`（及同等文档处理入口）MUST 跳过该任务，MUST NOT 将其状态改回 `processing`。

#### Scenario: 用户停止后残留任务跳过

- **WHEN** 队列中仍有针对某文档的处理任务，且该文档已因用户停止被标记为失败中断
- **THEN** worker 跳过处理并保持该文档为失败中断状态

#### Scenario: 全部重建中断文案同样跳过

- **WHEN** 文档因全部重建中断文案处于 `failed`
- **THEN** 残留 `ProcessDocument` 任务跳过且不写回 `processing`

### Requirement: 停止成功的用户提示

前端在文档管线维护停止成功后，MUST 提示用户在途文档已中断且维护已解锁（或语义等价文案），MUST NOT 再将「等待在途收尾」作为主成功路径的默认说明。

#### Scenario: 停止成功提示

- **WHEN** 用户点击停止且后端返回已取消的维护进度
- **THEN** UI 展示停止成功提示，表明在途已中断且可进行其他维护
