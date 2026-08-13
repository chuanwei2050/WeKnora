# CosyVoice2 TTS（构建前需将 CosyVoice 源码放到 ./vendor/CosyVoice）
FROM pytorch/pytorch:2.2.2-cuda12.1-cudnn8-runtime

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    AIR_GAPPED_MODE=true \
    HF_HUB_OFFLINE=1 \
    TRANSFORMERS_OFFLINE=1 \
    MODEL_DIR=/models \
    PORT=8000 \
    SERVED_MODEL_NAME=cosyvoice2-0.5b \
    QUANT=fp16 \
    PYTHONPATH=/opt/CosyVoice:/opt/CosyVoice/third_party/Matcha-TTS

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
      ffmpeg libsndfile1 git \
    && rm -rf /var/lib/apt/lists/*

# CosyVoice 运行时（过滤 torch*，保留 CUDA 基础镜像中的版本）
COPY vendor/CosyVoice /opt/CosyVoice
RUN pip install --upgrade pip \
 && if [ -f /opt/CosyVoice/requirements.txt ]; then \
      grep -viE '^(torch|torchaudio|torchvision)([=<>!]|$)' /opt/CosyVoice/requirements.txt > /tmp/cosy-req.txt \
      && pip install -r /tmp/cosy-req.txt; \
    fi

COPY requirements.txt .
RUN pip install -r requirements.txt

COPY server.py .

EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=10s --start-period=180s --retries=20 \
  CMD python -c "import urllib.request,json; d=json.load(urllib.request.urlopen('http://127.0.0.1:8000/healthz')); assert d.get('ready') is True"

CMD ["python", "server.py"]
