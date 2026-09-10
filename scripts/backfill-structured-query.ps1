param(
    [UInt64]$Tenant = 0,
    [switch]$AllTenants,
    [int]$Limit = 0,
    [ValidateRange(1, 16)]
    [int]$Concurrency = 4,
    [switch]$DryRun,
    [string]$Knowledge = ""
)

$ErrorActionPreference = 'Stop'
$containerEnvironment = @(
    'DB_HOST=postgres',
    'REDIS_ADDR=redis:6379',
    'DOCREADER_ADDR=docreader:50051',
    'MINIO_ENDPOINT=minio:9000',
    'MILVUS_ADDRESS=milvus:19530',
    'NEO4J_URI=bolt://neo4j:7687',
    'QDRANT_HOST=qdrant',
    'ELASTICSEARCH_ADDR=http://elasticsearch:9200',
    'AUTO_MIGRATE=false',
    'SKIP_TASK_RECONCILIATION=true'
)
$arguments = @('exec')
foreach ($entry in $containerEnvironment) {
    $arguments += @('-e', $entry)
}
$arguments += @('WeKnora-app-dev', 'go', 'run', './cmd/backfill-structured-query', '--limit', $Limit, '--concurrency', $Concurrency)
if ($Tenant -gt 0) {
    $arguments += @('--tenant', $Tenant)
} elseif ($AllTenants) {
    $arguments += '--all-tenants'
} else {
    throw 'Specify -Tenant <id> or -AllTenants explicitly.'
}
if ($DryRun) {
    $arguments += '--dry-run'
}
if ($Knowledge) {
    $arguments += @('--knowledge', $Knowledge)
}
if ((docker inspect -f '{{.State.Running}}' WeKnora-app-dev 2>$null) -ne 'true') {
    throw 'WeKnora-app-dev 未运行，请先执行 quick-dev.bat。'
}
& docker @arguments
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
