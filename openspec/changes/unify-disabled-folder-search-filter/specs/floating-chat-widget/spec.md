## ADDED Requirements

### Requirement: 悬浮窗显式提交文件夹过滤策略

悬浮窗在需要保持“关闭搜索的文件夹不进入对话检索”的产品行为时 MUST 在知识问答请求中显式提交 `filter_disabled_folders=true`，不得依赖服务端对字段缺失的隐式过滤。

#### Scenario: 悬浮窗发起知识问答
- **WHEN** 用户通过悬浮窗发送需要知识库检索的消息
- **THEN** 悬浮窗请求显式携带 `filter_disabled_folders=true`
- **AND** 服务端排除关闭搜索的文件夹及其后代文件

#### Scenario: 服务端缺省语义变化
- **WHEN** 服务端将字段缺失解释为不过滤
- **THEN** 已更新的悬浮窗仍保持不可搜索文件夹不进入检索的既有体验
