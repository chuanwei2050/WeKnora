# model-scripts — 内网离线模型一键部署

全容器方案：联网机下载并打包 → 拷到内网 → 一键启动。

入口只有一个：`deploy.py`。

---

## 三条核心命令

| 步骤 | 在哪跑 | 命令 |
|------|--------|------|
| **① 一键下载 + 打包** | 联网机 | `python deploy.py prepare --output-dir ./offline-bundle --clean` |
| **② 传到服务器** | — | 拷贝 `offline-bundle.tar.gz`（U 盘 / 专线均可） |
| **③ 一键部署启动** | 内网服务器 | `python deploy.py deploy --bundle /tmp/offline-bundle.tar.gz --data-dir /mnt/models --host <内网IP>` |

可选校验：

```bash
python deploy.py verify --data-dir /mnt/models
```

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
# 按需改 config.yaml：deploy.host、是否启用 chat 等
# 可选: export HF_TOKEN=xxx

python deploy.py prepare --output-dir ./offline-bundle --clean
```

产物：

- `offline-bundle/` — 权重、镜像 tar、compose、登记文件
- `offline-bundle.tar.gz` — **拷到内网的这一份**

`prepare` 会自动：下载模型 → pull/build Docker 镜像并 `docker save` → 打 tar.gz。

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

| 角色 | 端口 | 说明 |
|------|------|------|
| Chat + VLM | 8000 | 默认关闭；VLM **共用**主 Chat，不单独部署 |
| Embedding | 8001 | vLLM 容器 |
| Rerank | 8002 | `weknora-rerank:airgap` |
| ASR | 8004 | `weknora-asr:airgap` |
| TTS | 8005 | `weknora-tts:airgap` |

健康检查示例：

```bash
curl -s http://127.0.0.1:8001/v1/models
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
└── README.md
```

---

## 配置要点（config.yaml）

- 先 `cp config.yaml.example config.yaml` 再改
- `models.chat.enabled: true` 才下载/部署主模型；VLM 通过 `roles: [vlm]` 共用，**不要**单独配 VLM 服务
- `engine: vllm` = Embedding/Chat；`engine: container` = Rerank/ASR/TTS
- 量化权重用 `download_id`；写了 `fp8/awq/...` 却不填 `download_id`，`prepare` 会失败
- 单卡默认 ASR/TTS `needs_gpu: false`，避免和 Embedding 抢 GPU；多卡可改 `true` 并设 `device_ids`

密钥只用环境变量，**不要写进配置或打包介质**：

- `HF_TOKEN` / `HUGGING_FACE_HUB_TOKEN`
- `MODELSCOPE_TOKEN`
- 内网务必：`AIR_GAPPED_MODE=true`

---

## 产物落盘（勿提交 git）

| 阶段 | 路径 |
|------|------|
| prepare | `model-scripts/offline-bundle/`、`*.tar.gz`（已 ignore） |
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
