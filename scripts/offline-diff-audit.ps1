param(
  [Parameter(Mandatory=$true)][string]$FrozenInputsFile,
  [Parameter(Mandatory=$true)][string]$OnlineBaselineFile,
  [string]$DesktopLiteReport,
  [string]$ComposeAirgapReport,
  [string]$HelmAirgapReport,
  [string]$DesktopPackageDirectory,
  [string]$ComposePackageDirectory,
  [string]$HelmPackageDirectory,
  [string]$OutputFile = 'openspec/changes/add-air-gapped-model-deployment/evidence/online-offline-diff.json'
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path

function Resolve-Input([string]$Path) {
  $candidate = if ([System.IO.Path]::IsPathRooted($Path)) { $Path } else { Join-Path $root $Path }
  return (Resolve-Path -LiteralPath $candidate).Path
}
function Add-Check([System.Collections.Generic.List[object]]$Checks, [string]$Name, [string]$Status, [string]$Details, $Data = $null) {
  $Checks.Add([ordered]@{ name = $Name; status = $Status; details = $Details; data = $Data })
}
function Get-Hash([string]$Path) { return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant() }
function Read-Json([string]$Path, [string]$Label) {
  if ([string]::IsNullOrWhiteSpace($Path)) { return $null }
  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "$Label 不存在: $Path" }
  return Get-Content -Raw -Encoding UTF8 $Path | ConvertFrom-Json
}
function Resolve-PackageStage([string]$Path, [string]$Profile) {
  $rootPath = Resolve-Input $Path
  if (Test-Path -LiteralPath (Join-Path $rootPath 'manifest.json') -PathType Leaf) { return $rootPath }
  $candidates = @(Get-ChildItem -LiteralPath $rootPath -Directory | Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName 'manifest.json') })
  if ($candidates.Count -ne 1) { throw "$Profile 离线包目录必须直接包含 manifest.json，或只包含一个带 manifest.json 的包目录。" }
  return $candidates[0].FullName
}
function Get-ProfileReport([string]$Path, [string]$Profile, [System.Collections.Generic.List[object]]$Checks) {
  if ([string]::IsNullOrWhiteSpace($Path)) {
    Add-Check $Checks "profile_${Profile}" 'blocked' '未提供 profile 报告。'
    return $null
  }
  $report = Read-Json $Path $Profile
  if ([string]$report.schema -ne 'weknora-offline-frozen-suite/v1') {
    Add-Check $Checks "profile_${Profile}" 'failed' '报告 schema 不是统一冻结套件 schema。'
    return $report
  }
  if ([string]$report.profile -ne $Profile) {
    Add-Check $Checks "profile_${Profile}" 'failed' "报告 profile 不匹配：$($report.profile)。"
    return $report
  }
  Add-Check $Checks "profile_${Profile}" ([string]$report.gate) '已读取统一冻结套件报告。' $report
  return $report
}

$frozenPath = Resolve-Input $FrozenInputsFile
$onlinePath = Resolve-Input $OnlineBaselineFile
$frozen = Read-Json $frozenPath 'FrozenInputsFile'
$online = Read-Json $onlinePath 'OnlineBaselineFile'
$checks = [System.Collections.Generic.List[object]]::new()

if ($frozen.schema -ne 'weknora-acceptance-frozen-inputs/v1') { Add-Check $checks 'frozen_inputs_schema' 'failed' '冻结输入 schema 无效。' }
elseif ([string]::IsNullOrWhiteSpace([string]$frozen.freeze_sha256)) { Add-Check $checks 'frozen_inputs_schema' 'failed' '冻结输入缺少 freeze_sha256。' }
else { Add-Check $checks 'frozen_inputs_schema' 'passed' '冻结输入 schema 和 hash 存在。' }

$onlinePassed = ([string]$online.gate -eq 'passed' -and [string]$online.formal_acceptance.status -eq 'blocked')
if ($onlinePassed) { Add-Check $checks 'online_baseline' 'passed' '线上模型工程基线 gate 通过，但 formal acceptance 仍按报告声明为 blocked。' $online }
else { Add-Check $checks 'online_baseline' 'failed' '线上基线必须 gate=passed 且 formal acceptance 不得伪造通过。' $online }

$expectedModels = @{}
foreach ($entry in @($frozen.models.PSObject.Properties)) {
  if ($entry.Value.name) { $expectedModels[$entry.Name] = [string]$entry.Value.name }
}
$actualModels = @{}
foreach ($entry in @($online.models.PSObject.Properties)) { $actualModels[$entry.Name] = [string]$entry.Value }
$modelDiff = [System.Collections.Generic.List[object]]::new()
foreach ($role in $expectedModels.Keys) {
  if (-not $actualModels.ContainsKey($role)) { $modelDiff.Add([ordered]@{ role = $role; expected = $expectedModels[$role]; actual = $null; status = 'missing' }) }
  elseif ($expectedModels[$role] -ne $actualModels[$role]) { $modelDiff.Add([ordered]@{ role = $role; expected = $expectedModels[$role]; actual = $actualModels[$role]; status = 'mismatch' }) }
}
if ($modelDiff.Count -eq 0) { Add-Check $checks 'model_identity_diff' 'passed' '线上基线模型身份与冻结清单一致。' }
else { Add-Check $checks 'model_identity_diff' 'failed' '线上基线模型身份与冻结清单不一致。' @($modelDiff) }

$reports = @(
  [pscustomobject]@{ profile = 'desktop-lite'; path = $DesktopLiteReport }
  [pscustomobject]@{ profile = 'compose-airgap'; path = $ComposeAirgapReport }
  [pscustomobject]@{ profile = 'helm-airgap'; path = $HelmAirgapReport }
)
$profileReports = @{}
foreach ($item in $reports) { $profileReports[$item.profile] = Get-ProfileReport $item.path $item.profile $checks }

$freezeDiff = [System.Collections.Generic.List[object]]::new()
foreach ($item in $reports) {
  $report = $profileReports[$item.profile]
  if ($null -eq $report) { continue }
  $reportHash = [string]$report.frozen_inputs.freeze_sha256
  if ($reportHash -ne [string]$frozen.freeze_sha256) { $freezeDiff.Add([ordered]@{ profile = $item.profile; expected = [string]$frozen.freeze_sha256; actual = $reportHash }) }
}
if ($freezeDiff.Count -eq 0 -and @($profileReports.Values | Where-Object { $null -ne $_ }).Count -eq 3) { Add-Check $checks 'frozen_input_consistency' 'passed' '三个 profile 均复用同一冻结输入 hash。' }
elseif ($freezeDiff.Count -gt 0) { Add-Check $checks 'frozen_input_consistency' 'failed' 'profile 报告的冻结输入 hash 不一致。' @($freezeDiff) }
else { Add-Check $checks 'frozen_input_consistency' 'blocked' '三个 profile 报告尚未全部提供。' }

$packageMap = @(
  [pscustomobject]@{ profile = 'desktop-lite'; path = $DesktopPackageDirectory }
  [pscustomobject]@{ profile = 'compose-airgap'; path = $ComposePackageDirectory }
  [pscustomobject]@{ profile = 'helm-airgap'; path = $HelmPackageDirectory }
)
$packageResults = [System.Collections.Generic.List[object]]::new()
foreach ($item in $packageMap) {
  if ([string]::IsNullOrWhiteSpace($item.path)) { continue }
  try {
    $packageStage = Resolve-PackageStage $item.path $item.profile
    $manifestPath = Join-Path $packageStage 'manifest.json'
    $manifest = Read-Json $manifestPath "$($item.profile) manifest"
    $checksumsPath = Join-Path (Split-Path $manifestPath -Parent) 'checksums.json'
    $checksumHash = if (Test-Path -LiteralPath $checksumsPath) { Get-Hash $checksumsPath } else { $null }
    $expectedRoles = @($frozen.models.PSObject.Properties.Name)
    $declaredRoles = @($manifest.models | ForEach-Object { [string]$_.role })
    $missingRoles = @($expectedRoles | Where-Object { $_ -notin $declaredRoles })
    $packageResults.Add([ordered]@{ profile = $item.profile; manifest_sha256 = Get-Hash $manifestPath; checksums_sha256 = $checksumHash; declared_profile = [string]$manifest.profile; declared_architecture = [string]$manifest.architecture; missing_model_roles = @($missingRoles) })
    if ($missingRoles.Count -gt 0) { Add-Check $checks 'package_model_coverage' 'blocked' "$($item.profile) 缺少冻结清单中的模型角色：$($missingRoles -join ', ')。" @($missingRoles) }
  } catch { Add-Check $checks 'package_integrity' 'failed' $_.Exception.Message }
}
if ($packageResults.Count -eq 3) { Add-Check $checks 'package_integrity' 'passed' '三个 profile 均提供 manifest/checksums 摘要。' @($packageResults) }
else { Add-Check $checks 'package_integrity' 'blocked' '未提供三个 profile 的离线包目录，无法完成完整介质差异材料。' @($packageResults) }

$outbound = @()
foreach ($item in $profileReports.Values) {
  if ($null -eq $item) { continue }
  foreach ($check in @($item.checks | Where-Object { $_.name -eq 'outbound_audit' })) { $outbound += [ordered]@{ profile = [string]$item.profile; status = [string]$check.status; details = [string]$check.details } }
}
if ($outbound.Count -eq 3 -and @($outbound | Where-Object status -ne 'passed').Count -eq 0) { Add-Check $checks 'outbound_audit_diff' 'passed' '三个 profile 的出站审计均通过。' @($outbound) }
elseif ($outbound.Count -eq 0) { Add-Check $checks 'outbound_audit_diff' 'blocked' 'profile 报告未携带出站审计。' }
else { Add-Check $checks 'outbound_audit_diff' 'failed' '至少一个 profile 出站审计未通过。' @($outbound) }

$failed = @($checks | Where-Object status -eq 'failed').Count
$blocked = @($checks | Where-Object status -eq 'blocked').Count
$report = [ordered]@{
  schema = 'weknora-online-offline-diff/v1'
  online_baseline = $onlinePath
  frozen_inputs = [ordered]@{ path = $frozenPath; freeze_sha256 = [string]$frozen.freeze_sha256 }
  model_identity_diff = @($modelDiff)
  profiles = @($reports | ForEach-Object { $_.profile })
  package_integrity = @($packageResults)
  outbound_audit = @($outbound)
  checks = @($checks)
  gate = if ($failed -gt 0) { 'failed' } elseif ($blocked -gt 0) { 'blocked' } else { 'passed' }
  generated_at = [DateTimeOffset]::UtcNow
}
$json = $report | ConvertTo-Json -Depth 30
$report.integrity_sha256 = ([System.BitConverter]::ToString([System.Security.Cryptography.SHA256]::HashData([System.Text.Encoding]::UTF8.GetBytes($json)))).Replace('-','').ToLowerInvariant()
$outPath = if ([System.IO.Path]::IsPathRooted($OutputFile)) { $OutputFile } else { Join-Path $root $OutputFile }
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $outPath) | Out-Null
$report | ConvertTo-Json -Depth 30 | Set-Content -LiteralPath $outPath -Encoding UTF8
Write-Output "已生成线上/离线差异报告: $outPath"
Write-Output ($report | ConvertTo-Json -Depth 8)
if ($report.gate -eq 'failed') { exit 2 }
if ($report.gate -eq 'blocked') { exit 3 }
