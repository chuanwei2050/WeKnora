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

# Docker Compose reads COMPOSE_PROJECT_NAME from .env automatically, while
# child shell scripts only see exported environment variables. Keep both on
# the same project so app-container joins the network backed by the same data
# volumes.
if [ -z "${COMPOSE_PROJECT_NAME:-}" ] && [ -f "$PROJECT_ROOT/.env" ]; then
    configured_project_name="$(sed -n 's/^[[:space:]]*COMPOSE_PROJECT_NAME[[:space:]]*=[[:space:]]*//p' "$PROJECT_ROOT/.env" | tail -n 1 | tr -d '\r')"
    configured_project_name="${configured_project_name%\"}"
    configured_project_name="${configured_project_name#\"}"
    configured_project_name="${configured_project_name%\'}"
    configured_project_name="${configured_project_name#\'}"
    if [[ "$configured_project_name" =~ ^[a-zA-Z0-9][a-zA-Z0-9_-]*$ ]]; then
        export COMPOSE_PROJECT_NAME="$configured_project_name"
    fi
fi

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

use_windows_frontend() {
    use_container_backend && command -v powershell.exe &> /dev/null
}

# 与前端相同：Git Bash 的 nohup/& 仍挂在双击打开的 CMD 作业对象上，
# 关闭窗口会杀掉前台 docker run --rm，导致后端容器一起退出。
use_windows_backend() {
    use_container_backend && command -v powershell.exe &> /dev/null
}

windows_path() {
    local path="$1"

    if command -v wslpath &> /dev/null; then
        wslpath -w "$path"
    else
        cygpath -w "$path"
    fi
}

windows_frontend_is_alive() {
    local frontend_pid="$1"
    local frontend_pid_file
    local bridge_env

    frontend_pid_file="$(windows_path "$PROJECT_ROOT/logs/frontend.pid")"
    bridge_env="${WSLENV:+$WSLENV:}WEKNORA_FRONTEND_PID:WEKNORA_FRONTEND_PID_FILE"

    WSLENV="$bridge_env" \
    WEKNORA_FRONTEND_PID="$frontend_pid" \
    WEKNORA_FRONTEND_PID_FILE="$frontend_pid_file" \
    powershell.exe -NoProfile -NonInteractive -Command '
        $frontendProcessId = 0
        if (-not [int]::TryParse($env:WEKNORA_FRONTEND_PID, [ref]$frontendProcessId)) { exit 1 }
        $frontendProcess = Get-CimInstance Win32_Process -Filter "ProcessId = $frontendProcessId"
        if ($null -eq $frontendProcess -or $frontendProcess.CommandLine -notmatch "npm\.cmd.* run dev") { exit 1 }
        $frontendPidFile = Get-Item $env:WEKNORA_FRONTEND_PID_FILE -ErrorAction SilentlyContinue
        if ($null -eq $frontendPidFile) { exit 1 }
        if ($frontendProcess.CreationDate.ToUniversalTime() -gt $frontendPidFile.LastWriteTimeUtc.AddSeconds(1)) { exit 1 }
    ' > /dev/null 2>&1
}

# PID 文件丢失或校验失败时，仍可能留下占用 5173 的 vite/npm；按端口清理本仓库前端。
free_windows_frontend_port() {
    local frontend_dir
    local bridge_env

    frontend_dir="$(windows_path "$PROJECT_ROOT/frontend")"
    bridge_env="${WSLENV:+$WSLENV:}WEKNORA_FRONTEND_DIR"
    WSLENV="$bridge_env" \
    WEKNORA_FRONTEND_DIR="$frontend_dir" \
    powershell.exe -NoProfile -NonInteractive -Command '
        $frontendDir = [IO.Path]::GetFullPath($env:WEKNORA_FRONTEND_DIR).TrimEnd("\")
        $escapedDir = [regex]::Escape($frontendDir)
        $isOurFrontend = {
            param($cmd)
            if ([string]::IsNullOrEmpty($cmd)) { return $false }
            return ($cmd -match $escapedDir) -and ($cmd -match "vite\.js|run dev")
        }
        $listeners = @()
        try {
            $listeners = Get-NetTCPConnection -LocalPort 5173 -State Listen -ErrorAction Stop
        } catch {
            $listeners = @()
        }
        foreach ($listener in $listeners) {
            $proc = Get-CimInstance Win32_Process -Filter "ProcessId = $($listener.OwningProcess)" -ErrorAction SilentlyContinue
            if ($null -eq $proc) { continue }
            if (-not (& $isOurFrontend $proc.CommandLine)) { continue }
            & taskkill.exe /PID $proc.ProcessId /T /F | Out-Null
        }
        Get-CimInstance Win32_Process -ErrorAction SilentlyContinue |
            Where-Object {
                $_.Name -match "^(node|npm)\.exe$" -and
                (& $isOurFrontend $_.CommandLine)
            } |
            ForEach-Object {
                & taskkill.exe /PID $_.ProcessId /T /F | Out-Null
            }
    ' 2>/dev/null || true
}

# 判断 5173 是否为本仓库 Vite（避免复用其它项目的占用端口）。
windows_frontend_port_is_ours() {
    local frontend_dir
    local bridge_env

    frontend_dir="$(windows_path "$PROJECT_ROOT/frontend")"
    bridge_env="${WSLENV:+$WSLENV:}WEKNORA_FRONTEND_DIR"
    WSLENV="$bridge_env" \
    WEKNORA_FRONTEND_DIR="$frontend_dir" \
    powershell.exe -NoProfile -NonInteractive -Command '
        $frontendDir = [IO.Path]::GetFullPath($env:WEKNORA_FRONTEND_DIR).TrimEnd("\")
        $escapedDir = [regex]::Escape($frontendDir)
        try {
            $listeners = Get-NetTCPConnection -LocalPort 5173 -State Listen -ErrorAction Stop
        } catch {
            exit 1
        }
        foreach ($listener in $listeners) {
            $proc = Get-CimInstance Win32_Process -Filter "ProcessId = $($listener.OwningProcess)" -ErrorAction SilentlyContinue
            if ($null -eq $proc -or [string]::IsNullOrEmpty($proc.CommandLine)) { continue }
            if (($proc.CommandLine -match $escapedDir) -and ($proc.CommandLine -match "vite\.js")) { exit 0 }
        }
        exit 1
    ' > /dev/null 2>&1
}

start_windows_frontend() {
    local frontend_dir
    local frontend_log
    local frontend_error_log
    local frontend_pid_file
    local bridge_env

    frontend_dir="$(windows_path "$PROJECT_ROOT/frontend")"
    frontend_log="$(windows_path "$PROJECT_ROOT/logs/frontend.log")"
    frontend_error_log="$(windows_path "$PROJECT_ROOT/logs/frontend-error.log")"
    frontend_pid_file="$(windows_path "$PROJECT_ROOT/logs/frontend.pid")"
    bridge_env="${WSLENV:+$WSLENV:}WEKNORA_FRONTEND_DIR:WEKNORA_FRONTEND_LOG:WEKNORA_FRONTEND_ERROR_LOG:WEKNORA_FRONTEND_PID_FILE"

    WSLENV="$bridge_env" \
    WEKNORA_FRONTEND_DIR="$frontend_dir" \
    WEKNORA_FRONTEND_LOG="$frontend_log" \
    WEKNORA_FRONTEND_ERROR_LOG="$frontend_error_log" \
    WEKNORA_FRONTEND_PID_FILE="$frontend_pid_file" \
    powershell.exe -NoProfile -NonInteractive -Command '
        $env:VITE_WATCH_USE_POLLING = "true"
        $npmCommand = (Get-Command npm.cmd -ErrorAction Stop).Source
        $frontendOptions = @{
            FilePath = $npmCommand
            ArgumentList = @("run", "dev", "--", "--strictPort")
            WorkingDirectory = $env:WEKNORA_FRONTEND_DIR
            WindowStyle = "Hidden"
            RedirectStandardOutput = $env:WEKNORA_FRONTEND_LOG
            RedirectStandardError = $env:WEKNORA_FRONTEND_ERROR_LOG
            PassThru = $true
        }
        $frontendProcess = Start-Process @frontendOptions
        [IO.File]::WriteAllText($env:WEKNORA_FRONTEND_PID_FILE, $frontendProcess.Id)
    ' > /dev/null 2>&1 &
}

stop_windows_frontend() {
    local pid_file="$1"
    local frontend_pid

    if [ -f "$pid_file" ]; then
        frontend_pid="$(cat "$pid_file" 2>/dev/null || true)"
        if [[ "$frontend_pid" =~ ^[0-9]+$ ]] && windows_frontend_is_alive "$frontend_pid"; then
            log_info "停止上一次的前端进程 (Windows PID: $frontend_pid)..."
            if ! MSYS_NO_PATHCONV=1 taskkill.exe /PID "$frontend_pid" /T /F > /dev/null 2>&1 \
                && windows_frontend_is_alive "$frontend_pid"; then
                log_error "无法停止前端进程 (Windows PID: $frontend_pid)"
                return 1
            fi
        else
            log_info "前端上次进程已退出，清理旧 PID 文件"
        fi
        rm -f "$pid_file"
    fi

    # PID 对不上时仍释放 5173，避免下一次 Vite 报 Port already in use。
    free_windows_frontend_port
}

stop_frontend() {
    local pid_file="$1"

    if use_windows_frontend; then
        stop_windows_frontend "$pid_file"
    else
        stop_previous_process "前端" "$pid_file" "scripts/dev.sh frontend"
    fi
}

windows_backend_container_running() {
    if command -v docker.exe &> /dev/null; then
        docker.exe inspect -f '{{.State.Running}}' WeKnora-app-dev 2>/dev/null | grep -qi '^true$'
    elif command -v docker &> /dev/null; then
        docker inspect -f '{{.State.Running}}' WeKnora-app-dev 2>/dev/null | grep -qi '^true$'
    else
        return 1
    fi
}

remove_windows_backend_container() {
    if command -v docker.exe &> /dev/null; then
        docker.exe rm -f WeKnora-app-dev > /dev/null 2>&1 || true
    elif command -v docker &> /dev/null; then
        docker rm -f WeKnora-app-dev > /dev/null 2>&1 || true
    fi
}

windows_backend_is_alive() {
    local backend_pid="$1"
    local bridge_env

    # 容器已在跑即可视为存活（bash exec 成 docker 后 PID/命令行可能变化）。
    if windows_backend_container_running; then
        return 0
    fi

    # Git Bash/MSYS 下 Process.Start 的 Win32 CommandLine 会一直停留在
    # backend-launch.sh，不会因脚本内 exec 改成 app-container；若只匹配后者，
    # 会在容器尚未起来时误判进程已退出并被 stop_backend 杀掉。
    bridge_env="${WSLENV:+$WSLENV:}WEKNORA_BACKEND_PID"
    WSLENV="$bridge_env" \
    WEKNORA_BACKEND_PID="$backend_pid" \
    powershell.exe -NoProfile -NonInteractive -Command '
        $backendProcessId = 0
        if (-not [int]::TryParse($env:WEKNORA_BACKEND_PID, [ref]$backendProcessId)) { exit 1 }
        $backendProcess = Get-CimInstance Win32_Process -Filter "ProcessId = $backendProcessId"
        if ($null -eq $backendProcess) { exit 1 }
        if ($backendProcess.CommandLine -notmatch "backend-launch\.sh|dev\.sh.*app-container|app-container|WeKnora-app-dev") { exit 1 }
    ' > /dev/null 2>&1
}

start_windows_backend() {
    local bash_exe
    local project_dir
    local backend_pid_file
    local launcher_unix
    local bridge_env

    bash_exe="$(windows_path "$(command -v bash)")"
    project_dir="$(windows_path "$PROJECT_ROOT")"
    backend_pid_file="$(windows_path "$PROJECT_ROOT/logs/backend.pid")"
    launcher_unix="$PROJECT_ROOT/logs/backend-launch.sh"

    # Avoid embedding paths inside powershell/bash -c quote soup (spaces / quotes break).
    # Launch a small script and pass PROJECT_ROOT via the child process environment.
    cat > "$launcher_unix" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
cd "$WEKNORA_PROJECT_ROOT"
mkdir -p "$WEKNORA_PROJECT_ROOT/logs"
exec ./scripts/dev.sh app-container > "$WEKNORA_PROJECT_ROOT/logs/backend.log" 2>&1
EOF

    : > "$PROJECT_ROOT/logs/backend.log"
    rm -f "$PROJECT_ROOT/logs/backend.pid"

    bridge_env="${WSLENV:+$WSLENV:}WEKNORA_BASH_EXE:WEKNORA_BACKEND_DIR:WEKNORA_BACKEND_LAUNCHER:WEKNORA_BACKEND_PID_FILE:WEKNORA_PROJECT_ROOT"
    WSLENV="$bridge_env" \
    WEKNORA_BASH_EXE="$bash_exe" \
    WEKNORA_BACKEND_DIR="$project_dir" \
    WEKNORA_BACKEND_LAUNCHER="$launcher_unix" \
    WEKNORA_BACKEND_PID_FILE="$backend_pid_file" \
    WEKNORA_PROJECT_ROOT="$PROJECT_ROOT" \
    powershell.exe -NoProfile -NonInteractive -Command '
        $psi = New-Object System.Diagnostics.ProcessStartInfo
        $psi.FileName = $env:WEKNORA_BASH_EXE
        $psi.Arguments = [char]34 + $env:WEKNORA_BACKEND_LAUNCHER + [char]34
        $psi.WorkingDirectory = $env:WEKNORA_BACKEND_DIR
        $psi.UseShellExecute = $false
        $psi.CreateNoWindow = $true
        $psi.EnvironmentVariables["WEKNORA_PROJECT_ROOT"] = $env:WEKNORA_PROJECT_ROOT
        $proc = [Diagnostics.Process]::Start($psi)
        if ($null -eq $proc) { exit 1 }
        [IO.File]::WriteAllText($env:WEKNORA_BACKEND_PID_FILE, "$($proc.Id)")
    ' > /dev/null 2>&1
}

stop_windows_backend() {
    local pid_file="$1"
    local backend_pid

    if [ -f "$pid_file" ]; then
        backend_pid="$(cat "$pid_file" 2>/dev/null || true)"
        if [[ "$backend_pid" =~ ^[0-9]+$ ]]; then
            log_info "停止上一次的后端进程 (Windows PID: $backend_pid)..."
            MSYS_NO_PATHCONV=1 taskkill.exe /PID "$backend_pid" /T /F > /dev/null 2>&1 || true
        else
            log_info "后端上次进程已退出，清理旧 PID 文件"
        fi
        rm -f "$pid_file"
    fi

    remove_windows_backend_container
}

stop_backend() {
    local pid_file="$1"

    if use_windows_backend; then
        stop_windows_backend "$pid_file"
    else
        stop_previous_process "后端" "$pid_file" "$BACKEND_MARKER"
        if use_container_backend; then
            remove_windows_backend_container
        fi
    fi
}

process_is_alive() {
    local process_kind="$1"
    local process_pid="$2"

    case "$process_kind" in
        windows)
            windows_frontend_is_alive "$process_pid"
            ;;
        windows-backend)
            windows_backend_is_alive "$process_pid"
            ;;
        *)
            kill -0 "$process_pid" 2>/dev/null
            ;;
    esac
}

wait_for_log_pattern() {
    local name="$1"
    local pid="$2"
    local log_file="$3"
    local pattern="$4"
    local timeout="${5:-60}"
    local process_kind="${6:-unix}"

    for _ in $(seq 1 "$timeout"); do
        if ! process_is_alive "$process_kind" "$pid"; then
            log_error "$name 进程已退出，请查看日志: $log_file"
            return 1
        fi
        if grep -q "$pattern" "$log_file" 2>/dev/null; then
            return 0
        fi
        sleep 1
    done

    log_error "$name 在 ${timeout} 秒内未完成启动，请查看日志: $log_file"
    return 1
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
    local process_kind="${6:-unix}"
    local i

    if ! process_is_alive "$process_kind" "$pid"; then
        log_error "$name 进程已退出，请查看日志: $log_file"
        return 1
    fi

    if ! resolve_curl_bin >/dev/null; then
        log_warning "未检测到 curl，跳过 $name HTTP 就绪检查"
        return 0
    fi

    for i in $(seq 1 "$timeout"); do
        if ! process_is_alive "$process_kind" "$pid"; then
            log_error "$name 进程已退出，请查看日志: $log_file"
            return 1
        fi
        if http_endpoint_ready "$url"; then
            log_success "$name 已就绪: $url"
            return 0
        fi
        if (( i % 15 == 0 )); then
            log_info "仍在等待 $name 就绪 ($i/${timeout}s): $url"
        fi
        sleep 1
    done

    log_error "$name 在 ${timeout} 秒内未就绪，请查看日志: $log_file"
    return 1
}

resolve_curl_bin() {
    if command -v curl.exe &> /dev/null; then
        printf '%s\n' "curl.exe"
        return 0
    fi
    if command -v curl &> /dev/null; then
        printf '%s\n' "curl"
        return 0
    fi
    return 1
}

# 直连探测，避开 Clash/系统代理对 127.0.0.1 的劫持。
http_endpoint_ready() {
    local url="$1"
    local curl_bin

    curl_bin="$(resolve_curl_bin)" || return 1
    HTTP_PROXY= HTTPS_PROXY= ALL_PROXY= http_proxy= https_proxy= all_proxy= \
        "$curl_bin" --proxy "" -fsS --max-time 2 "$url" > /dev/null 2>&1
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
    BACKEND_MARKER="WeKnora-app-dev"
    BACKEND_READY_TIMEOUT=240
fi

case "$ACTION" in
    start)
        ;;
    stop)
        stop_backend "$PROJECT_ROOT/logs/backend.pid"
        frontend_stop_status=0
        stop_frontend "$PROJECT_ROOT/logs/frontend.pid" || frontend_stop_status=$?
        bash "$PROJECT_ROOT/scripts/dev.sh" stop
        services_stop_status=$?
        if [ "$frontend_stop_status" -ne 0 ]; then
            exit "$frontend_stop_status"
        fi
        exit "$services_stop_status"
        ;;
    *)
        log_error "未知命令: $ACTION"
        echo "用法: bash ./scripts/quick-dev.sh [start|stop]"
        exit 1
        ;;
esac

# 启动时不再无条件清空日志：热更新复用旧进程时，截断会丢掉正在写入的日志。

# 1. 启动基础设施
log_info "步骤 1/3: 启动基础设施服务..."
bash "$PROJECT_ROOT/scripts/dev.sh" start
if [ $? -ne 0 ]; then
    log_error "基础设施启动失败"
    exit 1
fi

# 等待依赖网络就绪（复用热更新进程时也只需短暂等待）
log_info "等待服务启动完成..."
sleep 2

# 2. 后端：健康则复用（保留热更新状态），否则重启
echo ""
log_info "步骤 2/3: 启动后端应用..."
BACKEND_PROCESS_KIND="unix"
BACKEND_REUSED=0
if use_windows_backend; then
    BACKEND_PROCESS_KIND="windows-backend"
fi

if http_endpoint_ready "http://127.0.0.1:8080/health"; then
    if use_container_backend && windows_backend_container_running; then
        BACKEND_REUSED=1
    elif [ -s "$PROJECT_ROOT/logs/backend.pid" ]; then
        BACKEND_PID="$(cat "$PROJECT_ROOT/logs/backend.pid" 2>/dev/null || true)"
        if [[ "$BACKEND_PID" =~ ^[0-9]+$ ]] && process_is_alive "$BACKEND_PROCESS_KIND" "$BACKEND_PID"; then
            BACKEND_REUSED=1
        fi
    fi
fi

if [ "$BACKEND_REUSED" -eq 1 ]; then
    BACKEND_PID="$(cat "$PROJECT_ROOT/logs/backend.pid" 2>/dev/null || true)"
    if ! [[ "$BACKEND_PID" =~ ^[0-9]+$ ]]; then
        # 容器热更新可仅靠 WeKnora-app-dev 存活；不要写入哨兵 PID 0。
        rm -f "$PROJECT_ROOT/logs/backend.pid"
        BACKEND_PID=""
    fi
    log_success "复用已运行的后端 (热更新中): http://127.0.0.1:8080/health"
else
    : > "$PROJECT_ROOT/logs/backend.log"
    : > "$PROJECT_ROOT/logs/backend-error.log"
    stop_backend "$PROJECT_ROOT/logs/backend.pid"
    if use_windows_backend; then
        start_windows_backend
        for _ in $(seq 1 10); do
            if [ -s "$PROJECT_ROOT/logs/backend.pid" ]; then
                break
            fi
            sleep 1
        done
        BACKEND_PID="$(cat "$PROJECT_ROOT/logs/backend.pid" 2>/dev/null || true)"
    else
        nohup bash -c 'cd "$1" && exec bash "$1/scripts/dev.sh" "$2"' _ "$PROJECT_ROOT" "$BACKEND_COMMAND" > "$PROJECT_ROOT/logs/backend.log" 2>&1 &
        BACKEND_PID=$!
        echo $BACKEND_PID > "$PROJECT_ROOT/logs/backend.pid"
    fi
    if ! [[ "$BACKEND_PID" =~ ^[0-9]+$ ]]; then
        log_error "后端启动失败，未获得有效 PID，请查看日志: $PROJECT_ROOT/logs/backend.log"
        stop_backend "$PROJECT_ROOT/logs/backend.pid"
        exit 1
    fi
    echo $BACKEND_PID > "$PROJECT_ROOT/logs/backend.pid"
    log_success "后端已在后台启动 (PID: $BACKEND_PID)"
    log_info "查看后端日志: tail -f $PROJECT_ROOT/logs/backend.log"

    if ! wait_for_http "后端" "$BACKEND_PID" "http://127.0.0.1:8080/health" "$PROJECT_ROOT/logs/backend.log" "$BACKEND_READY_TIMEOUT" "$BACKEND_PROCESS_KIND"; then
        stop_backend "$PROJECT_ROOT/logs/backend.pid"
        exit 1
    fi
fi

# 3. 前端：仅复用本仓库 Vite；否则释放端口后重启
echo ""
log_info "步骤 3/3: 启动前端应用..."
FRONTEND_PROCESS_KIND="unix"
FRONTEND_REUSED=0
if use_windows_frontend; then
    FRONTEND_PROCESS_KIND="windows"
fi

if http_endpoint_ready "http://127.0.0.1:5173/"; then
    FRONTEND_PID="$(cat "$PROJECT_ROOT/logs/frontend.pid" 2>/dev/null || true)"
    if use_windows_frontend; then
        if [[ "$FRONTEND_PID" =~ ^[0-9]+$ ]] && windows_frontend_is_alive "$FRONTEND_PID"; then
            FRONTEND_REUSED=1
        elif windows_frontend_port_is_ours; then
            # PID 文件丢失但仍是本仓库 Vite，直接复用。
            FRONTEND_REUSED=1
            FRONTEND_PID=""
            rm -f "$PROJECT_ROOT/logs/frontend.pid"
        fi
    elif [[ "$FRONTEND_PID" =~ ^[0-9]+$ ]] && process_is_alive "$FRONTEND_PROCESS_KIND" "$FRONTEND_PID"; then
        FRONTEND_REUSED=1
    fi
fi

if [ "$FRONTEND_REUSED" -eq 1 ]; then
    log_success "复用已运行的前端 (热更新中): http://127.0.0.1:5173/"
else
    : > "$PROJECT_ROOT/logs/frontend.log"
    : > "$PROJECT_ROOT/logs/frontend-error.log"
    if ! stop_frontend "$PROJECT_ROOT/logs/frontend.pid"; then
        log_error "无法停止上一次的前端进程，已中止启动"
        exit 1
    fi
    if use_windows_frontend; then
        rm -f "$PROJECT_ROOT/logs/frontend.pid"
        start_windows_frontend
        for _ in $(seq 1 10); do
            if [ -s "$PROJECT_ROOT/logs/frontend.pid" ]; then
                break
            fi
            sleep 1
        done
        FRONTEND_PID="$(cat "$PROJECT_ROOT/logs/frontend.pid" 2>/dev/null || true)"
    else
        nohup bash -c 'cd "$1" && exec bash "$1/scripts/dev.sh" frontend' _ "$PROJECT_ROOT" > "$PROJECT_ROOT/logs/frontend.log" 2>&1 &
        FRONTEND_PID=$!
    fi
    if ! [[ "$FRONTEND_PID" =~ ^[0-9]+$ ]]; then
        log_error "前端启动失败，未获得有效 PID，请查看日志: $PROJECT_ROOT/logs/frontend-error.log"
        stop_backend "$PROJECT_ROOT/logs/backend.pid"
        exit 1
    fi
    echo $FRONTEND_PID > "$PROJECT_ROOT/logs/frontend.pid"
    log_success "前端已在后台启动 (PID: $FRONTEND_PID)"
    log_info "查看前端日志: tail -f $PROJECT_ROOT/logs/frontend.log"

    if ! wait_for_http "前端" "$FRONTEND_PID" "http://127.0.0.1:5173/" "$PROJECT_ROOT/logs/frontend.log" 60 "$FRONTEND_PROCESS_KIND"; then
        stop_frontend "$PROJECT_ROOT/logs/frontend.pid"
        stop_backend "$PROJECT_ROOT/logs/backend.pid"
        exit 1
    fi
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
