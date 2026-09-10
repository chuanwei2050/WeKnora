# knowledge-base-qualification-aliases Specification

## Purpose
TBD - created by archiving change add-knowledge-base-qualification-aliases. Update Purpose after archive.
## Requirements
### Requirement: 知识库管理员可配置特殊资质简称
系统 MUST 允许具有知识库管理权限的用户在知识库设置中查看和保存“简称—标准名称”映射，并 MUST 将配置持久化在目标知识库范围内。

#### Scenario: 保存合法配置
- **WHEN** 管理员提交合法的简称和标准名称映射
- **THEN** 系统保存配置并在后续新检索中使用它

#### Scenario: 无权限用户修改配置
- **WHEN** 无知识库管理权限的用户尝试修改简称配置
- **THEN** 系统拒绝操作且不改变配置

### Requirement: 系统必须校验简称配置
系统 MUST 清理简称和标准名称的首尾空白，MUST 拒绝空值、超出长度或数量上限的配置，并 MUST 拒绝同一知识库内规范化后重复的简称。

#### Scenario: 提交重复简称
- **WHEN** 同一知识库配置中出现大小写或空白规范化后相同的简称
- **THEN** 系统拒绝保存并返回可理解的校验错误

### Requirement: 系统必须在知识库检索路由前统一扩展查询
系统 MUST 在选择向量、Elasticsearch、SQL、知识图谱、Wiki 或 FAQ 检索渠道之前检查当前授权知识库范围内的简称配置；命中时 MUST 保留完整原问题并追加对应标准名称，未命中时 MUST 保持查询不变。

#### Scenario: 查询命中一个简称
- **WHEN** 用户问题包含当前知识库配置中的简称
- **THEN** 系统生成包含完整原问题和对应标准名称的统一查询，再执行下游检索

#### Scenario: 查询未命中简称
- **WHEN** 用户问题不包含当前知识库范围内配置的简称
- **THEN** 系统将原问题不加修改地交给下游检索

#### Scenario: 多知识库配置相同简称
- **WHEN** 当前授权检索范围内多个知识库将同一简称映射到不同标准名称
- **THEN** 系统保留原问题并追加所有不同标准名称，且不越权读取配置

### Requirement: 所有知识库检索渠道必须复用统一查询
向量、Elasticsearch、SQL、知识图谱、Wiki 和 FAQ 检索 MUST 消费路由前生成的同一查询文本，MUST NOT 各自实现不同的简称映射规则；独立 Agent 知识库工具入口 MUST 复用同一预处理能力。

#### Scenario: 一个问题启用多个检索渠道
- **WHEN** 命中简称的问题同时路由到多个知识库检索渠道
- **THEN** 每个渠道接收相同的已扩展查询，结果继续进入现有合并和排序流程

#### Scenario: Web 搜索与知识库搜索并存
- **WHEN** 一个请求同时使用知识库检索和 Web 搜索
- **THEN** 知识库渠道使用已扩展查询，而 Web 搜索保持既有查询行为

### Requirement: 查询扩展必须保持可用性并受限
系统 MUST 对追加标准名称去重并限制配置数量、单项长度和扩展后总长度；配置读取或扩展失败时 MUST 使用原查询继续检索，不得使问答请求失败。

#### Scenario: 扩展结果达到长度上限
- **WHEN** 可追加的标准名称会使查询超过扩展长度上限
- **THEN** 系统按确定性顺序截断追加内容并完整保留原问题

#### Scenario: 配置读取失败
- **WHEN** 检索前无法读取简称配置
- **THEN** 系统记录错误并使用原问题继续既有检索流程

