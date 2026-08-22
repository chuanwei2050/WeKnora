## Context

宿主项目希望在任意业务页面显示悬浮聊天入口，并从授权知识库中选择一个或多个作为回答来源。宿主技术栈可能不同，因此外层接入必须轻量、稳定；聊天 UI 和复杂依赖则应继续由 WeKnora 独立发布。本变更复用 integration auth 和 chat API，不复制检索或会话逻辑。

## Goals / Non-Goals

**Goals:**

- 提供技术栈无关的初始化、打开、关闭和销毁接口。
- 通过隔离 iframe 承载聊天 UI，并支持有限主题与宿主事件。
- 支持固定、多选和全部允许三种知识库模式，且不能扩大服务端授权。
- 处理票据登录、流式回答、重连、取消、切页和资源清理。
- 支持鼠标、触控和键盘可操作的移动、缩放、最大化与还原。

**Non-Goals:**

- Widget 不提供知识库管理、投稿、审批、租户切换或系统设置。
- V1 不允许宿主任意注入 HTML/CSS 或替换内部聊天组件。
- 不在 SDK 中保存 client secret、service token 或长期用户 token。
- 不复制现有聊天视图、输入框、消息渲染或会话状态机。

## Decisions

### 通过薄外壳直接复用 ChatView

现有 standalone 聊天页和 Wiki 修复抽屉已经使用同一 `ChatView`；它虽然混合 Router、Pinia、`localStorage` 和页面外壳职责，但为本变更整体拆分约 6 万行组件会扩大回归面。先修复嵌入初始化中 `embeddedAgentId`/`embeddedKbIds` 与公开 `agentId`/`kbIds` 属性不一致，再让 `embedded-widget` 用薄适配外壳直接传入显式 props 并切换 Cookie 请求模式。只有未来出现独立复用边界时再单独提出 `ChatPanel` 重构；本变更继续复用现有 `Input-field`、消息渲染和会话能力。

### 薄 SDK/Web Component 加隔离 iframe

分发层只负责创建悬浮按钮、容器和 iframe，公开稳定生命周期 API；聊天页面由 WeKnora `embedded-widget` 模式提供。相比直接发布 Vue 组件，该方案不要求宿主共享 Vue、Pinia、Router 或 TDesign，也降低样式与依赖冲突。

iframe 使用满足聊天所需的最小 `sandbox`/`allow` 权限和固定受信 URL；SDK 不接受 `javascript:`、`data:` 或任意 HTML 作为 iframe、标题或图标来源。宿主 origin 与 iframe origin 在初始化时规范化并固定，后续消息不得自行改变信任来源。

### 知识库选择只缩小授权范围

初始化支持 `fixed`、`selectable`、`all-allowed`：`fixed` 固定为非空配置子集，`selectable` 显示授权列表并要求用户至少选择一个，二者创建 `selected` 会话并在每轮提交非空 `selected_knowledge_base_ids`；`all-allowed` 创建显式 `all-allowed` 会话，不提交知识库数组，使用会话创建时冻结的授权上限与当前实时权限交集，不因后续新增授权自动扩大。缺失、null 或空数组不代表全部。

SDK 配置使用以 `mode` 为判别字段的联合类型：`fixed` 必须带非空 `knowledgeBaseIds`，`selectable` 可带非空初始选择，`all-allowed` 禁止出现知识库数组。解析外部 JSON 后才进入内部类型，禁止以类型断言掩盖非法组合。

### 每轮提交实际选择

在 `fixed` 和 `selectable` 模式中，Widget 每轮发送非空 `selected_knowledge_base_ids`；在 `all-allowed` 模式中省略该字段，由服务端按冻结上限计算。服务端返回 `400` 时 Widget 修正本地选择状态，返回 `403` 时不做部分回答并刷新授权列表；消息历史始终显示服务端确认的该轮来源快照。

### 生命周期与页面导航解耦

SDK 提供 `init`、`open`、`close`、`destroy`。默认在同一浏览器标签页内保持会话，宿主可选择在路由切换时保留或销毁。`destroy` 必须移除 DOM、监听器、定时器、SSE 和 iframe；重复初始化使用实例 ID 隔离。

### 外壳拥有可移动、可缩放布局

拖动与缩放属于 SDK 外壳，不进入 iframe 聊天业务组件。入口和面板都可通过 Pointer Events 拖动；面板提供最小/最大尺寸约束、右下角缩放柄、最大化和还原。布局状态使用可判别联合区分 `normal` 与 `maximized`，公开 `moveTo`、`resizeTo`、`maximize`、`restore`、`resetLayout`。所有坐标基于 `visualViewport` 夹取到可见区域，viewport 变化后自动纠正；普通布局只在当前标签页使用 `sessionStorage` 按实例 ID 保存。标题栏、缩放柄和按钮必须提供键盘等价操作与可访问名称。

`normal` 状态必须同时拥有位置与尺寸，`maximized` 状态只保存还原用的合法 `normal` 快照；任何来自配置、存储或 `postMessage` 的值先做运行时解析，不让 `NaN`、负尺寸、未知枚举或多余权限字段进入布局状态。

### 主题和事件采用有限契约

V1 仅支持初始位置、初始尺寸、主色、标题、图标、浅色/深色。事件包括 `ready`、`open`、`close`、`layout-changed`、`unauthorized`、`answer-completed`、`error`；所有跨 iframe 消息执行 origin 与 schema 校验。

## Risks / Trade-offs

- [第三方页面 CSS 覆盖悬浮按钮] → SDK 使用命名空间或 Shadow DOM 保护外壳，聊天主体继续放在 iframe。
- [页面切换造成重复实例或连接泄漏] → 强制实例 ID、幂等初始化和可验证 `destroy` 清理。
- [多选知识库增加延迟] → 使用 Integration API 配置的服务端上限，Widget 不硬编码未经压测的数量。
- [断网重连产生重复回答] → 复用消息 `Idempotency-Key`、SSE `Last-Event-ID` 和消息快照查询；cursor 过期收到 `410` 后读取快照，不重发消息。

## Migration Plan

0. 确认认证授权和 Integration API 两个前置 change 已完成实施及验收；未满足时停止本 change。
1. 实现 `embedded-widget` 页面并只连接测试环境的 Integration API。
2. 构建薄 SDK/Web Component，完成初始化、认证、通信和销毁测试。
3. 增加三种选库模式、主题、稳定事件、SSE 续接、`410` 快照恢复与取消。
4. 在不同技术栈宿主中进行真实浏览器验收后发布版本化静态资源。
5. 回滚时宿主移除脚本和初始化代码；WeKnora 独立聊天页面不受影响。

## Open Questions

无阻塞性问题。Widget 包格式和静态资源发布位置由实现阶段结合现有构建链确定，但公开初始化配置、事件和销毁行为必须保持一致。
