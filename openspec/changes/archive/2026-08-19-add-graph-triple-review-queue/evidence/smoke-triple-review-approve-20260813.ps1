param(
  [string]$Target = 'http://127.0.0.1:18080',
  [string]$Email = 'codex-e2e-ffe6a43a902245778f7793ac8e06ddcd@example.invalid',
  [string]$Password = 'OpenSpecTest1!',
  [string]$PostgresContainer = 'a393146c3baa_WeKnora-postgres',
  [string]$Database = 'weknora_codex_e2e'
)
$ErrorActionPreference = 'Stop'
$base = $Target.TrimEnd('/')
$evidenceDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$logPath = Join-Path $evidenceDir "smoke-triple-review-approve-20260813.log"
$jsonPath = Join-Path $evidenceDir "smoke-triple-review-approve-20260813.json"

function Write-Log([string]$msg) {
  $line = "[{0}] {1}" -f (Get-Date -Format 'o'), $msg
  Write-Output $line
  Add-Content -Path $logPath -Value $line -Encoding utf8
}

if (Test-Path $logPath) { Remove-Item $logPath -Force }

Write-Log "START smoke triple-review approve (pending -> approve)"
Write-Log "TARGET=$base"

$login = Invoke-RestMethod -Method Post -Uri "$base/api/v1/auth/login" -ContentType 'application/json' -Body (@{
  email = $Email
  password = $Password
} | ConvertTo-Json)
$token = [string]$login.token
if ([string]::IsNullOrWhiteSpace($token) -and $null -ne $login.data) {
  $token = [string]$login.data.token
}
if ([string]::IsNullOrWhiteSpace($token)) {
  throw 'login did not return a bearer token'
}
$headers = @{
  Authorization = "Bearer $token"
  'Content-Type' = 'application/json'
}
Write-Log "LOGIN=ok"

$kbId = 'cc7662ad-4960-443f-9b4e-fb57bfa6d52d'
$knowledgeId = '2adb5611-3fcf-44ea-8e4c-9fb4569f3f43'
$chunkId = '78d7333d-7467-42fd-9139-c82bde7c3c9d'
$candidateId = [guid]::NewGuid().ToString()

$graphJson = '{"node":[{"name":"ApproveEntityA","entity_type":"concept","chunks":["' + $chunkId + '"]},{"name":"ApproveEntityB","entity_type":"concept","chunks":["' + $chunkId + '"]}],"relation":[{"node1":"ApproveEntityA","node2":"ApproveEntityB","type":"related_to"}]}'
$graphSql = $graphJson.Replace("'", "''")

$sql = @"
INSERT INTO graph_triple_candidates (
  id, tenant_id, knowledge_base_id, knowledge_id, chunk_id, graph_data, status, created_at
) VALUES (
  '$candidateId', 10000, '$kbId', '$knowledgeId', '$chunkId', '$graphSql'::jsonb, 'pending', NOW()
);
SELECT id, status FROM graph_triple_candidates WHERE id = '$candidateId';
"@

$sqlFile = "/tmp/smoke_triple_approve_$stamp.sql"
$sql | docker exec -i $PostgresContainer tee $sqlFile | Out-Null
$insertOut = docker exec $PostgresContainer psql -U postgres -d $Database -v ON_ERROR_STOP=1 -f $sqlFile 2>&1
Write-Log ("INSERT_OUT=" + (($insertOut | Out-String).Trim()))
if ($LASTEXITCODE -ne 0) {
  throw "psql INSERT failed: $insertOut"
}
Write-Log "INSERT=ok id=$candidateId"

$list = Invoke-RestMethod -Method Get -Uri "$base/api/v1/graph-triple-reviews?status=pending" -Headers $headers
$listIds = @()
if ($null -ne $list.data) {
  $listIds = @($list.data | ForEach-Object { [string]$_.id })
}
$inList = $listIds -contains $candidateId
Write-Log ("LIST_COUNT={0} IN_LIST={1}" -f $listIds.Count, $inList)
if (-not $inList) {
  throw "candidate $candidateId not found in GET /graph-triple-reviews?status=pending"
}

$got = Invoke-RestMethod -Method Get -Uri "$base/api/v1/graph-triple-reviews/$candidateId" -Headers $headers
$gotStatus = [string]$got.data.status
Write-Log "GET_STATUS=$gotStatus"
if ($gotStatus -ne 'pending') {
  throw "expected pending before approve, got $gotStatus"
}

$graphWriteVerified = $false
$graphWriteError = ''
$approveStatus = ''
try {
  $approved = Invoke-RestMethod -Method Post -Uri "$base/api/v1/graph-triple-reviews/$candidateId/approve" -Headers $headers -Body '{}' -ErrorAction Stop
  $approveStatus = [string]$approved.data.status
  Write-Log "APPROVE_STATUS=$approveStatus"
  $graphWriteVerified = ($approveStatus -eq 'written')
} catch {
  $graphWriteError = $_.Exception.Message
  Write-Log "APPROVE_ERROR=$graphWriteError"
  if ($_.ErrorDetails.Message) {
    Write-Log ("APPROVE_BODY=" + $_.ErrorDetails.Message)
    $graphWriteError = $_.ErrorDetails.Message
  }
}

$verify = Invoke-RestMethod -Method Get -Uri "$base/api/v1/graph-triple-reviews/$candidateId" -Headers $headers
$finalStatus = [string]$verify.data.status
Write-Log "VERIFY_STATUS=$finalStatus"

$apiStateChanged = ($finalStatus -in @('written', 'approved'))
$gate = if ($graphWriteVerified) { 'passed' } elseif ($apiStateChanged) { 'partial' } else { 'failed' }

$result = [ordered]@{
  schema = 'graph-triple-review-smoke/v1'
  run_at = (Get-Date).ToString('o')
  target = $base
  candidate_id = $candidateId
  tenant_id = 10000
  knowledge_base_id = $kbId
  knowledge_id = $knowledgeId
  chunk_id = $chunkId
  steps = [ordered]@{
    login = 'pass'
    insert_pending = 'pass'
    list_pending = 'pass'
    get_by_id = 'pass'
    post_approve = if ($graphWriteVerified) { 'pass' } elseif ($graphWriteError) { 'error' } else { 'unknown' }
    verify_status = 'pass'
  }
  final_status = $finalStatus
  graph_write_verified = $graphWriteVerified
  graph_write_error = $graphWriteError
  neo4j_write_validated = $false
  caveat = if (-not $graphWriteVerified) { 'Approve API invoked; Neo4j graph write not independently validated in this smoke.' } else { '' }
  gate = $gate
}
$result | ConvertTo-Json -Depth 6 | Set-Content -Path $jsonPath -Encoding utf8
Write-Log "GATE=$gate"
Write-Log "JSON=$jsonPath"
Write-Output "SMOKE_$gate candidate_id=$candidateId status=$finalStatus graph_write=$graphWriteVerified"
