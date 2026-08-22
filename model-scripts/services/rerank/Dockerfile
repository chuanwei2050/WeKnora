# Rerank 服务镜像（CPU 即可）
# Hub 不可达时可用本地 Debian 镜像，例如：
#   --build-arg BASE_IMAGE=wechatopenai/weknora-app:latest
ARG BASE_IMAGE=python:3.11-slim-bookworm
FROM ${BASE_IMAGE}

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    AIR_GAPPED_MODE=true \
    HF_HUB_OFFLINE=1 \
    TRANSFORMERS_OFFLINE=1 \
    MODEL_DIR=/models \
    PORT=8000 \
    SERVED_MODEL_NAME=model \
    QUANT=onnx-int8

WORKDIR /app

USER root
RUN set -eux; \
    if command -v apt-get >/dev/null 2>&1; then \
      apt-get update \
      && apt-get install -y --no-install-recommends libgomp1 \
      && rm -rf /var/lib/apt/lists/*; \
    elif command -v apk >/dev/null 2>&1; then \
      apk add --no-cache libgomp; \
    fi; \
    if ! command -v python >/dev/null 2>&1 && command -v python3 >/dev/null 2>&1; then \
      ln -sf "$(command -v python3)" /usr/local/bin/python; \
    fi; \
    if ! command -v pip >/dev/null 2>&1 && command -v pip3 >/dev/null 2>&1; then \
      ln -sf "$(command -v pip3)" /usr/local/bin/pip; \
    fi

COPY requirements.txt .
# ONNX INT8 为主；Debian 系统 Python 需 --break-system-packages
RUN pip install --upgrade pip --break-system-packages \
    && (pip install --break-system-packages torch --index-url https://download.pytorch.org/whl/cpu \
        || pip install --break-system-packages torch || true) \
    && pip install --break-system-packages -r requirements.txt

COPY server.py .

EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=10 \
  CMD python -c "import urllib.request,json; d=json.load(urllib.request.urlopen('http://127.0.0.1:8000/healthz')); assert d.get('ready') is True"

# 清除可能继承自 BASE_IMAGE 的 ENTRYPOINT（如 weknora-app）
ENTRYPOINT []
CMD ["python", "server.py"]
