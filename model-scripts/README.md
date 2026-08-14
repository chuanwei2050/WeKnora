# model-scripts — 内网离线模型一键部署

全容器方案：联网机下载并打包 → 拷到内网 → 一键启动。

入口只有一个：`deploy.py`。

---

## 三条核心命令

| 步骤 | 在哪跑 | 命令 |
|------|--------|------|
| **① 一键下载 + 打包** | 联网机 | `python deploy.py prepare --output-dir ./offline-bundle` |
| **② 传到服务器** | — | 拷贝 `offline-bundle.tar.gz`（U 盘 / 专线均可） |
| **③ 一键部署启动** | 内网服务器 | `python deploy.py deploy --bundle /tmp/offline-bundle.tar.gz --data-dir /mnt/models --host <内网IP>` |

可选校验：

```bash
python deploy.py verify --data-dir /mnt/models
```

> 首次准备可用 `--clean` 清空输出目录；**增量重跑不要加 `--clean`**（会删掉已下权重）。`--clean` 不会清理 `.cache/` 或本机 Docker 镜像。

---

## 完整示例

### 联网机（准备包）

```bash
cd model-scripts
python -m venv .venv
# Windows: .venv\Scripts\activate
source .venv/bin/activate

pip install -r requirements.txt
cp config.yaml.example config.yaml
# 按需改 config.yaml：deploy.host、GPU 数量、是否启用 chat 等
# 可选: export HF_TOKEN=xxx / MODELSCOPE_TOKEN=xxx

# 增量准备（已有权重会跳过；Hub 不可达时会复用本地基础镜像）
python deploy.py prepare --output-dir ./offline-bundle
```

常用开关：

| 参数 | 作用 |
|------|------|
| `--clean` | 清空 `offline-bundle/` 后重建（慎用） |
| `--force-download` | 强制重下全部启用模型 |
| `--skip-docker` | 跳过镜像 pull/build/save（只更权重/配置/打包） |
| `--skip-docker-build` | 仍处理 vLLM 镜像，跳过 rerank/asr/tts 构建 |
| `--skip-pip` | 跳过工具自身 `offline_packages` 下载 |
| `--no-archive` | 不生成 `.tar.gz` |

产物：

- `offline-bundle/` — 权重、镜像 tar、compose、登记文件
- `offline-bundle.tar.gz` — **拷到内网的这一份**

包内镜像（`offline-bundle/images/`）：

| 文件 | 用途 |
|------|------|
| `vllm-openai.tar` | Embedding / Verifier /（可选）Chat |
| `weknora-rerank.tar` | Rerank（ONNX INT8） |
| `weknora-asr.tar` | ASR（ONNX INT8） |
| `weknora-tts.tar` | TTS（fp16） |

`prepare` 会：下载模型 → pull/build Docker 镜像并 `docker save` → 打 tar.gz。Docker Hub 短暂失败时，若本机已有同名镜像会复用并重新 `docker save`；业务镜像基础层也会优先选用本机已有 `python`/`pytorch`（或兼容镜像）。

### 内网服务器（部署）

```bash
export AIR_GAPPED_MODE=true

# 若服务器还没有本工具依赖：
# pip install --no-index --find-links=./offline-bundle/offline_packages -r requirements.txt

python deploy.py deploy \
  --bundle /tmp/offline-bundle.tar.gz \
  --data-dir /mnt/models \
  --host 192.168.1.100
```

`deploy` 会自动：解压 → 校验权重 → `docker load` → `docker compose up -d`。

日常启停：

```bash
docker compose -p weknora-models -f /mnt/models/docker-compose.airgap.override.yml up -d
docker compose -p weknora-models -f /mnt/models/docker-compose.airgap.override.yml down
docker compose -p weknora-models -f /mnt/models/docker-compose.airgap.override.yml logs -f
```

---

## 默认服务端口

| 角色 | 端口 | 量化选择 | 说明 |
|------|------|----------|------|
| Chat + VLM | 8000 | **FP8** | 默认不下载；配置已写好 `Qwen/Qwen3.6-27B-FP8`，启用即按 FP8 |
| Embedding | 8001 | **AWQ-INT4** | 约 2.7GB，优于官方 bf16≈7.5GB |
| Rerank | 8002 | **ONNX INT8** | 约 0.5GB，优于官方≈2GB |
| Verifier + Judge | 8003 | **AWQ** | `Qwen3.5-9B`（默认与 Embedding 同 GPU0；双卡可改 `["1"]`） |
| ASR | 8004 | **ONNX INT8** | 约 0.23GB，优于官方≈0.9GB |
| TTS | 8005 | fp16 | 暂无可用 CosyVoice2 INT8 制品 |

策略：**能量化且有优势（体积/显存/速度）且栈可加载 → 用量化；否则用官方全精度。**

> 默认按 **单卡**（Embedding + Verifier 均 `device_ids: ["0"]`，显存占用已调低）。双卡可将 verifier 改为 `["1"]` 并提高双方 `gpu_memory_utilization`。

健康检查示例：

```bash
curl -s http://127.0.0.1:8001/v1/models
curl -s http://127.0.0.1:8003/v1/models
curl -s http://127.0.0.1:8002/healthz
```

---

## 目录说明

```
model-scripts/
├── deploy.py                 # 唯一入口（prepare / deploy / verify / gen-config）
├── config.yaml.example       # 配置模板（复制为 config.yaml 后修改）
├── config.yaml               # 本地配置（已 gitignore，勿提交）
├── requirements.txt          # 本工具依赖
├── docker-compose.airgap.override.yml  # 审阅模板；真正运行以 deploy 生成为准
├── services/                 # Rerank / ASR / TTS 的 Dockerfile + 服务代码
├── templates/                # WeKnora 批准端点 / 模型登记示例
├── systemd/                  # 遗留兜底（主路径用 compose，一般不用）
├── test_deploy.py            # 离线单测
├── .cache/                   # Hub/临时缓存（gitignore，落项目盘）
├── offline-bundle/           # prepare 产物（gitignore）
└── README.md
```

---

## 配置要点（config.yaml）

- 先 `cp config.yaml.example config.yaml` 再改
- **量化优先（有优势才切）**：Embedding/Rerank/ASR/Verifier 已用量化仓；Chat 27B 虽默认不下载，配置已固定 **FP8**（`Qwen/Qwen3.6-27B-FP8`）；TTS 仍为 fp16（无可用 INT8）
- 切换 `download_id`/`quant`/`revision` 后，`prepare` 会检测 `.airgap_source` 变更并自动重下（也可 `--force-download`）
- `models.verifier` 对应平台 `OFFLINE_VERIFIER_MODEL_2` / `OFFLINE_EVALUATION_JUDGE`（同端点，`roles: [evaluation_judge]`）
- `models.chat.enabled: true` 才下载/部署主模型（按已配 FP8）；VLM 通过 `roles: [vlm]` 共用，**不要**单独配 VLM 服务
- `engine: vllm` = Embedding / Verifier / Chat；`engine: container` = Rerank/ASR/TTS
- 量化权重必须填 `download_id`；写了 `fp8/awq/onnx-int8/...` 却不填，`prepare` 会失败
- **下载缓存默认在 `model-scripts/.cache/`**（项目盘），不写 `C:\Users\...`；可用 `bundle.hub_cache_dir` 改路径
- 默认单卡：Embedding / Verifier 均 → `device_ids: ["0"]`；双卡可将 verifier 改为 `["1"]` 并提高 `gpu_memory_utilization`
- 默认 ASR/TTS `needs_gpu: false`，避免和 Embedding 抢 GPU；多卡可改 `true` 并设 `device_ids`

密钥只用环境变量，**不要写进配置或打包介质**：

- `HF_TOKEN` / `HUGGING_FACE_HUB_TOKEN`
- `MODELSCOPE_TOKEN`
- 内网务必：`AIR_GAPPED_MODE=true`

---

## 产物落盘（勿提交 git）

| 阶段 | 路径 |
|------|------|
| prepare | `model-scripts/offline-bundle/`、`*.tar.gz`（已 ignore） |
| Hub 缓存 | `model-scripts/.cache/`（HF / ModelScope / tmp，已 ignore） |
| deploy | `/mnt/models/models/<角色>/`、compose、registry |

部署后平台登记文件：

- `/mnt/models/registry/approved_endpoints.json`
- `/mnt/models/registry/model_registry.yaml`

WeKnora 创建批准端点会生成新 UUID，请把 `approved_endpoint_id` 回填成平台返回的 ID。

---

## 测试

```bash
cd model-scripts
pip install -r requirements.txt
python test_deploy.py
```

不拉真实模型、不构建镜像；验证校验和、compose、CLI、airgap 拦截等。
