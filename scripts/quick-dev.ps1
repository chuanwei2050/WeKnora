param(
    [ValidateSet('start', 'stop')]
    [string]$Action = 'start'
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot

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

$drive = $projectRoot.Substring(0, 1).ToLowerInvariant()
$bashRoot = "/$drive" + $projectRoot.Substring(2).Replace('\', '/')
$bashRoot = $bashRoot.Replace("'", "'\''")

$command = "cd '$bashRoot' && exec ./scripts/quick-dev.sh $Action"
& $gitBash -lc $command
exit $LASTEXITCODE
