# CosyVoice2 TTS（构建前需将 CosyVoice 源码放到 ./vendor/CosyVoice）
# 离线可复用本地 vllm：docker build --build-arg BASE_IMAGE=vllm/vllm-openai:latest
ARG BASE_IMAGE=pytorch/pytorch:2.8.0-cuda12.8-cudnn9-runtime
FROM ${BASE_IMAGE}

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_INDEX_URL=https://mirrors.aliyun.com/pypi/simple \
    AIR_GAPPED_MODE=true \
    HF_HUB_OFFLINE=1 \
    TRANSFORMERS_OFFLINE=1 \
    MODEL_DIR=/models \
    PORT=8000 \
    SERVED_MODEL_NAME=model \
    QUANT=fp16 \
    PYTHONPATH=/opt/CosyVoice:/opt/CosyVoice/third_party/Matcha-TTS \
    PATH=/opt/conda/bin:${PATH}

WORKDIR /app

USER root
# apt 走阿里云镜像；显式安装 ffmpeg（兼容 pytorch / 其它 BASE_IMAGE）
RUN set -eux; \
    if command -v apt-get >/dev/null 2>&1; then \
      if [ -f /etc/apt/sources.list ]; then \
        sed -i 's|http://archive.ubuntu.com/ubuntu|http://mirrors.aliyun.com/ubuntu|g; s|http://security.ubuntu.com/ubuntu|http://mirrors.aliyun.com/ubuntu|g' /etc/apt/sources.list; \
      fi; \
      apt-get update \
      && apt-get install -y --no-install-recommends ffmpeg libsndfile1 git g++ make \
      && rm -rf /var/lib/apt/lists/*; \
    fi

COPY vendor/CosyVoice /opt/CosyVoice
RUN pip install --upgrade pip wheel "setuptools<81" \
 && if [ -f /opt/CosyVoice/requirements.txt ]; then \
      grep -viE '^(torch|torchaudio|torchvision|deepspeed|onnxruntime-gpu|openai-whisper|grpcio|grpcio-tools|gradio|fastapi-cli|gdown|matplotlib|pyarrow|tensorboard|tensorrt-cu12|tensorrt-cu12-bindings|tensorrt-cu12-libs)([=<>! ;]|$)' /opt/CosyVoice/requirements.txt > /tmp/cosy-req.txt \
      && pip install -r /tmp/cosy-req.txt; \
    fi

COPY requirements.txt .
RUN pip install -r requirements.txt
RUN pip install "gdown>=5.1.0" "matplotlib>=3.9.0" "pyarrow>=18.0.0"

COPY server.py .

EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=10s --start-period=180s --retries=20 \
  CMD python3 -c "import urllib.request,json; d=json.load(urllib.request.urlopen('http://127.0.0.1:8000/healthz')); assert d.get('ready') is True"

# 清除可能继承自 BASE_IMAGE 的 ENTRYPOINT（如 vLLM 镜像的 ["vllm","serve"]）
ENTRYPOINT []
CMD ["python3", "server.py"]
