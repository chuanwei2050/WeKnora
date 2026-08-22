#!/usr/bin/env bash
set -euo pipefail

cd /workspace

if [ ! -f .env ]; then
    echo '[ERROR] .env 文件不存在，请先创建配置文件' >&2
    exit 1
fi

# 兼容 Windows 编辑器生成的 UTF-8 BOM 与 CRLF。
requested_hot_reload="${WEKNORA_APP_HOT_RELOAD:-}"
set -a
env_bom=$'\xef\xbb\xbf'
source <(sed -e "1s/^${env_bom}//" -e 's/\r$//' .env)
set +a
if [ -n "$requested_hot_reload" ]; then
    export WEKNORA_APP_HOT_RELOAD="$requested_hot_reload"
fi

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
export ELASTICSEARCH_ADDR=http://elasticsearch:9200

runtime_dir=/tmp/weknora
mkdir -p "$runtime_dir"

build_app_to() {
    local output="$1"
    local ldflags
    ldflags="$(./scripts/get_version.sh ldflags) -X 'google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn'"
    env -i \
        PATH="$PATH" \
        HOME="$HOME" \
        GOPATH="${GOPATH:-/go}" \
        GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
        go build -ldflags="$ldflags" -o "$output" ./cmd/server
}

if [ "${WEKNORA_APP_HOT_RELOAD:-true}" != "true" ]; then
    echo '[INFO] 使用稳定模式启动后端，跳过 Air 文件监听。'
    build_app_to "$runtime_dir/main"
    exec "$runtime_dir/main"
fi

# Air 的 polling watcher 会为每个子目录启动一个轮询器。Windows bind mount 下，
# 大量并发 stat 容易触发 I/O error；这里改为单线程生成源码快照。
source_snapshot() {
    find cmd internal config migrations \
        -type f \
        ! -name '*_test.go' \
        \( -name '*.go' -o -name '*.sql' -o -name '*.tpl' -o -name '*.tmpl' -o -name '*.html' -o -name '*.yaml' \) \
        -printf '%T@ %s %p\n' \
        | sort \
        | sha256sum \
        | cut -d ' ' -f 1
}

build_app() {
    rm -f "$runtime_dir/main.next"
    build_app_to "$runtime_dir/main.next"
}

start_app() {
    "$runtime_dir/main" &
    app_pid=$!
}

stop_app() {
    if [ -n "${app_pid:-}" ] && kill -0 "$app_pid" 2>/dev/null; then
        kill "$app_pid"
        wait "$app_pid" 2>/dev/null || true
    fi
}

trap 'stop_app; exit 0' INT TERM

echo '[INFO] 首次构建后端应用...'
build_app
mv -f "$runtime_dir/main.next" "$runtime_dir/main"
last_snapshot="$(source_snapshot)"
start_app
echo '[INFO] 后端热更新已启动（单线程轮询，间隔 2 秒）。'

while kill -0 "$app_pid" 2>/dev/null; do
    sleep 2
    current_snapshot="$(source_snapshot)" || {
        echo '[WARN] 读取源码失败，2 秒后重试。' >&2
        continue
    }
    if [ "$current_snapshot" = "$last_snapshot" ]; then
        continue
    fi

    echo '[INFO] 检测到源码变化，重新构建...'
    if build_app; then
        stop_app
        mv -f "$runtime_dir/main.next" "$runtime_dir/main"
        last_snapshot="$current_snapshot"
        start_app
        echo '[INFO] 后端热更新完成。'
    else
        echo '[ERROR] 后端构建失败，保留当前进程并将在下一轮重试。' >&2
    fi
done

wait "$app_pid"
