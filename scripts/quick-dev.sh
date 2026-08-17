#!/bin/bash
# 快速启动开发环境的一键脚本
# 此脚本会在一个终端中启动所有必需的服务

# 设置颜色
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # 无颜色

# 获取项目根目录
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/.." && pwd )"

log_info() {
    printf "%b\n" "${BLUE}[INFO]${NC} $1"
}

log_success() {
    printf "%b\n" "${GREEN}[SUCCESS]${NC} $1"
}

log_error() {
    printf "%b\n" "${RED}[ERROR]${NC} $1"
}

log_warning() {
    printf "%b\n" "${YELLOW}[WARNING]${NC} $1"
}

use_container_backend() {
    case "$(uname -s)" in
        MINGW*|MSYS*|CYGWIN*)
            return 0
            ;;
    esac
    grep -qiE '(microsoft|wsl)' /proc/version 2> /dev/null
}

# 停止上一次由本脚本启动的本地进程，避免端口冲突。
stop_process_tree() {
    local pid="$1"
    local child_pid
    local child_pids

    if ! kill -0 "$pid" 2>/dev/null; then
        return 0
    fi

    child_pids="$(ps -eo pid=,ppid= 2>/dev/null | awk -v parent="$pid" '$2 == parent {print $1}')"
    for child_pid in $child_pids; do
        stop_process_tree "$child_pid"
    done

    kill "$pid" 2>/dev/null || true
    for _ in 1 2 3 4 5; do
        if ! kill -0 "$pid" 2>/dev/null; then
            return 0
        fi
        sleep 1
    done
    kill -9 "$pid" 2>/dev/null || true
}

stop_previous_process() {
    local name="$1"
    local pid_file="$2"
    local marker="$3"
    local pid
    local command

    if [ ! -f "$pid_file" ]; then
        return 0
    fi

    pid="$(cat "$pid_file" 2>/dev/null || true)"
    if ! [[ "$pid" =~ ^[0-9]+$ ]]; then
        log_warning "$name PID 文件无效，已清理: $pid_file"
        rm -f "$pid_file"
        return 0
    fi

    command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
    if [ -z "$command" ]; then
        log_info "$name 上次进程已退出，清理旧 PID 文件"
        rm -f "$pid_file"
        return 0
    fi

    if [[ "$command" != *"$marker"* ]]; then
        log_warning "$name PID $pid 不是本脚本启动的进程，跳过停止"
        rm -f "$pid_file"
        return 0
    fi

    log_info "停止上一次的 $name 进程 (PID: $pid)..."
    stop_process_tree "$pid"
    rm -f "$pid_file"
}

wait_for_http() {
    local name="$1"
    local pid="$2"
    local url="$3"
    local log_file="$4"
    local timeout="${5:-60}"

    if ! kill -0 "$pid" 2>/dev/null; then
        log_error "$name 进程已退出，请查看日志: $log_file"
        return 1
    fi

    if ! command -v curl &> /dev/null; then
        log_warning "未检测到 curl，跳过 $name HTTP 就绪检查"
        return 0
    fi

    for _ in $(seq 1 "$timeout"); do
        if ! kill -0 "$pid" 2>/dev/null; then
            log_error "$name 进程已退出，请查看日志: $log_file"
            return 1
        fi
        if curl -fsS --max-time 2 "$url" > /dev/null 2>&1; then
            log_success "$name 已就绪: $url"
            return 0
        fi
        sleep 1
    done

    log_error "$name 在 ${timeout} 秒内未就绪，请查看日志: $log_file"
    return 1
}

echo ""
printf "%b\n" "${GREEN}========================================${NC}"
printf "%b\n" "${GREEN}  WeKnora 快速开发环境启动${NC}"
printf "%b\n" "${GREEN}========================================${NC}"
echo ""

# 检查是否在项目根目录
cd "$PROJECT_ROOT"

# 创建后台服务的日志和 PID 目录
mkdir -p "$PROJECT_ROOT/logs" "$PROJECT_ROOT/tmp"

ACTION="${1:-start}"
BACKEND_COMMAND="app"
BACKEND_MARKER="scripts/dev.sh app"
BACKEND_READY_TIMEOUT=60
if use_container_backend; then
    BACKEND_COMMAND="app-container"
    BACKEND_MARKER="scripts/dev.sh app-container"
    BACKEND_READY_TIMEOUT=240
fi

case "$ACTION" in
    start)
        ;;
    stop)
        stop_previous_process "后端" "$PROJECT_ROOT/tmp/backend.pid" "$BACKEND_MARKER"
        stop_previous_process "前端" "$PROJECT_ROOT/tmp/frontend.pid" "scripts/dev.sh frontend"
        bash "$PROJECT_ROOT/scripts/dev.sh" stop
        exit $?
        ;;
    *)
        log_error "未知命令: $ACTION"
        echo "用法: bash ./scripts/quick-dev.sh [start|stop]"
        exit 1
        ;;
esac

# 每次启动前清空旧日志，避免本次未启动的服务留下上一轮的误导信息。
: > "$PROJECT_ROOT/logs/backend.log"
: > "$PROJECT_ROOT/logs/frontend.log"

# 1. 启动基础设施
log_info "步骤 1/3: 启动基础设施服务..."
bash "$PROJECT_ROOT/scripts/dev.sh" start
if [ $? -ne 0 ]; then
    log_error "基础设施启动失败"
    exit 1
fi

# 仅在依赖启动成功后停止本脚本上次记录的后端和前端，避免依赖失败时破坏可用的本地服务。
stop_previous_process "后端" "$PROJECT_ROOT/tmp/backend.pid" "$BACKEND_MARKER"
stop_previous_process "前端" "$PROJECT_ROOT/tmp/frontend.pid" "scripts/dev.sh frontend"

# 等待服务就绪
log_info "等待服务启动完成..."
sleep 5

# 2. 自动启动后端
echo ""
log_info "步骤 2/3: 启动后端应用..."
nohup bash -c 'cd "$1" && exec bash "$1/scripts/dev.sh" "$2"' _ "$PROJECT_ROOT" "$BACKEND_COMMAND" > "$PROJECT_ROOT/logs/backend.log" 2>&1 &
BACKEND_PID=$!
echo $BACKEND_PID > "$PROJECT_ROOT/tmp/backend.pid"
log_success "后端已在后台启动 (PID: $BACKEND_PID)"
log_info "查看后端日志: tail -f $PROJECT_ROOT/logs/backend.log"

if ! wait_for_http "后端" "$BACKEND_PID" "http://127.0.0.1:8080/health" "$PROJECT_ROOT/logs/backend.log" "$BACKEND_READY_TIMEOUT"; then
    stop_previous_process "后端" "$PROJECT_ROOT/tmp/backend.pid" "$BACKEND_MARKER"
    exit 1
fi

# 3. 自动启动前端（后端就绪后再启动，避免后端失败时额外占用前端端口）
echo ""
log_info "步骤 3/3: 启动前端应用..."
nohup bash -c 'cd "$1" && exec bash "$1/scripts/dev.sh" frontend' _ "$PROJECT_ROOT" > "$PROJECT_ROOT/logs/frontend.log" 2>&1 &
FRONTEND_PID=$!
echo $FRONTEND_PID > "$PROJECT_ROOT/tmp/frontend.pid"
log_success "前端已在后台启动 (PID: $FRONTEND_PID)"
log_info "查看前端日志: tail -f $PROJECT_ROOT/logs/frontend.log"

if ! wait_for_http "前端" "$FRONTEND_PID" "http://127.0.0.1:5173/" "$PROJECT_ROOT/logs/frontend.log"; then
    stop_previous_process "前端" "$PROJECT_ROOT/tmp/frontend.pid" "scripts/dev.sh frontend"
    stop_previous_process "后端" "$PROJECT_ROOT/tmp/backend.pid" "$BACKEND_MARKER"
    exit 1
fi

# 显示总结
echo ""
printf "%b\n" "${GREEN}========================================${NC}"
printf "%b\n" "${GREEN}  启动完成！${NC}"
printf "%b\n" "${GREEN}========================================${NC}"
echo ""

log_info "访问地址:"
echo "  - 前端: http://localhost:5173"
echo "  - 后端 API: http://localhost:8080"
echo "  - MinIO Console: http://localhost:9001"
echo "  - Jaeger UI: http://localhost:16686"
echo ""

log_info "管理命令:"
echo "  - 查看服务状态: bash ./scripts/dev.sh status"
echo "  - 查看依赖日志: bash ./scripts/dev.sh logs"
echo "  - 停止所有服务: bash ./scripts/quick-dev.sh stop"
echo ""

log_warning "停止后台进程:"
echo "  - 推荐执行: bash ./scripts/quick-dev.sh stop"

echo ""
log_success "开发环境已就绪，开始编码吧！"
echo ""
