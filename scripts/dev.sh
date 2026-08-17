#!/bin/bash
# 开发环境启动脚本 - 只启动基础设施，app 和 frontend 需要手动在本地运行

# 设置颜色
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # 无颜色

# 获取项目根目录
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/.." && pwd )"

# 日志函数
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

# 选择可用的 Docker Compose 命令
DOCKER_COMPOSE_BIN=""
DOCKER_COMPOSE_SUBCMD=""
DOCKER_COMPOSE_FILE="docker-compose.dev.yml"
DOCKER_CLI_BIN=""

set_compose_file_path() {
    if [[ "$DOCKER_COMPOSE_BIN" != "docker.exe" && "$DOCKER_COMPOSE_BIN" != "docker-compose.exe" ]]; then
        return 0
    fi

    local compose_src="$PROJECT_ROOT/docker-compose.dev.yml"
    local converted=""

    # Git Bash / MSYS 优先 cygpath，避免误走 wslpath。
    if [[ -n "${MSYSTEM:-}" || "${OSTYPE:-}" == msys* || "${OSTYPE:-}" == cygwin* ]] && command -v cygpath &> /dev/null; then
        converted="$(cygpath -w "$compose_src" 2>/dev/null || true)"
    elif command -v wslpath &> /dev/null; then
        converted="$(wslpath -w "$compose_src" 2>/dev/null || true)"
    elif command -v cygpath &> /dev/null; then
        converted="$(cygpath -w "$compose_src" 2>/dev/null || true)"
    fi

    if [[ -n "$converted" ]]; then
        DOCKER_COMPOSE_FILE="$converted"
    fi
}

# 尝试一组 compose/cli；version 必须可用。daemon 可用则优先采用。
# 返回：0=可用且 daemon 通；1=compose 可用但 daemon 不通；2=完全不可用
try_compose_candidate() {
    local bin="$1"
    local subcmd="$2"
    local cli="$3"

    if [ -n "$subcmd" ]; then
        "$bin" $subcmd version &> /dev/null || return 2
    else
        "$bin" version &> /dev/null || return 2
    fi

    DOCKER_COMPOSE_BIN="$bin"
    DOCKER_COMPOSE_SUBCMD="$subcmd"
    DOCKER_CLI_BIN="$cli"
    DOCKER_COMPOSE_FILE="docker-compose.dev.yml"
    set_compose_file_path

    if [ -z "$cli" ]; then
        return 0
    fi
    if "$cli" info &> /dev/null; then
        return 0
    fi
    return 1
}

detect_compose_cmd() {
    local fallback_bin="" fallback_subcmd="" fallback_cli=""
    local status

    # 候选顺序：原生 docker → docker-compose → Windows .exe（Git Bash / 未集成 WSL）
    local candidates=(
        "docker|compose|docker"
        "docker-compose||"
        "docker.exe|compose|docker.exe"
        "docker-compose.exe||docker.exe"
    )

    local item bin subcmd cli
    for item in "${candidates[@]}"; do
        IFS='|' read -r bin subcmd cli <<< "$item"
        if ! command -v "$bin" &> /dev/null; then
            continue
        fi
        # docker-compose.exe 场景：info 用 docker.exe，但 docker.exe 可能不存在
        if [ -n "$cli" ] && ! command -v "$cli" &> /dev/null; then
            cli=""
        fi

        try_compose_candidate "$bin" "$subcmd" "$cli"
        status=$?
        if [ $status -eq 0 ]; then
            return 0
        fi
        if [ $status -eq 1 ] && [ -z "$fallback_bin" ]; then
            fallback_bin="$DOCKER_COMPOSE_BIN"
            fallback_subcmd="$DOCKER_COMPOSE_SUBCMD"
            fallback_cli="$DOCKER_CLI_BIN"
        fi
    done

    if [ -n "$fallback_bin" ]; then
        DOCKER_COMPOSE_BIN="$fallback_bin"
        DOCKER_COMPOSE_SUBCMD="$fallback_subcmd"
        DOCKER_CLI_BIN="$fallback_cli"
        DOCKER_COMPOSE_FILE="docker-compose.dev.yml"
        set_compose_file_path
        return 0
    fi
    return 1
}

# 显示帮助信息
show_help() {
    printf "%b\n" "${GREEN}WeKnora 开发环境脚本${NC}"
    echo "用法: $0 [命令] [选项]"
    echo ""
    echo "命令:"
    echo "  start      启动基础设施服务（postgres, redis, docreader）"
    echo "  stop       停止所有服务"
    echo "  restart    重启所有服务"
    echo "  logs       查看服务日志"
    echo "  status     查看服务状态"
    echo "  app        启动后端应用（本地运行）"
    echo "  frontend   启动前端开发服务器（本地运行）"
    echo "  help       显示此帮助信息"
    echo ""
    echo "可选 Profile（用于 start 命令）:"
    echo "  --minio    启动 MinIO 对象存储"
    echo "  --qdrant   启动 Qdrant 向量数据库"
    echo "  --neo4j    启动 Neo4j 图数据库"
    echo "  --jaeger   启动 Jaeger 链路追踪"
    echo "  --dex      启动 Dex（OIDC 身份认证）"
    echo "  --full     启动所有可选服务"
    echo ""
    echo "示例："
    echo "  $0 start                    # 启动基础服务"
    echo "  $0 start --qdrant           # 启动基础服务 + Qdrant"
    echo "  $0 start --qdrant --jaeger  # 启动基础服务 + Qdrant + Jaeger"
    echo "  $0 start --dex             # 启动基础服务 + Dex"
    echo "  $0 start --full             # 启动所有服务"
    echo "  $0 app                      # 在另一个终端启动后端"
    echo "  $0 frontend                 # 在另一个终端启动前端"
}

# 检查 Docker
check_docker() {
    if ! detect_compose_cmd; then
        log_error "未检测到可用的 Docker Compose"
        log_error "Windows 请使用 Git Bash / .\\scripts\\quick-dev.ps1，或在 Docker Desktop 中开启当前 WSL 发行版的 WSL Integration"
        return 1
    fi

    if [ -n "$DOCKER_CLI_BIN" ] && ! "$DOCKER_CLI_BIN" info &> /dev/null; then
        log_error "Docker服务未运行"
        return 1
    fi

    return 0
}

# 启动基础设施服务
start_services() {
    log_info "启动开发环境基础设施服务..."
    
    check_docker
    if [ $? -ne 0 ]; then
        return 1
    fi
    
    cd "$PROJECT_ROOT"
    
    # 检查 .env 文件
    if [ ! -f ".env" ]; then
        log_error ".env 文件不存在，请先创建"
        return 1
    fi
    
    # 解析 profile 参数
    shift  # 移除 "start" 命令本身
    PROFILES="--profile full"
    ENABLED_SERVICES=""
    
    while [ $# -gt 0 ]; do
        case "$1" in
            --minio)
                PROFILES="$PROFILES --profile minio"
                ENABLED_SERVICES="$ENABLED_SERVICES minio"
                ;;
            --qdrant)
                PROFILES="$PROFILES --profile qdrant"
                ENABLED_SERVICES="$ENABLED_SERVICES qdrant"
                ;;
            --neo4j)
                PROFILES="$PROFILES --profile neo4j"
                ENABLED_SERVICES="$ENABLED_SERVICES neo4j"
                ;;
            --jaeger)
                PROFILES="$PROFILES --profile jaeger"
                ENABLED_SERVICES="$ENABLED_SERVICES jaeger"
                ;;
            --dex)
                PROFILES="$PROFILES --profile dex"
                ENABLED_SERVICES="$ENABLED_SERVICES dex"
                ;;
            --full)
                PROFILES="--profile full"
                ENABLED_SERVICES="minio qdrant neo4j jaeger dex"
                break
                ;;
            *)
                log_warning "未知参数: $1"
                ;;
        esac
        shift
    done
    
    # 启动服务
    "$DOCKER_COMPOSE_BIN" $DOCKER_COMPOSE_SUBCMD -f "$DOCKER_COMPOSE_FILE" $PROFILES up -d
    
    if [ $? -eq 0 ]; then
        log_success "基础设施服务已启动"
        echo ""
        log_info "服务访问地址:"
        echo "  - PostgreSQL:    localhost:5432"
        echo "  - Redis:         localhost:6379"
        echo "  - DocReader:     localhost:50051"
        
        # 根据启用的 profile 显示额外服务
        if [[ "$ENABLED_SERVICES" == *"minio"* ]]; then
            echo "  - MinIO:         localhost:9000 (Console: localhost:9001)"
        fi
        if [[ "$ENABLED_SERVICES" == *"qdrant"* ]]; then
            echo "  - Qdrant:        localhost:6333 (gRPC: localhost:6334)"
        fi
        if [[ "$ENABLED_SERVICES" == *"neo4j"* ]]; then
            echo "  - Neo4j:         localhost:7474 (Bolt: localhost:7687)"
        fi
        if [[ "$ENABLED_SERVICES" == *"jaeger"* ]]; then
            echo "  - Jaeger:        localhost:16686"
        fi
        if [[ "$ENABLED_SERVICES" == *"dex"* ]]; then
            echo "  - Dex:           localhost:5556"
        fi
        
        echo ""
        log_info "接下来的步骤:"
        printf "%b\n" "${YELLOW}1. 在新终端运行后端:${NC} make dev-app"
        printf "%b\n" "${YELLOW}2. 在新终端运行前端:${NC} make dev-frontend"
        return 0
    else
        log_error "服务启动失败"
        return 1
    fi
}

# 停止服务
stop_services() {
    log_info "停止开发环境服务..."
    
    check_docker
    if [ $? -ne 0 ]; then
        return 1
    fi
    
    cd "$PROJECT_ROOT"
    "$DOCKER_COMPOSE_BIN" $DOCKER_COMPOSE_SUBCMD -f "$DOCKER_COMPOSE_FILE" down
    
    if [ $? -eq 0 ]; then
        log_success "所有服务已停止"
        return 0
    else
        log_error "服务停止失败"
        return 1
    fi
}

# 重启服务
restart_services() {
    stop_services
    sleep 2
    start_services
}

# 查看日志
show_logs() {
    check_docker || return 1
    cd "$PROJECT_ROOT"
    "$DOCKER_COMPOSE_BIN" $DOCKER_COMPOSE_SUBCMD -f "$DOCKER_COMPOSE_FILE" logs -f
}

# 查看状态
show_status() {
    check_docker || return 1
    cd "$PROJECT_ROOT"
    "$DOCKER_COMPOSE_BIN" $DOCKER_COMPOSE_SUBCMD -f "$DOCKER_COMPOSE_FILE" ps
}

# 启动后端应用（本地）
start_app() {
    log_info "启动后端应用（本地开发模式）..."
    
    cd "$PROJECT_ROOT"
    
    # 兼容 WSL：Windows 安装的 Go 可能只以 go.exe 暴露
    local go_bin=""
    if command -v go &> /dev/null; then
        go_bin="go"
    elif command -v go.exe &> /dev/null; then
        go_bin="go.exe"
    else
        log_error "Go 未安装"
        return 1
    fi
    
    # 加载环境变量（使用 set -a 确保所有变量都被导出）
    if [ -f ".env" ]; then
        log_info "加载 .env 文件..."
        set -a
        # Windows 编辑器生成的 .env 可能包含 UTF-8 BOM 和 CRLF，先清理后再加载。
        local env_bom=$'\xef\xbb\xbf'
        source <(sed -e "1s/^${env_bom}//" -e 's/\r$//' .env)
        set +a
    else
        log_error ".env 文件不存在，请先创建配置文件"
        return 1
    fi
    
    # 设置本地开发环境变量（覆盖 Docker 容器地址）
    export DB_HOST=localhost
    export DOCREADER_ADDR=localhost:50051
    export DOCREADER_TRANSPORT=grpc
    export MINIO_ENDPOINT=localhost:9000
    export REDIS_ADDR=localhost:6379
    export MILVUS_ADDRESS=localhost:19530
    export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317
    export NEO4J_URI=bolt://localhost:7687
    export QDRANT_HOST=localhost
    
    # 确保必要的环境变量已设置
    if [ -z "$DB_DRIVER" ]; then
        log_error "DB_DRIVER 环境变量未设置，请检查 .env 文件"
        return 1
    fi
    
    log_info "环境变量已设置，启动应用..."
    log_info "数据库地址: $DB_HOST:${DB_PORT:-5432}"
    
    export CGO_CFLAGS="-Wno-deprecated-declarations -Wno-gnu-folding-constant"
    if [[ "$(uname)" == "Darwin" ]]; then
      export CGO_LDFLAGS="-Wl,-no_warn_duplicate_libraries"
    fi

    # 检查是否安装了 Air（热重载工具）
    local air_bin=""
    if [ "$go_bin" = "go" ] && command -v air &> /dev/null; then
        air_bin="air"
    fi
    if [ -n "$air_bin" ]; then
        log_success "检测到 Air，使用热重载模式启动..."
        log_info "修改 Go 代码后将自动重新编译和重启"
        "$air_bin"
    else
        log_info "未检测到 Air，使用普通模式启动"
        log_warning "提示: 安装 Air 可以实现代码修改后自动重启"
        log_info "安装命令: go install github.com/air-verse/air@latest"
        LDFLAGS="$(./scripts/get_version.sh ldflags) -X 'google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn'"
        "$go_bin" run -ldflags="$LDFLAGS" ./cmd/server
    fi
}

# 启动前端（本地）
start_frontend() {
    log_info "启动前端开发服务器..."
    
    cd "$PROJECT_ROOT/frontend"
    
    # 检查 npm 是否安装
    if ! command -v npm &> /dev/null; then
        log_error "npm 未安装"
        return 1
    fi
    
    # 检查依赖是否已安装
    if [ ! -d "node_modules" ]; then
        log_warning "node_modules 不存在，正在安装依赖..."
        npm install
    fi
    
    log_info "启动 Vite 开发服务器..."
    log_info "前端将运行在 http://localhost:5173"
    
    # 运行开发服务器
    npm run dev -- --strictPort
}

# 解析命令
CMD="${1:-help}"
case "$CMD" in
    start)
        start_services "$@"
        ;;
    stop)
        stop_services
        ;;
    restart)
        restart_services
        ;;
    logs)
        show_logs
        ;;
    status)
        show_status
        ;;
    app)
        start_app
        ;;
    frontend)
        start_frontend
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        log_error "未知命令: $CMD"
        show_help
        exit 1
        ;;
esac

exit $?
