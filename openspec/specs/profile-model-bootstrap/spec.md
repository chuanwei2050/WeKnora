# profile-model-bootstrap Specification

## Purpose
TBD - created by archiving change auto-initialize-profile-models. Update Purpose after archive.
## Requirements
### Requirement: 活动 Profile 模型自动登记
系统必须（MUST）仅在持久化播种版本尚未完成时读取 `ONLINE_*` 与 `OFFLINE_*` 模型环境变量，并为两套配置中所有有效角色创建平台可见、可编辑的普通模型登记。每条登记必须保存所属 Profile 与角色。支持的角色必须包含 Chat、Verifier 1、Verifier 2、Evaluation Judge、Embedding、Rerank、VLM、ASR、TTS。

#### Scenario: offline Profile 首次启动
- **WHEN** `MODEL_PROFILE=offline` 且 offline 模型名称和 Base URL 均有效
- **THEN** 系统在模型列表中创建对应的普通默认模型，且名称、类型、Base URL、API Key、Embedding 维度与环境变量一致

#### Scenario: online Profile 首次启动
- **WHEN** `MODEL_PROFILE=online` 且 online 模型配置有效
- **THEN** 系统只登记 `ONLINE_*` 所描述的模型，不混入 `OFFLINE_*` 配置

### Requirement: 幂等与角色复用
系统必须（MUST）在首次播种时复用同名、同类型、同规范化 Base URL 的现有模型；成功后记录播种版本。重复启动不得再次根据 env 创建、覆盖或补回模型。若一个已有登记的类型同时满足多个 Profile 角色，系统必须复用该登记而不是按角色重复创建。

#### Scenario: 重复启动
- **WHEN** 数据库已经存在由当前 Profile 描述的等价模型登记
- **THEN** 系统不新增模型记录且不覆盖管理员后来选择的默认模型

#### Scenario: 管理员修改或删除种子模型
- **WHEN** 播种完成后管理员在模型设置页修改服务器地址、名称、凭据或删除模型
- **THEN** 后续服务启动保留该数据库状态，不从 env 恢复旧值或重新创建已删除模型

#### Scenario: Chat 兼任第一个 Verifier
- **WHEN** `verifier_1` 与 Chat 使用相同名称且清单允许 `KnowledgeQA` 类型
- **THEN** 系统使用 Chat 登记满足 `verifier_1`，不额外创建同端点 Verifier 记录

### Requirement: 无效配置安全跳过
系统必须（MUST）跳过名称为空、名称或 Base URL 含占位符、或 Base URL 无法解析的角色，且不得把 API Key 写入日志或 Profile 状态响应。

#### Scenario: 角色配置仍含占位符
- **WHEN** 某角色名称或 Base URL 含 `__FILL_` 或未展开的 `${...}`
- **THEN** 系统不为该角色写入模型记录，并由现有 Profile 清单继续报告缺口

### Requirement: offline 种子端点显式批准
系统必须（MUST）为有效的 offline 远程模型 env 种子按规范化 scheme、host、port 创建或复用平台批准端点，并把模型绑定到该端点。批准用途与模型角色必须精确合并；不得因此放行未在 env 中声明的其他私网目标。

#### Scenario: 预检内网种子模型
- **WHEN** offline 种子模型指向 `192.168.10.232` 且管理员执行能力预检
- **THEN** 批准端点校验通过并向该精确目标执行预检

#### Scenario: 管理员修改种子 URL
- **WHEN** 管理员把已绑定种子模型的 Base URL 改为不同主机或端口
- **THEN** 原批准端点不匹配且请求被拒绝，直到管理员显式批准新目标

### Requirement: 默认值与生命周期
系统必须（MUST）把首次创建的当前 Profile 模型设为对应模型类型的默认模型，但不得修改现有知识库绑定。数据库写入失败时服务启动必须失败并报告不含密钥的错误。

#### Scenario: 首次创建默认模型
- **WHEN** 当前 Profile 某模型类型尚无等价登记
- **THEN** 系统创建该登记、将其设为该类型默认模型，并保留已有知识库的模型 ID

#### Scenario: 数据库写入失败
- **WHEN** 引导过程中无法创建或更新模型记录
- **THEN** 服务不进入可服务状态，错误信息标识失败角色但不包含 API Key

### Requirement: 模型设置页不展示 Profile 与内置限制
模型设置页不得（MUST NOT）展示 Profile 状态检查清单或内置模型说明。页面必须（MUST）提供 online/offline 切换控件，并只展示当前所选 Profile 的模型。由 Profile env 播种的模型必须（MUST）按普通模型展示并支持编辑、删除。

#### Scenario: 查看并编辑播种模型
- **WHEN** 管理员打开模型设置页
- **THEN** 页面展示当前活动 Profile 的模型分类列表，种子模型不带“内置”标签且操作菜单包含编辑与删除

### Requirement: Profile 切换控制实际模型路由
系统必须（MUST）使用 `MODEL_PROFILE` 初始化持久化活动 Profile，并提供仅平台管理员可调用的查询和切换接口。运行时取得 Profile 模型时，必须按逻辑角色解析为活动 Profile 下的模型；不得因已有知识库保存了另一 Profile 的模型 ID 而继续调用非活动 Profile。

#### Scenario: 从 online 切换为 offline
- **WHEN** 管理员把活动 Profile 从 online 切换为 offline
- **THEN** 后续 Chat、Embedding、Rerank、VLM、ASR、TTS 和验证调用使用对应 offline 角色模型

#### Scenario: 活动 Profile 缺少角色
- **WHEN** 请求所需角色在活动 Profile 下没有模型
- **THEN** 请求明确失败并指出缺失角色，不回退到另一 Profile

#### Scenario: Embedding 维度不兼容
- **WHEN** 管理员切换 Profile 且两套 Embedding 模型的有效维度不同
- **THEN** 系统拒绝切换并提示需要先处理索引兼容性

### Requirement: 校验模型独立分组
模型设置页必须（MUST）把 `Verifier` 与 `EvaluationJudge` 类型展示在校验模型分组，不得混入普通对话模型分组。

#### Scenario: 展示 9B 校验模型
- **WHEN** 当前 Profile 包含 `qwen3.5-9b` Verifier 或 Evaluation Judge 登记
- **THEN** 该模型只出现在校验模型分组，并可独立编辑

### Requirement: 所有模型选择器遵循活动 Profile
对话输入框、知识库配置、Agent 配置及通用模型选择器必须（MUST）只提供持久化活动 Profile 下的模型，不得同时展示 online 与 offline 模型。若先前选中的模型不属于活动 Profile，界面必须选择当前 Profile 下同类的默认或首个可用模型。

#### Scenario: offline 对话模型下拉
- **WHEN** 当前活动 Profile 为 offline 且用户打开对话输入框的模型下拉
- **THEN** 下拉只展示 offline 的 `KnowledgeQA` 模型，不展示 online 模型

