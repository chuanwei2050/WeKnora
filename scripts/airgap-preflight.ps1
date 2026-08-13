param(
  [Parameter(Mandatory=$true)][string]$DeploymentDirectory,
  [ValidateSet('desktop-lite','compose-airgap','helm-airgap')][string]$Profile = 'compose-airgap',
  [switch]$RequireSameHost
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path -LiteralPath $DeploymentDirectory).Path
$manifestPath = Join-Path $root 'manifest.json'
if (-not (Test-Path -LiteralPath $manifestPath)) { throw '缺少 manifest.json' }
$manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
if ($manifest.schema -ne 'weknora-airgap/v1' -or $manifest.profile -ne $Profile) { throw '离线清单 schema 或 profile 不匹配' }
if ($manifest.secrets.excluded -ne $true -or $manifest.secrets.injected -ne $true) { throw '离线清单未声明 secret 排除和注入策略' }

# `docker-compose.yml` is the shared development base. The formal air-gapped
# override below replaces every active core image; only the formal override
# and air-gapped values/lock are policy inputs for this scan.
$configFiles = Get-ChildItem -LiteralPath $root -Recurse -File | Where-Object {
  $_.Extension -in @('.yaml','.yml','.env','.conf') -and $_.Name -ne 'Chart.yaml' -and
  $_.FullName.Substring($root.Length + 1).Replace('\', '/') -notin @('helm/values.yaml', 'docker-compose.yml')
}
$forbiddenPattern = '(?i)(^|[/:])latest(["''\s]|$)|https?://(?!localhost|127\.0\.0\.1|::1|10\.|172\.(1[6-9]|2[0-9]|3[01])\.|192\.168\.)'
$forbidden = foreach ($file in $configFiles) {
  $insideHelmComment = $false
  $lineNumber = 0
  foreach ($line in Get-Content -LiteralPath $file.FullName) {
    $lineNumber++
    if ($insideHelmComment) {
      if ($line -match '\*/}}') { $insideHelmComment = $false }
      continue
    }
    if ($line -match '{{/\*') {
      if ($line -notmatch '\*/}}') { $insideHelmComment = $true }
      continue
    }
    if ($line.TrimStart() -match '^(#|//)') { continue }
    if ($line -match $forbiddenPattern) {
      [pscustomobject]@{ Path = $file.FullName; LineNumber = $lineNumber; Line = $line }
    }
  }
}
if ($forbidden) { throw '离线介质包含 latest 或未批准公网 URL' }
foreach ($component in @($manifest.components)) {
  if ([string]$component.location -notin @('same-host', 'private-network')) {
    throw "严格离线 profile 禁止组件 $($component.name) 使用位置 $($component.location)"
  }
}
$locationEntries = @($manifest.components)
foreach ($model in @($manifest.models)) {
  if ([string]$model.location -in @('public', 'unknown', '')) {
    throw "严格离线 profile 禁止模型 $($model.model_name) 使用位置 $($model.location)"
  }
  $locationEntries += [pscustomobject]@{
    name = "model:$($model.model_name)"
    location = $model.location
  }
}
if ($RequireSameHost) {
  foreach ($component in $locationEntries) {
    if ($component.location -ne 'same-host') {
      throw "single-node 要求组件或模型 $($component.name) 为 same-host，实际为 $($component.location)"
    }
  }
}
& (Join-Path $PSScriptRoot 'import-airgap.ps1') -PackageDirectory $root
Write-Host "离线预检通过：profile=$Profile，组件位置和制品完整性满足当前门禁。"
