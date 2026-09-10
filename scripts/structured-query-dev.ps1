param(
    [ValidateSet('start', 'stop')]
    [string]$Action = 'start'
)

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Split-Path -Parent $PSScriptRoot)).Path
$runtimeDir = Join-Path $projectRoot 'tmp\structured-query'
$logDir = Join-Path $projectRoot 'logs'
$apiPidFile = Join-Path $runtimeDir 'api.pid'
$workerPidFile = Join-Path $runtimeDir 'worker.pid'
$python = Join-Path $projectRoot 'structured-query\.venv312\Scripts\python.exe'

function Read-DotEnv([string]$Path) {
    $values = @{}
    if (-not (Test-Path -LiteralPath $Path)) { return $values }
    foreach ($line in [IO.File]::ReadAllLines($Path, [Text.Encoding]::UTF8)) {
        $trimmed = $line.Trim()
        if (-not $trimmed -or $trimmed.StartsWith('#')) { continue }
        $separator = $trimmed.IndexOf('=')
        if ($separator -le 0) { continue }
        $name = $trimmed.Substring(0, $separator).Trim()
        $value = $trimmed.Substring($separator + 1).Trim()
        if (($value.StartsWith('"') -and $value.EndsWith('"')) -or
            ($value.StartsWith("'") -and $value.EndsWith("'"))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        $values[$name] = $value
    }
    return $values
}

function Stop-ManagedProcess([string]$Name, [string]$PidFile, [string]$Marker) {
    if (-not (Test-Path -LiteralPath $PidFile)) { return }
    $processId = 0
    $rawPid = (Get-Content -LiteralPath $PidFile -Raw).Trim()
    if (-not [int]::TryParse($rawPid, [ref]$processId)) {
        Remove-Item -LiteralPath $PidFile -Force
        return
    }
    $process = Get-CimInstance Win32_Process -Filter "ProcessId = $processId" -ErrorAction SilentlyContinue
    if ($null -ne $process -and $process.CommandLine -match $Marker) {
        Write-Host "停止 $Name (PID: $processId)..."
        & taskkill.exe /PID $processId /T /F | Out-Null
    }
    Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
}

function Wait-ForHttp([string]$Url, [int]$TimeoutSeconds) {
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    while ([DateTime]::UtcNow -lt $deadline) {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 -Uri $Url
            if ($response.StatusCode -eq 200) { return $true }
        } catch { }
        Start-Sleep -Milliseconds 500
    }
    return $false
}

if ($Action -eq 'stop') {
    Stop-ManagedProcess '结构化查询 worker' $workerPidFile 'structured_query worker'
    Stop-ManagedProcess '结构化查询 API' $apiPidFile 'structured_query.api:app'
    exit 0
}

if (-not (Test-Path -LiteralPath $python)) {
    throw "缺少结构化查询 Python 环境: $python。请先在 structured-query 目录创建 .venv312 并安装依赖。"
}

$config = Read-DotEnv (Join-Path $projectRoot '.env')
$required = @('DB_USER', 'DB_PASSWORD', 'DB_NAME', 'MINIO_ACCESS_KEY_ID', 'MINIO_SECRET_ACCESS_KEY', 'MINIO_BUCKET_NAME', 'WEKNORA_STRUCTURED_QUERY_API_KEY')
foreach ($name in $required) {
    if (-not $config[$name]) { throw ".env 缺少必需配置: $name" }
}

New-Item -ItemType Directory -Force -Path $runtimeDir, $logDir | Out-Null
Stop-ManagedProcess '结构化查询 worker' $workerPidFile 'structured_query worker'
Stop-ManagedProcess '结构化查询 API' $apiPidFile 'structured_query.api:app'

$dbHost = if ($config['DB_HOST'] -and $config['DB_HOST'] -notin @('localhost', 'postgres', 'WeKnora-postgres-dev')) { $config['DB_HOST'] } else { '127.0.0.1' }
$dbPort = if ($config['DB_PORT']) { $config['DB_PORT'] } else { '5432' }
$escapedUser = [Uri]::EscapeDataString($config['DB_USER'])
$escapedPassword = [Uri]::EscapeDataString($config['DB_PASSWORD'])
$escapedDatabase = [Uri]::EscapeDataString($config['DB_NAME'])
$redisPassword = $config['REDIS_PASSWORD']
$redisAuth = if ($redisPassword) { ':' + [Uri]::EscapeDataString($redisPassword) + '@' } else { '' }
$redisDb = if ($config['REDIS_DB']) { $config['REDIS_DB'] } else { '0' }
$serviceKey = $config['WEKNORA_STRUCTURED_QUERY_API_KEY']

$processEnv = @{
    PYTHONPATH = (Join-Path $projectRoot 'structured-query\src')
    STRUCTURED_QUERY_POSTGRES_DSN = "postgresql+psycopg://${escapedUser}:${escapedPassword}@${dbHost}:${dbPort}/${escapedDatabase}"
    STRUCTURED_QUERY_REDIS_URL = "redis://${redisAuth}127.0.0.1:6379/${redisDb}"
    STRUCTURED_QUERY_S3_ENDPOINT = 'http://127.0.0.1:9000'
    STRUCTURED_QUERY_S3_ACCESS_KEY = $config['MINIO_ACCESS_KEY_ID']
    STRUCTURED_QUERY_S3_SECRET_KEY = $config['MINIO_SECRET_ACCESS_KEY']
    STRUCTURED_QUERY_S3_BUCKET = $config['MINIO_BUCKET_NAME']
    STRUCTURED_QUERY_MILVUS_URI = 'http://127.0.0.1:19530'
    STRUCTURED_QUERY_WEKNORA_BASE_URL = 'http://127.0.0.1:8080'
    STRUCTURED_QUERY_WEKNORA_SERVICE_KEY = $serviceKey
    STRUCTURED_QUERY_API_KEYS = "${serviceKey}:service"
}

$common = @{
    FilePath = $python
    WorkingDirectory = (Join-Path $projectRoot 'structured-query')
    WindowStyle = 'Hidden'
    Environment = $processEnv
    PassThru = $true
}
$api = Start-Process @common -ArgumentList @('-m', 'uvicorn', 'structured_query.api:app', '--host', '127.0.0.1', '--port', '8090') `
    -RedirectStandardOutput (Join-Path $logDir 'structured-query-api.log') `
    -RedirectStandardError (Join-Path $logDir 'structured-query-api-error.log')
[IO.File]::WriteAllText($apiPidFile, [string]$api.Id)

if (-not (Wait-ForHttp 'http://127.0.0.1:8090/v1/health/live' 60)) {
    Stop-ManagedProcess '结构化查询 API' $apiPidFile 'structured_query.api:app'
    throw "结构化查询 API 未就绪，请查看 $logDir\structured-query-api-error.log"
}
Start-Sleep -Milliseconds 500
$api.Refresh()
if ($api.HasExited) {
    Remove-Item -LiteralPath $apiPidFile -Force -ErrorAction SilentlyContinue
    throw "结构化查询 API 进程已退出（端口 8090 可能被其他进程占用），请查看 $logDir\structured-query-api-error.log"
}

$worker = Start-Process @common -ArgumentList @('-m', 'structured_query', 'worker') `
    -RedirectStandardOutput (Join-Path $logDir 'structured-query-worker.log') `
    -RedirectStandardError (Join-Path $logDir 'structured-query-worker-error.log')
[IO.File]::WriteAllText($workerPidFile, [string]$worker.Id)
Start-Sleep -Seconds 2
if ($worker.HasExited) {
    Remove-Item -LiteralPath $workerPidFile -Force -ErrorAction SilentlyContinue
    throw "结构化查询 worker 启动失败，请查看 $logDir\structured-query-worker-error.log"
}

Write-Host '结构化查询 API 已就绪: http://127.0.0.1:8090'
Write-Host "结构化查询 worker 已启动 (PID: $($worker.Id))"
