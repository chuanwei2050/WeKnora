## ADDED Requirements

### Requirement: Widget 必须可由异构宿主独立挂载
系统 MUST 提供不要求宿主共享 Vue、Router、Pinia 或 UI 组件库的 JS SDK 或 Web Component，并 MUST 通过隔离 iframe 加载聊天主体。

#### Scenario: 非 Vue 宿主初始化 Widget
- **WHEN** 宿主使用文档规定的脚本和配置调用 `init`
- **THEN** 页面出现悬浮入口，且聊天 iframe 能独立完成初始化

### Requirement: Widget 必须提供明确生命周期
SDK MUST 提供 `init`、`open`、`close` 和 `destroy`，初始化 MUST 对实例 ID 幂等，`destroy` MUST 清理 DOM、事件监听、定时器、SSE 和 iframe。

#### Scenario: 重复初始化相同实例
- **WHEN** 宿主以相同实例 ID 和兼容配置重复调用 `init`
- **THEN** SDK 返回现有实例而不创建第二个按钮、iframe 或流连接

#### Scenario: 销毁 Widget
- **WHEN** 宿主调用实例的 `destroy`
- **THEN** 所有实例资源被释放，后续页面事件不再触发该实例行为

### Requirement: Widget 必须使用一次性票据认证
Widget MUST 通过宿主后端取得 bootstrap ticket 并经 iframe 消息协议兑换嵌入会话，MUST NOT 在浏览器配置中接收 client secret、service token 或长期用户 token。

#### Scenario: Widget 首次打开并认证
- **WHEN** 已登录宿主用户首次打开 Widget
- **THEN** 宿主后端签发一次性 ticket，iframe 兑换后加载当前用户授权的知识库

### Requirement: Widget 必须支持三种知识库选择模式
Widget MUST 支持 `fixed`、`selectable` 和 `all-allowed`；`fixed` 和 `selectable` MUST 创建 `selected` 会话并使用非空知识库数组，`all-allowed` MUST 创建显式 `all-allowed` 会话且不得提交知识库数组。所有配置知识库和用户选择 MUST 位于服务端授权范围内，客户端 MUST NOT 将缺失、null 或空数组解释为全部，也 MUST NOT 扩大服务端范围。

#### Scenario: 固定知识库模式
- **WHEN** 宿主配置 `fixed` 和一个授权知识库子集
- **THEN** Widget 不允许用户选择集合外知识库，所有消息只请求该子集

#### Scenario: 多选模式
- **WHEN** 宿主配置 `selectable`
- **THEN** Widget 显示当前授权列表并允许用户选择一个或多个不超过服务端上限的知识库

#### Scenario: 多选模式没有选择知识库
- **WHEN** 用户尝试以缺失、null 或空选择发送消息
- **THEN** Widget 阻止发送并要求至少选择一个知识库，不自动切换为全部授权范围

#### Scenario: 全部允许模式
- **WHEN** 宿主显式配置 `all-allowed`
- **THEN** Widget 创建不带知识库数组的 all-allowed 会话，使用服务端在创建时冻结的授权上限且不因新增授权自动扩大

#### Scenario: 配置包含越权知识库
- **WHEN** 宿主初始化配置包含用户或 client 未授权的知识库
- **THEN** Widget 不显示越权知识库且服务端拒绝任何绕过 UI 的越权请求

### Requirement: Widget 必须逐轮提交实际知识库选择
`fixed` 和 `selectable` 的每条消息 MUST 携带非空 `selected_knowledge_base_ids`；`all-allowed` 的消息 MUST 省略该字段。所有模式 MUST 在消息历史中保留服务端确认的实际知识库来源快照。

#### Scenario: 会话中调整知识库
- **WHEN** 用户在下一轮回答前调整授权知识库选择
- **THEN** 下一条消息使用新选择，旧消息仍显示各自原有来源快照

#### Scenario: 已选知识库权限被撤销
- **WHEN** 发送消息时服务端返回知识库权限 `403`
- **THEN** Widget 不展示部分回答，刷新授权列表并提示用户重新选择

#### Scenario: all-allowed 会话发送消息
- **WHEN** Widget 在 all-allowed 会话中发送消息
- **THEN** Widget 省略 `selected_knowledge_base_ids` 并显示服务端返回的实际来源快照

### Requirement: Widget 必须支持可恢复的流式回答
Widget MUST 使用 Integration API 的 `Idempotency-Key` 和稳定 SSE envelope，MUST 在临时断线后从最后确认事件续接；cursor 过期收到 `410 Gone` 时 MUST 通过 `message_snapshot_url` 查询消息快照且 MUST NOT 重发消息，并 MUST 支持取消生成中的消息。

#### Scenario: 流式回答中断后恢复
- **WHEN** 网络暂时断开后恢复且服务端事件仍在保留期内
- **THEN** Widget 从最后确认 `event_id` 后继续显示，不重复提交消息或重复渲染已确认片段

#### Scenario: SSE cursor 已过期
- **WHEN** 续接接口返回 `410 Gone` 和 `message_snapshot_url`
- **THEN** Widget 查询消息快照并恢复 completed、failed 或 cancelled 终态，不重新提交用户消息

#### Scenario: 用户取消回答
- **WHEN** 用户对生成中的消息执行取消
- **THEN** Widget 调用消息 cancel 接口并展示服务端确认的取消终态

### Requirement: Widget 必须提供有限且安全的主题配置
Widget MUST 只接受位置、主色、标题、图标和浅色/深色模式等版本化主题字段，并 MUST 验证所有配置值；V1 MUST NOT 接受任意 HTML 或 CSS 注入。

#### Scenario: 宿主设置品牌主题
- **WHEN** 宿主提供合法主色、标题、图标和位置
- **THEN** 悬浮入口和聊天外壳应用配置，同时保持内容可读和交互可用

### Requirement: Widget 必须发布稳定宿主事件
Widget MUST 提供 `ready`、`open`、`close`、`unauthorized`、`answer-completed` 和 `error` 事件，并 MUST 校验 iframe 消息的 origin、类型和负载结构。

#### Scenario: 回答完成通知宿主
- **WHEN** 服务端发出 `answer.completed` 且 Widget 完成渲染
- **THEN** SDK 向宿主触发结构化 `answer-completed`，不暴露 token 或内部 pipeline 数据

#### Scenario: 收到伪造 iframe 消息
- **WHEN** SDK 收到非允许 origin 或 schema 不合法的消息
- **THEN** SDK 忽略消息且不触发宿主事件或改变会话状态

### Requirement: Widget 模式必须限制功能范围
`embedded-widget` MUST 只提供聊天相关页面和操作，MUST NOT 允许导航到知识库管理、文档审批、组织、租户或系统设置。

#### Scenario: 用户构造管理页路由
- **WHEN** Widget iframe 内尝试打开管理或系统设置路由
- **THEN** 前端阻止导航，后端仍对对应资源执行独立授权校验

### Requirement: Widget 不得影响独立聊天模式
未加载 SDK 或未启用 `embedded-widget` 时，WeKnora 原有独立聊天页面和内部会话接口 MUST 保持现有行为。

#### Scenario: 独立部署不引用 Widget
- **WHEN** 用户通过 WeKnora standalone 入口使用聊天
- **THEN** 系统无需 Widget 配置即可完成原有会话和流式回答
