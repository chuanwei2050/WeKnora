param(
  [Parameter(Mandatory=$true)][string]$PackageDirectory,
  [ValidateSet('desktop-lite','compose-airgap','helm-airgap')][string]$ExpectedProfile,
  [ValidateSet('amd64','arm64')][string]$ExpectedArchitecture,
  [string]$ImageArchiveDirectory
)
$ErrorActionPreference = 'Stop'
$stage = (Resolve-Path -LiteralPath $PackageDirectory).Path
$manifestPath = Join-Path $stage 'manifest.json'
if (-not (Test-Path -LiteralPath $manifestPath)) { throw '缺少 manifest.json' }
$manifest = Get-Content -Raw $manifestPath | ConvertFrom-Json
if ($manifest.schema -ne 'weknora-airgap/v1') { throw "不支持的离线清单 schema: $($manifest.schema)" }
if ($ExpectedProfile -and $manifest.profile -ne $ExpectedProfile) { throw "profile 不匹配: $($manifest.profile)" }
if ($ExpectedArchitecture -and $manifest.architecture -ne $ExpectedArchitecture) { throw "架构不匹配: $($manifest.architecture)" }
if ($manifest.secrets.excluded -ne $true -or $manifest.secrets.injected -ne $true) { throw '离线清单未声明 secret 已排除且需要注入' }

$formalFiles = Get-ChildItem -LiteralPath $stage -Recurse -File | Where-Object { $_.Name -match 'docker-compose.*airgap|values-airgap.*ya?ml' }
if ($formalFiles | Select-String -Pattern '(?i)(^|[^\w])latest([^\w]|$)') { throw '正式离线清单禁止使用 latest 标签' }

$manifest = Get-Content -Raw (Join-Path $stage 'checksums.json') | ConvertFrom-Json
foreach ($entry in @($manifest)) {
  $relative = [string]$entry.path
  if ([string]::IsNullOrWhiteSpace($relative) -or [System.IO.Path]::IsPathRooted($relative) -or $relative -match '(^|[\\/])\.\.([\\/]|$)') { throw "非法清单路径: $relative" }
  $file = [System.IO.Path]::GetFullPath((Join-Path $stage $relative))
  if (-not $file.StartsWith($stage + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) { throw "清单路径越界: $relative" }
  if (-not (Test-Path -LiteralPath $file)) { throw "缺少介质文件: $($entry.path)" }
  $actual = (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actual -ne $entry.sha256) { throw "SHA-256 校验失败: $($entry.path)" }
}

if ($ImageArchiveDirectory) {
  $archiveRoot = (Resolve-Path -LiteralPath $ImageArchiveDirectory).Path
  $archives = Get-ChildItem -LiteralPath $archiveRoot -File | Where-Object { $_.Extension -in @('.tar', '.gz', '.tgz', '.zst') }
  if (-not $archives) { throw '未找到 Docker 镜像归档（支持 .tar/.gz/.tgz/.zst）' }
  foreach ($archive in $archives) {
    & docker load --input $archive.FullName
    if ($LASTEXITCODE -ne 0) { throw "Docker 镜像导入失败: $($archive.Name)" }
  }
}
Write-Host '离线介质校验通过；请在本地 Docker 或已批准内网镜像仓库中加载镜像后再启动 profile。'
