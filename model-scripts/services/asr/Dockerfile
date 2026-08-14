# ASR SenseVoice / FunASR
# 默认 pytorch CUDA runtime；离线可复用本地 vllm 镜像：
#   docker build --build-arg BASE_IMAGE=vllm/vllm-openai:latest
ARG BASE_IMAGE=pytorch/pytorch:2.2.2-cuda12.1-cudnn8-runtime
FROM ${BASE_IMAGE}

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    AIR_GAPPED_MODE=true \
    HF_HUB_OFFLINE=1 \
    TRANSFORMERS_OFFLINE=1 \
    MODEL_DIR=/models \
    PORT=8000 \
    SERVED_MODEL_NAME=sensevoice-small \
    QUANT=onnx-int8

WORKDIR /app

USER root
RUN set -eux; \
    if command -v apt-get >/dev/null 2>&1; then \
      apt-get update && apt-get install -y --no-install-recommends ffmpeg libsndfile1 \
      && rm -rf /var/lib/apt/lists/*; \
    elif command -v apk >/dev/null 2>&1; then \
      apk add --no-cache ffmpeg libsndfile; \
    fi

COPY requirements.txt .
RUN pip install --upgrade pip && pip install -r requirements.txt
# 基础镜像已含 CUDA torch；requirements 不得再声明 torch

COPY server.py .

EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=10s --start-period=120s --retries=15 \
  CMD python -c "import urllib.request,json; d=json.load(urllib.request.urlopen('http://127.0.0.1:8000/healthz')); assert d.get('ready') is True"

# 清除可能继承自 BASE_IMAGE 的 ENTRYPOINT（如 vLLM 镜像的 ["vllm","serve"]）
ENTRYPOINT []
CMD ["python", "server.py"]
