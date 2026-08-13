param(
  [Parameter(Mandatory=$true)][string]$FrozenInputsFile,
  [Parameter(Mandatory=$true)][string]$SuiteFile,
  [ValidateSet('desktop-lite','compose-airgap','helm-airgap')][string]$Profile = 'compose-airgap',
  [string]$DeploymentDirectory,
  [string]$Target,
  [string]$TokenFile,
  [string]$FormalTokenFile,
  [string]$OutboundAuditFile,
  [string]$RunId = ('offline-' + [DateTime]::UtcNow.ToString('yyyyMMddHHmmss')),
  [int]$TTFTLimitMs = 15000,
  [int]$TimeoutSeconds = 120,
  [switch]$ConfirmTestEnvironment,
  [switch]$DryRun,
  [string]$OutputFile
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path

function Fail([string]$Message) { throw $Message }
function Resolve-Repo([string]$Path) {
  $candidate = if ([System.IO.Path]::IsPathRooted($Path)) { $Path } else { Join-Path $root $Path }
  return (Resolve-Path -LiteralPath $candidate).Path
}
function Add-Check([System.Collections.Generic.List[object]]$Checks, [string]$Name, [string]$Status, [string]$Details, $Data = $null) {
  $Checks.Add([ordered]@{ name = $Name; status = $Status; details = $Details; data = $Data })
}
function Get-Hash([string]$Path) { return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant() }

if (-not (Test-Path -LiteralPath $FrozenInputsFile -PathType Leaf)) { Fail "FrozenInputsFile 不存在: $FrozenInputsFile" }
if (-not (Test-Path -LiteralPath $SuiteFile -PathType Leaf)) { Fail "SuiteFile 不存在: $SuiteFile" }
$frozenPath = (Resolve-Path -LiteralPath $FrozenInputsFile).Path
$suitePath = (Resolve-Path -LiteralPath $SuiteFile).Path
$frozen = Get-Content -Raw -Encoding UTF8 $frozenPath | ConvertFrom-Json
$suite = Get-Content -Raw -Encoding UTF8 $suitePath | ConvertFrom-Json
if ($frozen.schema -ne 'weknora-acceptance-frozen-inputs/v1') { Fail '冻结输入 schema 不受支持。' }
if ([string]::IsNullOrWhiteSpace([string]$frozen.freeze_sha256)) { Fail '冻结输入缺少 freeze_sha256。' }
$rootPath = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$expectedSuite = [System.IO.Path]::GetFullPath((Join-Path $rootPath ([string]$frozen.files.acceptance_suite.path).Replace('/','\')))
if ($suitePath -ne $expectedSuite -or (Get-Hash $suitePath) -ne [string]$frozen.files.acceptance_suite.sha256) {
  Fail 'SuiteFile 与冻结输入清单不一致。'
}

$checks = [System.Collections.Generic.List[object]]::new()
$startedAt = [DateTimeOffset]::UtcNow
$manifest = $null
if ($DeploymentDirectory) {
  $manifestPath = Join-Path (Resolve-Repo $DeploymentDirectory) 'manifest.json'
  if (Test-Path -LiteralPath $manifestPath -PathType Leaf) {
    $manifest = Get-Content -Raw -Encoding UTF8 $manifestPath | ConvertFrom-Json
  } else { Add-Check $checks 'deployment_manifest' 'blocked' '部署目录缺少 manifest.json。' }
}

if ($null -ne $manifest) {
  $locations = @($manifest.components | ForEach-Object { [pscustomobject]@{ name = [string]$_.name; location = [string]$_.location } })
  $locations += @($manifest.models | ForEach-Object { [pscustomobject]@{ name = "model:$($_.model_name)"; location = [string]$_.location } })
  $nonSameHost = @($locations | Where-Object { $_.location -ne 'same-host' })
  $expectedRoles = @($frozen.models.PSObject.Properties.Name)
  $declaredRoles = @($manifest.models | ForEach-Object { [string]$_.role })
  $missingRoles = @($expectedRoles | Where-Object { $_ -notin $declaredRoles })
  $singleStatus = if ($missingRoles.Count -gt 0) { 'blocked' } elseif ($nonSameHost.Count -eq 0) { 'passed' } else { 'failed' }
  $details = if ($missingRoles.Count -gt 0) { "profile=$Profile；模型角色清单缺失：$($missingRoles -join ', ')。" } else { "profile=$Profile；位置不符组件数=$($nonSameHost.Count)。" }
  Add-Check $checks 'single_node_gate' $singleStatus $details ([ordered]@{ profile = $Profile; non_same_host = @($nonSameHost); missing_model_roles = @($missingRoles) })
} else {
  Add-Check $checks 'single_node_gate' 'blocked' '未提供可审计部署清单，不能计算 single-node。'
}

if ($DeploymentDirectory) {
  $preflight = Join-Path $PSScriptRoot 'airgap-preflight.ps1'
  try {
    & $preflight -DeploymentDirectory (Resolve-Repo $DeploymentDirectory) -Profile $Profile | Out-Null
    Add-Check $checks 'airgap_preflight' 'passed' '离线介质清单、位置和校验和预检通过。'
  } catch { Add-Check $checks 'airgap_preflight' 'failed' $_.Exception.Message }
} else { Add-Check $checks 'airgap_preflight' 'blocked' '未提供离线包目录。' }

if ($OutboundAuditFile) {
  if (-not (Test-Path -LiteralPath $OutboundAuditFile -PathType Leaf)) { Add-Check $checks 'outbound_audit' 'failed' '出站审计文件不存在。' }
  else {
    $audit = Get-Content -Raw -Encoding UTF8 $OutboundAuditFile | ConvertFrom-Json
    $auditPassed = ([string]$audit.schema -eq 'weknora-outbound-audit/v1' -and [string]$audit.status -eq 'passed' -and [int]$audit.successful_public_connections -eq 0)
    Add-Check $checks 'outbound_audit' $(if ($auditPassed) { 'passed' } else { 'failed' }) '出站审计必须证明成功公网连接数为 0。' $audit
  }
} else { Add-Check $checks 'outbound_audit' 'blocked' '未提供网络层出站审计结果。' }

$formalReport = $null
$formalTokenPath = if (-not [string]::IsNullOrWhiteSpace($FormalTokenFile)) { $FormalTokenFile } else { $TokenFile }
if ($Target -and $formalTokenPath -and $ConfirmTestEnvironment -and -not $DryRun) {
  $formalOutput = Join-Path ([System.IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString('N') + '-formal.json')
  try {
    & (Join-Path $PSScriptRoot 'formal-acceptance-suite.ps1') -SuiteFile $suitePath -Target $Target -TokenFile $formalTokenPath -RunId $RunId -Profile $Profile -TTFTLimitMs $TTFTLimitMs -TimeoutSeconds $TimeoutSeconds -ConfirmTestEnvironment -FrozenInputsFile $frozenPath -OutputFile $formalOutput | Out-Null
  } catch { }
  if (Test-Path -LiteralPath $formalOutput) { $formalReport = Get-Content -Raw -Encoding UTF8 $formalOutput | ConvertFrom-Json }
  if ($null -ne $formalReport) { Add-Check $checks 'frozen_e2e_suite' ([string]$formalReport.gate) '统一正式验收执行器结果。' $formalReport }
  else { Add-Check $checks 'frozen_e2e_suite' 'failed' '正式验收执行器未生成报告。' }
  Remove-Item -LiteralPath $formalOutput -Force -ErrorAction SilentlyContinue
} elseif ($DryRun) {
  Add-Check $checks 'frozen_e2e_suite' 'blocked' 'dry-run 未执行真实离线 RAG/Agent。'
} else { Add-Check $checks 'frozen_e2e_suite' 'blocked' '缺少 Target、TokenFile 或 ConfirmTestEnvironment。' }
$serverLoad = $null
if ($Profile -eq 'desktop-lite') {
  Add-Check $checks 'server_load_gate' 'not_applicable' 'desktop-lite 不执行服务器负载门禁；该结果不能替代 single-node。'
} elseif ($Target -and $TokenFile -and $ConfirmTestEnvironment -and -not $DryRun) {
  $loadOutput = Join-Path ([System.IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString('N') + '-load.json')
  try {
    & (Join-Path $PSScriptRoot 'acceptance-benchmark.ps1') -Target $Target -Users 50 -Concurrent 10 -DurationSeconds 60 -TTFTLimitMs $TTFTLimitMs -ConfirmTestEnvironment -TokenFile $TokenFile -OutputFile $loadOutput | Out-Null
  } catch { }
  if (Test-Path -LiteralPath $loadOutput) { $serverLoad = Get-Content -Raw -Encoding UTF8 $loadOutput | ConvertFrom-Json }
  if ($null -ne $serverLoad) { Add-Check $checks 'server_load_gate' ([string]$serverLoad.gate) '服务器负载门禁仅由 50 会话/10 并发结果判定。' $serverLoad }
  else { Add-Check $checks 'server_load_gate' 'failed' '负载执行器未生成报告。' }
  Remove-Item -LiteralPath $loadOutput -Force -ErrorAction SilentlyContinue
} elseif ($DryRun) { Add-Check $checks 'server_load_gate' 'blocked' 'dry-run 未发起服务器负载。' }
else { Add-Check $checks 'server_load_gate' 'blocked' '缺少 Target、TokenFile 或 ConfirmTestEnvironment。' }

$failed = @($checks | Where-Object { $_.status -eq 'failed' }).Count
$blocked = @($checks | Where-Object { $_.status -eq 'blocked' }).Count
$report = [ordered]@{
  schema = 'weknora-offline-frozen-suite/v1'
  profile = $Profile
  run_id = $RunId
  frozen_inputs = [ordered]@{ path = $frozenPath; freeze_sha256 = [string]$frozen.freeze_sha256; suite_sha256 = Get-Hash $suitePath }
  applicability = [ordered]@{ document_ingest = $true; retrieval = $true; routing = $true; graph = $true; verification = $true; voice = $true; performance = $true }
  profile_semantics = [ordered]@{ single_node = 'component/model location gate'; server_load = if ($Profile -eq 'desktop-lite') { 'not_applicable' } else { '50 independent sessions / 10 concurrent sessions' } }
  checks = @($checks)
  single_node_gate = (@($checks | Where-Object name -eq 'single_node_gate' | Select-Object -First 1).status)
  server_load_gate = (@($checks | Where-Object name -eq 'server_load_gate' | Select-Object -First 1).status)
  started_at = $startedAt
  completed_at = [DateTimeOffset]::UtcNow
  gate = if ($failed -gt 0) { 'failed' } elseif ($blocked -gt 0) { 'blocked' } else { 'passed' }
}
$payload = $report | ConvertTo-Json -Depth 30
$report.integrity_sha256 = ([System.BitConverter]::ToString([System.Security.Cryptography.SHA256]::HashData([System.Text.Encoding]::UTF8.GetBytes($payload)))).Replace('-','').ToLowerInvariant()
if ($OutputFile) {
  $outPath = [System.IO.Path]::GetFullPath($OutputFile)
  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $outPath) | Out-Null
  $report | ConvertTo-Json -Depth 30 | Set-Content -LiteralPath $outPath -Encoding UTF8
  Write-Output "已生成离线冻结套件报告: $outPath"
}
Write-Output ($report | ConvertTo-Json -Depth 8)
if ($report.gate -eq 'failed') { exit 2 }
if ($report.gate -eq 'blocked') { exit 3 }
