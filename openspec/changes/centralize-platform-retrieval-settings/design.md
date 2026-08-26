## Context

系统已经存在单例 `PlatformSettings.RetrievalConfig`，并在读取租户时覆盖租户历史配置，但当前字段只覆盖 `EmbeddingTopK`、阈值、`RerankTopK` 和 rerank 模型。智能体另外保存 `VectorRecallTopK`、`KeywordRecallTopK`、`RRFVectorWeight`、`RerankCandidateTopK` 等检索策略并在问答初始化后覆盖全局值；Integration RAG 不经过智能体覆盖，只读取不完整的平台配置，并在 handler 末端使用独立 `top_k` 截断。知识搜索页仍可通过名为 tenant 的 KV API 编辑实际的平台单例配置，配置所有权和 UI 角色不一致。

本变更涉及平台配置模型、问答初始化、智能体兼容、Integration API 和多个前端入口。平台管理员是策略所有者；租户管理员、普通成员、智能体和外部调用方是策略消费者。

## Goals / Non-Goals

**Goals:**

- 用现有 `PlatformSettings.RetrievalConfig` 表达完整检索流水线，并成为唯一运行时真源。
- 让快速问答、智能推理、智能体、无摘要知识搜索以及 Integration 单次/批量搜索得到相同的有效策略。
- 明确区分召回总预算、融合候选、rerank 候选和最终输出上限。
- 仅允许平台管理员修改策略，关闭租户和智能体重复入口。
- 保留历史字段和请求 DTO 的反序列化兼容，支持无数据破坏回滚。

**Non-Goals:**

- 不把检索策略拆成按租户、按智能体、按接口或按知识库的覆盖层。
- 不改变知识库授权、文件夹过滤、阈值算法、RRF 算法或 rerank 模型实现。
- 不允许 Integration `top_k` 增大平台预算。
- 不在本变更删除数据库中的旧 JSON 字段。

## Decisions

### 1. 扩充现有平台 RetrievalConfig，而不是新增配置实体

平台配置增加 `EnableQueryExpansion`、`VectorRecallTopK`、`KeywordRecallTopK`、`RRFVectorWeight` 和 `RerankCandidateTopK`，并保留已有预算与阈值字段。`EnableQueryExpansion` 是所有入口的能力上限：关闭时路由不得重新启用；开启时仍由请求级 L1-L4 路由决定本次是否实际扩展。`RerankModelID` 不作为检索策略覆盖入口，运行时继续解析平台默认 rerank 模型，避免出现两个模型真源。完整阶段关系为：

```text
vector_recall_top_k + keyword_recall_top_k
  -> RRF/去重，最多 embedding_top_k
  -> rerank 输入最多 rerank_candidate_top_k
  -> 最终最多 rerank_top_k
```

所有 Top K 均表示整次查询跨全部选定知识库的总数，不乘知识库数量。选择现有单例可以复用权限、持久化和 `ApplyToTenant`；另建表或租户覆盖会重新引入多真源。

### 2. 建立单一有效配置解析函数

后端提供一个从 `PlatformSettings.RetrievalConfig` 生成完整、有默认值且已校验配置的解析边界。所有 ChatManage 初始化和无摘要搜索都使用该结果；智能体覆盖函数停止写入检索字段，只继续处理提示词、对话、工具和其他非检索配置。不得在各调用链复制默认值。

平台默认值为：查询扩展开启、`EmbeddingTopK=30`、两路 recall 各 `50`、`RRFVectorWeight=0.7`、向量阈值 `0.3`、关键词阈值 `0.3`、`RerankCandidateTopK=20`、`RerankTopK=10`、Rerank 阈值 `0.3`。校验必须保证：

- recall 值为 `1..500`；
- `1 <= RerankTopK <= RerankCandidateTopK <= EmbeddingTopK`；
- RRF 权重和阈值处于既有合法范围；
- 缺失字段补默认值，非法显式更新返回参数错误，不静默保存。

### 3. Integration top_k 仅缩小最终响应

Integration DTO 继续接受 `top_k`，维持已有客户端兼容。省略或为 0 时使用平台 `RerankTopK`；显式正值时有效上限为 `min(request.top_k, platform.RerankTopK)`。handler 不再维护独立 `INTEGRATION_DEFAULT_TOP_K` 作为检索策略，也不能通过 `INTEGRATION_MAX_TOP_K` 放大平台值；请求硬上限仍可作为安全校验存在。

单次与 batch 每个 query 使用相同函数计算响应上限，内部召回与 rerank 始终使用完整平台策略。

### 4. 关闭覆盖入口但保留兼容字段

- 平台设置新增“检索策略”导航，仅在 platform workspace 的 `platform_admin` 可见和可写。
- 同一页面以独立“回答兜底”分组维护 `ConversationConfig` 中的兜底策略、固定回复和模型提示词；该分组仅供快速问答使用，不并入 `RetrievalConfig`，也不影响智能推理和纯检索接口。
- `KnowledgeSearch.vue` 移除 `RetrievalSettings` 编辑入口。
- `AgentEditorModal.vue` 移除检索策略和回答兜底入口（包括查询扩展开关）、前端默认补齐和迁移逻辑；后端保存旧智能体 DTO 时仍允许旧字段存在，但运行时忽略检索与兜底覆盖。
- tenant KV `GET retrieval-config` 可继续返回平台有效配置供只读兼容；`PUT` 必须继续由后端校验 platform_admin，普通管理员不能通过直接 API 绕过 UI。

选择“隐藏 UI + 后端拒绝覆盖”而不是只隐藏 UI，是为了防止旧客户端或手工请求继续改变策略。

### 5. 迁移不采用租户或智能体值反推

平台已有字段保留；新增字段缺失时使用平台默认值并在平台管理员首次保存或启动补全流程中持久化。不同租户/智能体可能存在互相冲突的历史值，自动选取任意一个都会改变其他用户行为，因此不把历史局部值升级为平台策略。

## Risks / Trade-offs

- [旧智能体结果数量发生变化] → 上线前展示平台有效值并以现有主流默认值初始化；历史字段保留以便回滚。
- [Integration 客户端依赖大于平台上限的 top_k] → DTO 保持兼容，但响应明确受平台上限约束，并更新接口文档和契约测试。
- [配置热更新后的并发请求读取不同版本] → 每个请求开始时解析一次不可变快照，同一请求全程复用。
- [只关闭前端仍可调用旧 API] → 服务端写接口强制 platform_admin，消费者路径不读取租户或智能体覆盖。
- [统一配置降低个别租户调优能力] → 这是单一真源的明确取舍；本变更不添加例外层。

## Migration Plan

1. 扩充类型和平台 API，补齐默认值与校验测试；不改变消费者。
2. 切换所有消费者到统一解析器，并添加跨入口等价测试。
3. 调整 Integration `top_k` 语义和契约测试、文档。
4. 新增平台 UI 并验证角色权限，随后隐藏知识搜索和智能体入口。
5. 观察有效配置、召回数量和结果数量日志；确认后停止记录旧覆盖值的运行时使用。

回滚时恢复旧消费者读取逻辑即可；历史租户和智能体字段未删除，平台新增 JSON 字段也可被旧版本忽略。

## Open Questions

- 无。当前决策是平台管理员拥有唯一检索策略，租户管理员和 Integration 调用方只能缩小最终响应，不能覆盖流水线预算。
