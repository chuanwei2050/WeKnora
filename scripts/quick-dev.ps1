param(
    [ValidateSet('start', 'stop')]
    [string]$Action = 'start'
)

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Split-Path -Parent $PSScriptRoot)).Path

$gitBashCandidates = @()
$gitCommand = Get-Command git.exe -ErrorAction SilentlyContinue
if ($gitCommand) {
    $gitRoot = Split-Path -Parent (Split-Path -Parent $gitCommand.Source)
    $gitBashCandidates += Join-Path $gitRoot 'bin\bash.exe'
}
$gitBashCandidates += @(
    'C:\Program Files\Git\bin\bash.exe',
    'C:\Program Files (x86)\Git\bin\bash.exe'
)

$gitBash = $gitBashCandidates |
    Select-Object -Unique |
    Where-Object { Test-Path -LiteralPath $_ } |
    Select-Object -First 1

if (-not $gitBash) {
    throw '未找到 Git Bash。请安装 Git for Windows，或直接在 Git Bash 中运行 ./scripts/quick-dev.sh。'
}

# Git Bash 可直接吃盘符路径（F:/...）；UNC 不支持手写转换。
if ($projectRoot -notmatch '^[A-Za-z]:[\\/]') {
    throw "当前仓库路径不受支持（需本地盘符路径）: $projectRoot"
}
$bashRoot = ($projectRoot -replace '\\', '/')
$bashRoot = $bashRoot.Replace("'", "'\''")

$command = "cd '$bashRoot' && exec ./scripts/quick-dev.sh $Action"
& $gitBash -lc $command
exit $LASTEXITCODE
