#!/usr/bin/env bash
set -euo pipefail

cd /workspace

if [ ! -f .env ]; then
    echo '[ERROR] .env 文件不存在，请先创建配置文件' >&2
    exit 1
fi

# 兼容 Windows 编辑器生成的 UTF-8 BOM 与 CRLF。
set -a
env_bom=$'\xef\xbb\xbf'
source <(sed -e "1s/^${env_bom}//" -e 's/\r$//' .env)
set +a

# 容器通过 Compose 开发网络访问基础设施。
export DB_HOST=postgres
export REDIS_ADDR=redis:6379
export DOCREADER_ADDR=docreader:50051
export DOCREADER_TRANSPORT=grpc
export MINIO_ENDPOINT=minio:9000
export MILVUS_ADDRESS=milvus:19530
export OTEL_EXPORTER_OTLP_ENDPOINT=jaeger:4317
export NEO4J_URI=bolt://neo4j:7687
export QDRANT_HOST=qdrant

if ! command -v air > /dev/null 2>&1; then
    go install github.com/air-verse/air@v1.61.7
fi

# Windows 绑定目录的文件事件不总能传入 Linux 容器，使用轮询确保热更新可靠。
sed \
    -e 's/^  poll = false/  poll = true/' \
    -e 's/^  poll_interval = 0/  poll_interval = 2000/' \
    -e 's/^  include_dir = \[\]/  include_dir = ["cmd", "internal", "config", "migrations"]/' \
    .air.toml > /tmp/weknora-air.toml

exec air -c /tmp/weknora-air.toml
