## Why

其他项目需要在任意业务页面快速调用 WeKnora 问答能力，而不跳转到知识库管理页。需要提供技术栈无关、可销毁、权限受控的悬浮聊天组件，并复用稳定的 Integration API。

## What Changes

- 提供可通过 JS SDK 或 Web Component 初始化的悬浮聊天入口，聊天 UI 在隔离 iframe 中运行。
- 支持固定知识库、授权范围内多选和全部允许三种选择模式；前端选择只能缩小服务端授权范围。
- `fixed` 和 `selectable` 每轮传递非空显式选择，`all-allowed` 使用会话创建时冻结的授权上限；所有模式保存服务端确认的实际来源快照。
- 支持稳定 SSE envelope、断线续传、过期 cursor 的消息快照恢复、取消、错误恢复和同标签页会话保持。
- 支持位置、主色、标题、图标及浅色/深色模式等有限主题配置。
- 定义初始化、打开、关闭、销毁、未授权、回答完成和错误等宿主事件与生命周期。

## Capabilities

### New Capabilities

- `floating-chat-widget`: 定义悬浮聊天组件的挂载方式、知识库选择、会话行为、主题、事件和生命周期。

### Modified Capabilities

无。

## Impact

- 影响前端聊天页面、可分发 SDK/Web Component、iframe 通信、静态资源构建和宿主接入文档。
- **实施门禁**：只有 `add-integration-client-authz` 和 `add-integration-knowledge-api` 已完成实施并通过各自验收后，才能开始本 change；仅有 planning artifacts complete 不满足门禁。
- 不改变 WeKnora 现有独立聊天页面和内部会话接口。
