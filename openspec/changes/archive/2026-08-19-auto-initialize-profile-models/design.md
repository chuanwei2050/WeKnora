## Context

`internal/modelprofile` 已能解析活动 `MODEL_PROFILE` 并按角色读取环境变量，但只输出只读状态。模型登记由初始化页面请求驱动，未覆盖 Verifier、Evaluation Judge、ASR、TTS，也不会读取 Profile。模型仓储和依赖注入容器已经在数据库迁移完成后可用，适合在服务启动期间执行一次幂等引导。

## Goals / Non-Goals

**Goals:**

- 在 HTTP 服务对外提供请求前，将活动 Profile 中的有效模型登记为普通可编辑默认模型。
- 支持 Chat、Verifier、Evaluation Judge、Embedding、Rerank、VLM、ASR、TTS。
- 仅在持久化播种版本尚未完成时读取 online/offline 两套 env；播种后管理员修改模型不被 env 覆盖。
- `MODEL_PROFILE` 只初始化持久化活动 Profile；之后由设置页切换控制运行时。
- 删除模型设置页的 Profile 清单和内置模型提示，以在线/离线切换和按 Profile 过滤替代。

**Non-Goals:**

- 不在启动过程中探测或下载模型。
- 不自动修改已有知识库的模型绑定。
- 不删除管理员已有的模型登记。
- 不把 `remote` 等同于公网；端点位置根据 Base URL 推导。

## Decisions

1. **在依赖注入容器完成模型仓储注册后同步执行引导。** 与页面懒初始化相比，这能保证设置页首次打开时数据已经存在；同步失败会阻止服务以半初始化状态启动。
2. **把环境变量解析和登记计划放在 `internal/modelprofile`。** 该包已经拥有 Profile、角色和环境变量命名知识；登记执行仍通过 `ModelRepository`，不绕过数据库模型类型。
3. **首次播种仍以 `(Name, Type, normalized BaseURL)` 作为复用键。** 仅按名称复用可能把公网模型误当成内网模型；包含 API Key 会泄漏或因密钥轮换产生重复记录，因此不进入复用键。
4. **每个逻辑角色独立登记。** 即使 `verifier_2` 与 Chat 共用名称和端点，也保留不同 `profile_role`；`qwen3.5-9b` 作为 `verifier_1` 显示在校验模型分组，而不是对话模型。裁判复用能力更强的主模型。
5. **在 `platform_settings` 保存播种版本与活动 Profile。** 活动 Profile 为空时用 `MODEL_PROFILE` 初始化；UI 切换后以数据库值为准。
6. **模型记录保存 `profile` 与 `profile_role`。** 两套配置允许同名同类型模型共存；Verifier 1/2 通过角色区分。
7. **运行时按活动 Profile 和角色解析。** 已有知识库保存的模型 ID 作为逻辑角色入口；若其 Profile 非活动 Profile，则解析同角色的活动模型，缺失时明确失败而不回退公网。
8. **Embedding 切换要求维度一致。** online/offline Embedding 维度不同则拒绝切换，避免向现有索引发送维度不兼容的查询向量。
9. **种子模型使用 `is_builtin=false`。** 已由旧实现创建且与当前种子等价的内置记录，在升级播种时转成普通模型。
10. **无效或占位配置按角色跳过。** 启动引导不写入不可用记录。
11. **offline env 是批准端点的信任边界。** 引导仅为 env 明确声明的远程目标创建或复用精确批准记录，并把用途、模型角色合并到该记录；运行时继续执行目标匹配和 SSRF allowlist 校验。

## Risks / Trade-offs

- [配置的端点尚未启动] → 引导只登记不探测，服务可启动；管理员通过现有连接测试确认可用性。
- [播种后 Profile 配置变更不生效] → 这是种子语义的预期结果；管理员在模型设置页修改服务器和凭据。
- [多实例同时首次启动产生重复] → 先查后建只能降低概率；本次不新增数据库唯一约束，以避免阻断现有重复数据。模型服务现有等价登记检查作为应用层补充。
- [公网主模型与 offline Profile 并存] → 忠实登记配置并标记为 `public`；启用严格离线时仍由现有门禁拒绝公网依赖。

## Migration Plan

1. 部署包含引导逻辑的新版本并保留现有模型数据。
2. 首次启动自动播种 online/offline 两套有效模型，并把旧的无 Profile 模型归入初始活动 Profile。
3. 在设置页确认种子模型可编辑，并按需运行连接测试。
4. 回滚代码不会删除已创建记录；这些记录仍可被现有模型管理和知识库引用。

## Open Questions

无。
