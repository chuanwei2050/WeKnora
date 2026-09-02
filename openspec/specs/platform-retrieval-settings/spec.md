# platform-retrieval-settings Specification

## Purpose
TBD - created by archiving change centralize-platform-retrieval-settings. Update Purpose after archive.
## Requirements
### Requirement: 平台检索策略必须是唯一运行时真源
系统 MUST 在 `PlatformSettings.RetrievalConfig` 保存完整检索策略，并 MUST 让快速问答、智能推理、智能体、无摘要知识搜索、Integration 单次搜索和 Integration 批量搜索使用同一份请求级有效配置。租户和智能体历史检索字段 MUST NOT 覆盖平台策略。

#### Scenario: 不同入口检索同一范围
- **WHEN** 不同问答和搜索入口以相同 query 和知识库范围发起检索
- **THEN** 各入口使用相同的召回预算、融合上限、rerank 候选上限、最终上限、权重和阈值

#### Scenario: 历史智能体带有检索覆盖值
- **WHEN** 智能体记录仍包含合法但不同于平台设置的检索字段
- **THEN** 系统能够读取该记录但运行时忽略这些检索字段
- **AND** 智能体的提示词、工具、对话和其他非检索配置继续生效

### Requirement: 平台策略必须描述完整且有界的检索阶段
平台策略 MUST 包含 `enable_query_expansion`、`embedding_top_k`、`vector_recall_top_k`、`keyword_recall_top_k`、`rrf_vector_weight`、`rerank_candidate_top_k`、`rerank_top_k`、`batch_max_results`、`batch_max_content_chars` 和向量/关键词/rerank 阈值。`enable_query_expansion=false` 时任何路由和智能体 MUST NOT 重新启用查询扩展；为 true 时请求级路由 MAY 按复杂度决定是否实际扩展。rerank 模型 MUST 继续使用平台默认模型配置，检索策略不得创建第二个模型覆盖入口。召回和 rerank 数量 MUST 表示一次查询跨全部知识库的总预算，MUST NOT 乘知识库数量；batch 字段 MUST 仅限制 Integration 批量响应的整批总量。

#### Scenario: 多知识库搜索
- **WHEN** 请求选择多个知识库且平台两路 recall 均配置为 50
- **THEN** 向量和关键词检索各自最多请求 50 个候选
- **AND** 系统不得按知识库数量放大预算

#### Scenario: 非法阶段顺序
- **WHEN** 平台管理员提交 `rerank_top_k > rerank_candidate_top_k` 或 `rerank_candidate_top_k > embedding_top_k`
- **THEN** 系统拒绝保存并返回可定位字段的参数错误

#### Scenario: 旧平台配置缺少批量预算
- **WHEN** 系统读取尚无批量预算字段的平台设置
- **THEN** 有效配置使用 `batch_max_results=200` 和 `batch_max_content_chars=200000`

#### Scenario: 平台管理员保存非法批量预算
- **WHEN** platform_admin 提交非正数或超过服务端允许范围的批量总结果数或正文字符数
- **THEN** 系统拒绝保存并返回可定位字段的参数错误

### Requirement: 只有平台管理员可以修改检索策略
系统 MUST 仅允许 platform workspace 中的 `platform_admin` 查看编辑入口和保存平台检索策略。租户管理员、普通成员、智能体编辑器和外部 Integration 主体 MUST NOT 修改或覆盖策略。

#### Scenario: 平台管理员保存策略
- **WHEN** platform_admin 在平台设置提交合法检索策略
- **THEN** 系统原子保存完整配置并返回规范化后的有效值

#### Scenario: 普通管理员直接调用旧写接口
- **WHEN** tenant_admin 绕过隐藏的 UI 直接调用 retrieval-config 写接口
- **THEN** 系统返回 403 且平台配置保持不变

### Requirement: 重复检索配置入口必须关闭
系统 MUST 在平台设置提供唯一“检索策略”编辑入口，并 MUST 从知识搜索页和智能体编辑器移除检索参数编辑控件。关闭入口不得删除历史配置数据。

#### Scenario: 租户管理员打开知识搜索
- **WHEN** tenant_admin 或普通成员进入知识搜索页
- **THEN** 页面不显示检索参数编辑器

#### Scenario: 编辑智能体
- **WHEN** 有权编辑智能体的用户打开编辑器
- **THEN** 页面不显示召回、融合、rerank 数量、权重或阈值的覆盖控件

### Requirement: 平台统一维护快速问答兜底行为
系统 MUST 在平台检索策略页面提供独立的“回答兜底”分组，并 MUST 将兜底策略、固定回复和模型提示词保存在平台 `ConversationConfig`。快速问答 MUST 在每个新请求读取该平台配置，历史智能体兜底字段 MUST NOT 覆盖平台值。智能推理、智能体工具流程和纯检索接口 MUST NOT 因该配置改变原有降级行为。

#### Scenario: 平台修改兜底配置后发起快速问答
- **WHEN** platform_admin 保存新的兜底策略、固定回复或模型提示词
- **AND** 用户随后发起新的快速问答请求
- **THEN** 新请求使用刚保存的平台兜底配置
- **AND** 已经执行中的请求继续使用请求开始时的配置快照

### Requirement: 旧配置必须无损兼容并使用确定默认值
系统 MUST 保留租户和智能体历史检索字段的反序列化能力，MUST NOT 从任意局部历史值自动推断平台策略。平台新增字段缺失时 MUST 使用一套集中定义的确定默认值。

#### Scenario: 从旧版本升级且平台新增字段缺失
- **WHEN** 系统读取只有旧 RetrievalConfig 字段的平台设置
- **THEN** 系统补齐集中定义的默认值并为所有消费者生成完整有效快照
- **AND** 不选择任何租户或智能体的历史覆盖值

