param(
  [Parameter(Mandatory = $true)][string]$ManifestPath,
  [Parameter(Mandatory = $true)][string]$StateDirectory,
  [ValidateSet('snapshot', 'rollback')][string]$Action = 'snapshot',
  [ValidateSet('passed', 'failed', 'rolled_back')][string]$RunStatus = 'passed',
  [string]$TargetSnapshot,
  [string]$OperationId = ([guid]::NewGuid().ToString())
)

$ErrorActionPreference = 'Stop'

function Resolve-ContainedPath([string]$Path, [string]$Root) {
  $resolvedRoot = [System.IO.Path]::GetFullPath((Resolve-Path -LiteralPath $Root).Path)
  $resolvedPath = [System.IO.Path]::GetFullPath((Resolve-Path -LiteralPath $Path).Path)
  if (-not $resolvedPath.StartsWith($resolvedRoot + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Path must be contained by the state directory: $Path"
  }
  return $resolvedPath
}

function Get-ManifestHash([string]$Path) {
  return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

$manifest = (Resolve-Path -LiteralPath $ManifestPath).Path
if (-not (Test-Path -LiteralPath $StateDirectory)) {
  New-Item -ItemType Directory -Path $StateDirectory | Out-Null
}
$state = (Resolve-Path -LiteralPath $StateDirectory).Path
$snapshots = Join-Path $state 'snapshots'
$auditPath = Join-Path $state 'rollback-audit.jsonl'
New-Item -ItemType Directory -Force -Path $snapshots | Out-Null

$currentJson = Get-Content -Raw -LiteralPath $manifest | ConvertFrom-Json
if ($currentJson.schema -ne 'weknora-airgap/v1') {
  throw "Unsupported air-gap manifest schema: $($currentJson.schema)"
}

function Write-Audit([hashtable]$Entry) {
  ($Entry | ConvertTo-Json -Depth 12 -Compress) | Add-Content -LiteralPath $auditPath -Encoding UTF8
}

if ($Action -eq 'snapshot') {
  $snapshotName = "{0}-{1}.json" -f (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssfffZ'), $OperationId
  $snapshotPath = Join-Path $snapshots $snapshotName
  Copy-Item -LiteralPath $manifest -Destination $snapshotPath
  $hash = Get-ManifestHash $snapshotPath
  Write-Audit @{
    operation_id = $OperationId
    action = 'snapshot'
    run_status = $RunStatus
    manifest = $manifest
    snapshot = $snapshotPath
    before_sha256 = $hash
    after_sha256 = $hash
    created_at = (Get-Date).ToUniversalTime().ToString('o')
  }
  Write-Host "Manifest snapshot saved: $snapshotPath"
  Write-Host "sha256: $hash"
  exit 0
}

if ([string]::IsNullOrWhiteSpace($TargetSnapshot)) {
  throw 'Rollback requires -TargetSnapshot to avoid restoring an unknown manifest.'
}
$target = Resolve-ContainedPath $TargetSnapshot $state
if (-not (Test-Path -LiteralPath $target -PathType Leaf)) {
  throw "Target snapshot does not exist: $TargetSnapshot"
}
$targetJson = Get-Content -Raw -LiteralPath $target | ConvertFrom-Json
if ($targetJson.schema -ne 'weknora-airgap/v1') {
  throw "Unsupported target snapshot schema: $($targetJson.schema)"
}

$beforeHash = Get-ManifestHash $manifest
$afterHash = Get-ManifestHash $target
$backup = Join-Path $snapshots ("pre-rollback-{0}.json" -f $OperationId)
Copy-Item -LiteralPath $manifest -Destination $backup
Copy-Item -LiteralPath $target -Destination $manifest
Write-Audit @{
  operation_id = $OperationId
  action = 'rollback'
  run_status = 'rolled_back'
  manifest = $manifest
  target_snapshot = $target
  pre_rollback_snapshot = $backup
  before_sha256 = $beforeHash
  after_sha256 = $afterHash
  restored_profile = [string]$targetJson.profile
  restored_models = @($targetJson.models | ForEach-Object { [string]$_.model_name })
  created_at = (Get-Date).ToUniversalTime().ToString('o')
}
Write-Host "Manifest rolled back: $manifest"
Write-Host "before sha256: $beforeHash"
Write-Host "after sha256:  $afterHash"
