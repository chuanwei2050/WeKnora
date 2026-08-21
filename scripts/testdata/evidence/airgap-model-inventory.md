# 内网模型角色清单

> 清单快照：2026-08-11。本文记录当前开发机上可直接核验的 Ollama 制品和仓库已实现的模型角色适配器；“未提供”是验收缺项，不代表系统会回退到公网。

## 采集依据

本快照由以下本机命令生成：

- `ollama list`
- `ollama show -v <model>`
- `ollama show --license <model>`
- `Get-CimInstance Win32_ComputerSystem`
- `Get-CimInstance Win32_VideoController`

当前主机报告约 15.6 GiB 内存和 NVIDIA RTX 3050 Laptop GPU（约 4 GiB 显存）。当前 Ollama 监听本机 `127.0.0.1:11434`，推理引擎为 Ollama，协议为 Ollama 原生 API；Lite 端点必须由服务端判定为 `same-host` 后才能参与单节点门禁。

## 已预加载制品

| 制品 | Ollama ID | 角色与已观察能力 | 参数/量化 | 上下文/维度 | 许可证证据 | 当前硬件建议 |
|---|---|---|---|---|---|---|
| `qwen3.5:0.8b` | `f3817196d142` | Chat/VLM；具备 completion、vision、tools、thinking | 873.44M，Q8_0 | 262144 / 1024 | Ollama Modelfile 明示 Apache-2.0 | 当前 4 GiB GPU 可加载；并发按 1 起步 |
| `bge-m3:latest` | `790764642607` | Embedding | 566.70M，F16 | 8192 / 1024 | Ollama Modelfile 明示 MIT | 当前 4 GiB GPU 或 CPU 可加载；知识库维度必须为 1024 |
| `nomic-embed-text:latest` | `0a109f422b47` | Embedding | 137M，F16 | 2048 / 768 | Ollama Modelfile 明示 Apache-2.0 | CPU/GPU 均可；不可与 1024 维索引混用 |
| `minicpm-v4.6:latest` | `e95583acac77` | VLM/Chat；具备 completion、vision、tools、thinking | 752.16M + 548.27M，Q4_K_M | 262144 / 1024 | 本机 Modelfile 未声明许可证，须由部署方补充来源证明 | 需要视觉编码器显存；当前机器仅作单并发探针 |
| `gme-qwen2-vl:Q4_K_M` | `c4c51164c020` | 视觉/多模态候选；需通过 VLM 图像探针后才可启用 | 1.5B，Q4_K_M | 32768 / 1536 | 本机 Modelfile 未声明许可证，禁止自动打包权重 | 需要视觉编码器显存；当前机器仅作单并发探针 |
| `qwen3-embedding:0.6b` | `ac6da0dfba84` | Embedding 候选 | 595.78M，Q8_0 | 32768 / 1024 | 本机 Modelfile 未声明许可证，须补充来源证明 | 当前 4 GiB GPU 或 CPU 可加载 |

## 角色覆盖与缺口

| 角色 | 当前内网候选 | 协议/引擎 | 当前状态 |
|---|---|---|---|
| Chat | `qwen3.5:0.8b` | Ollama / Ollama | 已发现，可执行本地 Chat 探针 |
| Embedding | `bge-m3:latest`、`nomic-embed-text:latest`、`qwen3-embedding:0.6b` | Ollama / Ollama | 已发现；维度需与知识库绑定一致 |
| Rerank | 无 | OpenAI-compatible 适配器已实现 | 缺少预加载模型，离线预检必须失败并列明缺项 |
| VLM | `qwen3.5:0.8b`（复用 Chat）、`minicpm-v4.6:latest`、`gme-qwen2-vl:Q4_K_M` | Ollama / Ollama | `qwen3.5:0.8b` 可作为单模型部署方案，但仍须通过图像输入探针；其余为独立候选 |
| ASR | 无 | OpenAI-compatible 适配器已实现 | 缺少内网端点/模型 |
| TTS | 无 | OpenAI-compatible 适配器已实现 | 缺少内网端点/模型 |
| 验证/反思 | 无独立候选 | 复用 Chat 结构化输出能力 | 严格多模型验收缺少第二个规范化模型身份 |
| 评测裁判 | 无独立候选 | 复用 Chat 结构化输出能力 | 未配置裁判模型与冻结评测基线 |
| Parser/OCR | 无专用模型 | 文档解析器和 VLM 能力可选 | 当前未启用模型 OCR 路径 |

## 交付前必须补齐

1. 为 Rerank、ASR、TTS、验证/反思、评测裁判和启用的 Parser/OCR 角色登记内网端点、模型版本、协议、推理引擎、硬件配额和能力探针结果。
2. 为 `minicpm-v4.6`、`gme-qwen2-vl` 和 `qwen3-embedding` 补充可再分发许可证、权重 SHA-256 和客户导入步骤；未补齐前不进入离线包。
3. 使用真实部署硬件重跑上下文、并发、TTFT、Embedding 维度、VLM 图像、结构化输出和音频探针。本机清单只证明制品已预加载，不构成线上/生产质量基线。
4. 只有全部必需角色都具备批准端点和通过结果后，才可生成最终 `desktop-lite`/`compose-airgap`/`helm-airgap` 交付结论。
