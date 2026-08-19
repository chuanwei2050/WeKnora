## Why

当前 `MODEL_PROFILE` 只生成模型缺口清单，初始化流程不会把活动 Profile 中声明的模型登记到数据库，导致部署者已经配置内网模型端点后仍需逐项手工添加。生产初始化应直接生成一套可用且可重复执行的默认模型登记，并让清单只报告真实异常。

## What Changes

- 服务启动时读取活动 `MODEL_PROFILE` 对应的 `ONLINE_*` 或 `OFFLINE_*` 模型配置。
- 将有效的 Chat、Verifier、Evaluation Judge、Embedding、Rerank、VLM、ASR、TTS 配置一次性登记为普通可编辑默认模型。
- 使用持久化版本标记将 `ONLINE_*` 与 `OFFLINE_*` 各播种一次；播种完成后，重启不得覆盖管理员在 UI 中作出的修改。
- 使用 `MODEL_PROFILE` 初始化持久化活动 Profile，并允许管理员在设置页切换 online/offline。
- 模型记录保存 Profile 与角色；设置页只展示正在配置的 Profile，运行时把已有模型 ID 按角色解析到活动 Profile 对应模型。
- 配置缺失或包含占位符时跳过该角色，不以无效数据写库。
- 删除模型设置页的 Profile 检查清单及“内置模型”提示；新增紧凑的在线/离线切换控件。
- Verifier 与 Evaluation Judge 从对话模型中分离，显示在校验模型分组。
- 初始化登记不得发起模型推理请求，端点可用性仍由现有预检和严格离线门禁负责。
- 将默认 offline Profile 对齐到指定的内网服务端点和主 Chat/VLM 服务。

## Capabilities

### New Capabilities

- `profile-model-bootstrap`: 从活动模型 Profile 幂等创建平台默认模型登记，并与现有模型检查清单保持一致。

### Modified Capabilities

无。

## Impact

- 后端启动生命周期和模型仓储写入。
- `internal/modelprofile` 的 Profile 配置解析和角色映射。
- 模型配置环境变量及相关单元测试。
- 不新增外部 API 或第三方依赖。
