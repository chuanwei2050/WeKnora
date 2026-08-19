## 1. 前置接口与组件外壳

- [x] 1.1 执行实施门禁：核验认证授权和 Integration API change 已完成代码实施及验收；任一未满足时停止本 change
- [x] 1.2 定义版本化 SDK/Web Component 初始化配置、实例 API、事件和错误类型，以判别联合表达知识库模式和布局状态，并在边界解析外部值
- [x] 1.3 实现命名空间或 Shadow DOM 保护的悬浮按钮和 iframe 容器
- [x] 1.4 实现实例 ID 幂等初始化以及 `open`、`close`、`destroy` 生命周期
- [x] 1.5 修复现有 `ChatView` 嵌入属性漂移，以薄适配外壳让 standalone、Wiki 修复抽屉和 Widget 直接复用同一组件，不做大规模聊天组件拆分
- [x] 1.6 实现 `moveTo`、`resizeTo`、`maximize`、`restore`、`resetLayout` 和 `layout-changed` 契约

## 2. Widget 页面与认证

- [x] 2.1 增加仅暴露聊天功能的 `embedded-widget` 页面和路由守卫
- [x] 2.2 实现 SDK、宿主后端与 iframe 之间的一次性 ticket 登录引导
- [x] 2.3 校验 iframe 消息 origin、类型和 schema，并发布稳定宿主事件
- [x] 2.4 实现位置、主色、标题、图标和浅色/深色的受控主题配置
- [x] 2.5 实现入口与面板 Pointer Events 拖动、缩放柄、最大化/还原、键盘等价操作和可访问名称
- [x] 2.6 实现 `visualViewport` 边界夹取、viewport 变化纠正以及按实例 ID 的 `sessionStorage` 普通布局保持
- [x] 2.7 为 iframe 设置最小 sandbox/allow 权限，拒绝危险 URL 与运行时来源变更，并覆盖凭证不进入 URL、存储和日志的测试

## 3. 选库与流式会话

- [x] 3.1 实现 fixed/selectable 到 selected 会话、非空选择校验，以及 all-allowed 显式会话和冻结授权上限语义
- [x] 3.2 在 fixed/selectable 每轮发送非空选择，在 all-allowed 省略选择字段，并展示服务端确认的来源快照
- [x] 3.3 实现 `400` 选择纠正、`403` 授权列表刷新、失效选择清理和禁止部分回答
- [x] 3.4 接入 `Idempotency-Key`、稳定 SSE envelope、事件续接、`410` 消息快照恢复、回答取消和三种终态
- [x] 3.5 实现同标签页切页时可配置的会话保留或销毁行为

## 4. 构建、兼容与验收

- [x] 4.1 产出可版本化发布的 SDK/Web Component 和聊天 iframe 静态资源
- [ ] 4.2 在 Vue、React 和原生页面宿主中验证挂载、主题、移动、缩放、最大化/还原、选库、聊天、重连和销毁
- [ ] 4.3 覆盖重复初始化、多个实例、路由切换、空选库、all-allowed 上限不扩张、断网、过期 cursor、撤权、伪造消息和资源泄漏测试
- [x] 4.4 验证未加载 Widget 时 WeKnora standalone 聊天及内部会话接口无回归
- [x] 4.5 编写宿主接入、认证回调、配置、事件、错误处理和卸载文档
