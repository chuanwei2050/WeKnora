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
export DB_PORT="${DB_PORT:-5432}"
export REDIS_ADDR=redis:6379
export DOCREADER_ADDR=docreader:50051
export DOCREADER_TRANSPORT=grpc
export MINIO_ENDPOINT=minio:9000
export MINIO_PUBLIC_ENDPOINT="${MINIO_PUBLIC_ENDPOINT:-localhost:9000}"
export MINIO_ACCESS_KEY_ID="${MINIO_ACCESS_KEY_ID:-minioadmin}"
export MINIO_SECRET_ACCESS_KEY="${MINIO_SECRET_ACCESS_KEY:-minioadmin}"
export MINIO_BUCKET_NAME="${MINIO_BUCKET_NAME:-weknora}"
export MINIO_USE_SSL="${MINIO_USE_SSL:-false}"
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
    # Windows bind mount 下容器内 git 常失败（exit 128），禁用 VCS stamping。
    env -i \
        PATH="$PATH" \
        HOME="$HOME" \
        GOPATH="${GOPATH:-/go}" \
        GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
        GOFLAGS="${GOFLAGS:--buildvcs=false}" \
        go build -buildvcs=false -ldflags="$ldflags" -o "$output" ./cmd/server
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

start_app_bin() {
    local bin="$1"
    "$bin" &
    echo $!
}

stop_pid() {
    local pid="$1"
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
        kill "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
    fi
}

wait_app_ready() {
    local pid="$1"
    local timeout_secs="${2:-45}"
    local i

    for i in $(seq 1 "$timeout_secs"); do
        if ! kill -0 "$pid" 2>/dev/null; then
            return 1
        fi
        if curl --noproxy '*' -fsS --max-time 1 "http://127.0.0.1:8080/health" >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done

    # Timed out without a healthy /health — do not treat a still-alive process as ready.
    return 1
}

app_pid=""

stop_app() {
    stop_pid "$app_pid"
    app_pid=""
}

trap 'stop_app; exit 0' INT TERM

echo '[INFO] 首次构建后端应用...'
build_app
mv -f "$runtime_dir/main.next" "$runtime_dir/main"
cp -f "$runtime_dir/main" "$runtime_dir/main.prev"
last_snapshot="$(source_snapshot)"
app_pid="$(start_app_bin "$runtime_dir/main")"
if ! wait_app_ready "$app_pid" 120; then
    echo '[ERROR] 后端首次启动失败' >&2
    exit 1
fi
echo '[INFO] 后端热更新已启动（单线程轮询，间隔 2 秒）。'

consecutive_crashes=0

# 监督循环不能因业务进程退出而结束：否则 docker run --rm 会把整个后端容器带走。
while true; do
    if ! kill -0 "$app_pid" 2>/dev/null; then
        consecutive_crashes=$((consecutive_crashes + 1))
        echo "[WARN] 后端进程已退出 (连续 ${consecutive_crashes} 次)，正在重新拉起..." >&2
        if [ "$consecutive_crashes" -ge 3 ]; then
            echo '[WARN] 连续崩溃，等待下一次源码变化后再构建重启。' >&2
            while true; do
                sleep 2
                current_snapshot="$(source_snapshot)" || continue
                if [ "$current_snapshot" != "$last_snapshot" ]; then
                    break
                fi
            done
            echo '[INFO] 检测到源码变化，重新构建...'
            if build_app && mv -f "$runtime_dir/main.next" "$runtime_dir/main"; then
                cp -f "$runtime_dir/main" "$runtime_dir/main.prev"
                last_snapshot="$(source_snapshot)" || true
                consecutive_crashes=0
            else
                echo '[ERROR] 后端构建失败，继续等待源码变化。' >&2
                last_snapshot="$(source_snapshot)" || true
                continue
            fi
        fi
        app_pid="$(start_app_bin "$runtime_dir/main")"
        if wait_app_ready "$app_pid" 60; then
            consecutive_crashes=0
            echo '[INFO] 后端已重新拉起。'
        else
            stop_pid "$app_pid"
            app_pid=""
            echo '[ERROR] 后端重新拉起失败。' >&2
        fi
        continue
    fi

    sleep 2
    current_snapshot="$(source_snapshot)" || {
        echo '[WARN] 读取源码失败，2 秒后重试。' >&2
        continue
    }
    if [ "$current_snapshot" = "$last_snapshot" ]; then
        continue
    fi

    # Windows 编辑器保存可能连续触发多次快照变化，稍等稳定再构建。
    sleep 1
    settled_snapshot="$(source_snapshot)" || continue
    if [ "$settled_snapshot" != "$current_snapshot" ]; then
        continue
    fi

    echo '[INFO] 检测到源码变化，重新构建...'
    if ! build_app; then
        echo '[ERROR] 后端构建失败，保留当前进程并将在下一轮重试。' >&2
        continue
    fi

    # 8080 只能有一个监听者：先停旧进程，再启新进程；失败则回滚到上一份二进制。
    stop_app
    mv -f "$runtime_dir/main.next" "$runtime_dir/main"
    app_pid="$(start_app_bin "$runtime_dir/main")"
    if wait_app_ready "$app_pid" 60; then
        cp -f "$runtime_dir/main" "$runtime_dir/main.prev"
        last_snapshot="$settled_snapshot"
        consecutive_crashes=0
        echo '[INFO] 后端热更新完成。'
    else
        echo '[ERROR] 新二进制启动失败，回滚到上一份可用二进制。' >&2
        stop_pid "$app_pid"
        app_pid=""
        if [ -f "$runtime_dir/main.prev" ]; then
            cp -f "$runtime_dir/main.prev" "$runtime_dir/main"
            app_pid="$(start_app_bin "$runtime_dir/main")"
            if wait_app_ready "$app_pid" 60; then
                consecutive_crashes=0
                echo '[INFO] 已回滚并恢复旧进程。'
            else
                stop_pid "$app_pid"
                app_pid=""
                echo '[ERROR] 回滚后仍无法启动。' >&2
            fi
        fi
        # 避免同一坏快照反复构建；等下次文件变化。
        last_snapshot="$settled_snapshot"
    fi
done
