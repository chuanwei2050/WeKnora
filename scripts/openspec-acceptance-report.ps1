param(
  [Parameter(Mandatory=$true)][string]$OutputDirectory,
  [ValidateSet('desktop-lite','compose-airgap','helm-airgap')][string]$Profile = 'compose-airgap',
  [string]$PackageDirectory,
  [string]$Target,
  [string]$TokenFile,
  [int]$Users = 50,
  [int]$Concurrent = 10,
  [int]$DurationSeconds = 60,
  [switch]$ConfirmTestEnvironment,
  [switch]$RequireSameHost,
  [switch]$ExpectSameHostFailure
)

$ErrorActionPreference = 'Stop'
if ($RequireSameHost -and $ExpectSameHostFailure) {
  throw 'RequireSameHost 与 ExpectSameHostFailure 不能同时使用。'
}
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$out = [System.IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force -Path $out | Out-Null
$startedAt = [DateTimeOffset]::UtcNow
$checks = [System.Collections.Generic.List[object]]::new()

function Add-Check {
  param([string]$Name, [string]$Status, [string]$Command, [string]$Details)
  $checks.Add([ordered]@{
    name = $Name
    status = $Status
    command = $Command
    details = $Details
  })
}

function Invoke-LocalCheck {
  param([string]$Name, [string]$Command, [scriptblock]$Action)
  try {
    $output = (& $Action 2>&1 | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { throw $output }
    Add-Check $Name 'passed' $Command $output
  } catch {
    Add-Check $Name 'failed' $Command $_.Exception.Message
  }
}

function Invoke-ExpectedSameHostFailure {
  param([string]$Command, [scriptblock]$Action)
  $output = ''
  try {
    $output = (& $Action 2>&1 | Out-String).Trim()
    throw 'single-node 预检未拒绝 private-network 端点。'
  } catch {
    $details = (($output + "`n" + $_.Exception.Message).Trim())
    if ($details -match 'single-node.*same-host.*private-network') {
      Add-Check 'single_node_gate' 'passed' $Command $details
    } else {
      Add-Check 'single_node_gate' 'failed' $Command $details
    }
  }
}

Invoke-LocalCheck 'openspec_validate' 'openspec validate --all --strict --no-interactive' {
  openspec validate --all --strict --no-interactive
}

Invoke-LocalCheck 'git_diff_check' 'git diff --check' {
  git -C $root diff --check
}

if ($PackageDirectory) {
  $preflight = Join-Path $PSScriptRoot 'airgap-preflight.ps1'
  $preflightArgs = @{
    DeploymentDirectory = $PackageDirectory
    Profile = $Profile
  }
  if ($RequireSameHost) { $preflightArgs.RequireSameHost = $true }
  $preflightCommand = "airgap-preflight.ps1 -DeploymentDirectory $PackageDirectory -Profile $Profile" + $(if ($RequireSameHost) { ' -RequireSameHost' } else { '' })
  Invoke-LocalCheck 'airgap_preflight' $preflightCommand {
    if ($ExpectSameHostFailure) {
      & $preflight -DeploymentDirectory $PackageDirectory -Profile $Profile
    } else {
      & $preflight @preflightArgs
    }
  }
  if ($ExpectSameHostFailure) {
    Invoke-ExpectedSameHostFailure "airgap-preflight.ps1 -DeploymentDirectory $PackageDirectory -Profile $Profile -RequireSameHost" {
      & $preflight -DeploymentDirectory $PackageDirectory -Profile $Profile -RequireSameHost
    }
  }
} else {
  Add-Check 'airgap_preflight' 'blocked' 'airgap-preflight.ps1' '未提供已生成的离线包目录，未执行介质完整性和位置门禁。'
}

if ($Target) {
  if (-not $ConfirmTestEnvironment) {
    Add-Check 'online_load_benchmark' 'blocked' 'acceptance-benchmark.ps1' '提供了线上目标但未使用 ConfirmTestEnvironment，拒绝发起负载。'
  } elseif (-not $TokenFile) {
    Add-Check 'online_load_benchmark' 'blocked' 'acceptance-benchmark.ps1' '缺少 TokenFile；未执行线上模型、50 会话和 10 并发验收。'
  } else {
    $benchmark = Join-Path $PSScriptRoot 'acceptance-benchmark.ps1'
    $benchmarkOutput = Join-Path $out 'online-load.json'
    Invoke-LocalCheck 'online_load_benchmark' "acceptance-benchmark.ps1 -Target $Target -Users $Users -Concurrent $Concurrent -DurationSeconds $DurationSeconds" {
      & $benchmark -Target $Target -Users $Users -Concurrent $Concurrent -DurationSeconds $DurationSeconds -ConfirmTestEnvironment -TokenFile $TokenFile -OutputFile $benchmarkOutput
    }
  }
} else {
  Add-Check 'online_load_benchmark' 'blocked' 'acceptance-benchmark.ps1' '未提供线上目标和认证令牌，线上模型、TTFT、50 会话和 10 并发门禁未执行。'
}

$failed = @($checks | Where-Object { $_.status -eq 'failed' }).Count
$blocked = @($checks | Where-Object { $_.status -eq 'blocked' }).Count
$gate = if ($failed -gt 0) { 'failed' } elseif ($blocked -gt 0) { 'blocked' } else { 'passed' }
$report = [ordered]@{
  schema = 'weknora-openspec-acceptance/v1'
  profile = $Profile
  started_at = $startedAt
  completed_at = [DateTimeOffset]::UtcNow
  gate = $gate
  checks = @($checks)
}

$jsonPath = Join-Path $out 'openspec-acceptance.json'
$mdPath = Join-Path $out 'openspec-acceptance.md'
$report | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $jsonPath -Encoding UTF8
$md = [System.Collections.Generic.List[string]]::new()
$md.Add("# OpenSpec 验收报告")
$md.Add("")
$md.Add("- profile: $Profile")
$md.Add("- gate: **$gate**")
$md.Add("- generated_at: $([DateTimeOffset]::UtcNow.ToString('o'))")
$md.Add("")
$md.Add('| 检查项 | 状态 | 说明 |')
$md.Add('| --- | --- | --- |')
foreach ($check in $checks) {
  $detail = ([string]$check.details).Replace('|', '\|').Replace("`r", ' ').Replace("`n", ' ')
  $md.Add("| $($check.name) | $($check.status) | $detail |")
}
$md -join "`n" | Set-Content -LiteralPath $mdPath -Encoding UTF8
Write-Output "已生成验收报告: $jsonPath"
Write-Output ($report | ConvertTo-Json -Depth 8)
if ($gate -eq 'failed') { exit 2 }
if ($gate -eq 'blocked') { exit 3 }
