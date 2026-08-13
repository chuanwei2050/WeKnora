param(
  [Parameter(Mandatory=$true)][string]$OutputDirectory,
  [ValidateSet('desktop-lite','compose-airgap','helm-airgap')][string]$Profile = 'compose-airgap',
  [ValidateSet('amd64','arm64')][string]$Architecture = 'amd64',
  [string]$DesktopArtifact,
  [string]$ImageArchiveDirectory,
  [string]$ModelInventory
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$out = [System.IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force -Path $out | Out-Null
$stage = Join-Path $out "weknora-airgap-$Profile-$Architecture"
if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
New-Item -ItemType Directory -Force -Path $stage | Out-Null

$models = @()
$modelArtifacts = @()
if ($ModelInventory) {
  $inventoryPath = (Resolve-Path -LiteralPath $ModelInventory).Path
  $inventory = Get-Content -Raw -LiteralPath $inventoryPath | ConvertFrom-Json
  foreach ($model in @($inventory.models)) {
    if (-not $model.model_name -or -not $model.license -or $null -eq $model.redistributable -or @($model.import_steps).Count -eq 0) {
      throw '模型清单必须提供 model_name、license、redistributable 和 import_steps。'
    }
    $entry = [ordered]@{
      role = [string]$model.role
      protocol = [string]$model.protocol
      location = [string]$model.location
      artifact_policy = [string]$model.artifact_policy
      model_name = [string]$model.model_name
      license = [string]$model.license
      redistributable = [bool]$model.redistributable
      artifact_included = $false
      import_steps = @($model.import_steps | ForEach-Object { [string]$_ })
    }
    if ($model.redistributable) {
      if (-not $model.artifact_path) { throw "可再分发模型 $($model.model_name) 必须提供 artifact_path。" }
      $artifactSource = (Resolve-Path -LiteralPath ([string]$model.artifact_path)).Path
      $artifactName = [System.IO.Path]::GetFileName($artifactSource)
      $artifactTarget = Join-Path $stage (Join-Path 'model-weights' $artifactName)
      New-Item -ItemType Directory -Force -Path (Split-Path -Parent $artifactTarget) | Out-Null
      Copy-Item -LiteralPath $artifactSource -Destination $artifactTarget
      $hash = (Get-FileHash -LiteralPath $artifactTarget -Algorithm SHA256).Hash.ToLowerInvariant()
      $entry.artifact_included = $true
      $entry.artifact_path = "model-weights/$artifactName"
      $entry.artifact_sha256 = $hash
      $modelArtifacts += [ordered]@{ path = "model-weights/$artifactName"; source = 'model-inventory'; architecture = $Architecture; sha256 = $hash }
    }
    $models += [pscustomobject]$entry
  }
}
if (-not $ModelInventory) {
  Write-Warning '未提供 -ModelInventory：本包只包含应用介质，不构成模型覆盖或 single-node 交付包。'
}

Copy-Item -LiteralPath (Join-Path $root 'config') -Destination (Join-Path $stage 'config') -Recurse
Copy-Item -LiteralPath (Join-Path $root 'migrations') -Destination (Join-Path $stage 'migrations') -Recurse
Copy-Item -LiteralPath (Join-Path $root 'docker-compose.yml') -Destination (Join-Path $stage 'docker-compose.yml')
Copy-Item -LiteralPath (Join-Path $root 'deploy/airgap') -Destination (Join-Path $stage 'deploy') -Recurse
Copy-Item -LiteralPath (Join-Path $root 'helm') -Destination (Join-Path $stage 'helm') -Recurse
Copy-Item -LiteralPath (Join-Path $root 'helm/values-airgap.yaml') -Destination (Join-Path $stage 'helm-values-airgap.yaml')
Copy-Item -LiteralPath (Join-Path $root 'helm/values-airgap.lock.yaml') -Destination (Join-Path $stage 'helm-values-airgap.lock.yaml')
Copy-Item -LiteralPath (Join-Path $root 'deploy/airgap/manifest.schema.json') -Destination (Join-Path $stage 'manifest.schema.json')
Copy-Item -LiteralPath (Join-Path $root 'docs/airgap-operations.md') -Destination (Join-Path $stage 'airgap-operations.md')
Copy-Item -LiteralPath (Join-Path $root 'scripts/rollback-airgap.ps1') -Destination (Join-Path $stage 'rollback-airgap.ps1')
if ($DesktopArtifact) {
  $desktop = (Resolve-Path -LiteralPath $DesktopArtifact).Path
  New-Item -ItemType Directory -Force -Path (Join-Path $stage 'artifacts') | Out-Null
  Copy-Item -LiteralPath $desktop -Destination (Join-Path $stage 'artifacts')
}
if ($ImageArchiveDirectory) {
  $imageRoot = (Resolve-Path -LiteralPath $ImageArchiveDirectory).Path
  New-Item -ItemType Directory -Force -Path (Join-Path $stage 'images') | Out-Null
  Get-ChildItem -LiteralPath $imageRoot -File | Where-Object { $_.Extension -in @('.tar', '.gz', '.tgz', '.zst') } | Copy-Item -Destination (Join-Path $stage 'images')
}
if ($Profile -eq 'helm-airgap') {
  $helmValuesPath = Join-Path $stage 'helm-values-airgap.yaml'
  $helmLockPath = Join-Path $stage 'helm-values-airgap.lock.yaml'
  $helmValues = Get-Content -Raw $helmValuesPath
  $helmLock = Get-Content -Raw $helmLockPath
  if ($helmValues -match '(?i)\blatest\b' -or $helmLock -match '(?i)\blatest\b') {
    throw 'helm-airgap 的 values/lock 禁止使用 latest。'
  }
  if ($helmLock -match 'REPLACE_WITH_VERIFIED_DIGEST' -or $helmValues -match 'REPLACE_WITH_VERIFIED_DIGEST') {
    throw 'helm-airgap 禁止使用占位镜像 digest；请先填入发布流程确认的 sha256 digest。'
  }
  $lockDigests = [regex]::Matches($helmLock, '(?m)^\s*digest:\s*sha256:([0-9a-f]{64})\s*$')
  if ($lockDigests.Count -eq 0) {
    throw 'helm-airgap 必须为每个镜像提供完整的 sha256 digest。'
  }
  $valueDigests = [regex]::Matches($helmValues, '(?ms)^\s*repository:\s*([^\r\n]+)\s*\r?\n\s*tag:\s*[^\r\n]+\s*\r?\n\s*digest:\s*(sha256:[0-9a-f]{64})\s*$')
  if ($valueDigests.Count -eq 0) {
    throw 'helm-airgap 的每个正式镜像必须在 values 中声明完整的 sha256 digest。'
  }
  foreach ($match in $valueDigests) {
    $imageName = $match.Groups[1].Value.Trim()
    $digest = $match.Groups[2].Value.Trim()
    if ($helmLock -notmatch "(?m)^\s*-\s*name:\s*$([regex]::Escape($imageName))\s*\r?\n\s*digest:\s*$([regex]::Escape($digest))\s*$") {
      throw "helm-airgap values 中的镜像 $imageName@$digest 未在 lock 中精确登记。"
    }
  }
}
if ($Profile -eq 'compose-airgap') {
  $composeOverride = Get-Content -Raw (Join-Path $stage 'deploy/docker-compose.airgap.override.yml')
  $composeImages = [regex]::Matches($composeOverride, '(?m)^\s*image:\s*([^\r\n]+)')
  foreach ($match in $composeImages) {
    $image = $match.Groups[1].Value.Trim()
    if ($image -notmatch '@sha256:[0-9a-f]{64}') {
      throw "compose-airgap 禁止使用可变镜像引用: $image"
    }
  }
}
@{ schema='weknora-airgap/v1'; profile=$Profile; architecture=$Architecture; generated_at=(Get-Date).ToUniversalTime().ToString('o'); components=@(@{name='app'; location='same-host'; source='repository'; license='Apache-2.0'}, @{name='docreader'; location='same-host'; source='repository'; license='Apache-2.0'}); models=$models; model_inventory_provided=[bool]$ModelInventory; artifacts=$modelArtifacts; checksums=@(); secrets=@{injected=$true; excluded=$true} } |
  ConvertTo-Json -Depth 10 | Set-Content -LiteralPath (Join-Path $stage 'manifest.json') -Encoding UTF8

$secretPatterns = @(
  '(?i)(?<![\$\{])\bapi[_-]?key\s*[:=]\s*(?!["'']?(?:\$\{|\$\(|\{\{|<))[^\s,"'']+',
  '(?i)(?<![\$\{])\bpassword\s*[:=]\s*(?!["'']?(?:\$\{|\$\(|\{\{|<))[^\s,"'']+',
  '(?i)(?<![\$\{])\bjwt[_-]?secret\s*[:=]\s*(?!["'']?(?:\$\{|\$\(|\{\{|<))[^\s,"'']+',
  '(?i)(?<![\$\{])\bprivate[_-]?key\s*[:=]\s*(?!["'']?(?:\$\{|\$\(|\{\{|<))[^\s,"'']+',
  '(?i)(?<![\$\{])\bapp[_-]?secret\s*[:=]\s*(?!["'']?(?:\$\{|\$\(|\{\{|<))[^\s,"'']+'
)
$textExtensions = @(
  '', '.conf', '.env', '.go', '.html', '.json', '.js', '.md', '.properties',
  '.ps1', '.py', '.sh', '.sql', '.ts', '.tsx', '.toml', '.txt', '.vue',
  '.yaml', '.yml'
)
$textFiles = Get-ChildItem -LiteralPath $stage -Recurse -File | Where-Object {
  $textExtensions -contains $_.Extension.ToLowerInvariant()
}
$leaks = $textFiles | Select-String -Pattern $secretPatterns -ErrorAction SilentlyContinue
if ($leaks) { throw '离线包检测到疑似 secret 字段，请改用 secret file 或 existing Secret。' }

$entries = Get-ChildItem -LiteralPath $stage -Recurse -File | ForEach-Object {
  $hash = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
  [pscustomobject]@{ path=$_.FullName.Substring($stage.Length + 1); sha256=$hash; architecture=$Architecture; source='repository' }
}
$entries | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $stage 'checksums.json') -Encoding UTF8
Write-Host "已生成 $stage；导入前必须使用 import-airgap.ps1 校验 checksums.json。"
