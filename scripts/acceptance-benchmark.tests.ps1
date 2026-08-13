$ErrorActionPreference = 'Stop'
$benchmark = Join-Path $PSScriptRoot 'acceptance-benchmark.ps1'

function Assert-Fails([scriptblock]$action, [string]$name) {
  try {
    & $action
    throw "测试应失败但未失败: $name"
  } catch {
    if ($_.Exception.Message -like "测试应失败*") { throw }
  }
}

Assert-Fails { & $benchmark -Target 'https://127.0.0.1:9999' } '未确认测试环境'
Assert-Fails { & $benchmark -Target 'https://127.0.0.1:9999' -ConfirmTestEnvironment -Users 51 } '用户数上限'
Assert-Fails { & $benchmark -Target 'https://127.0.0.1:9999' -ConfirmTestEnvironment -Users 10 -Concurrent 11 } '并发上限'
Assert-Fails { & $benchmark -Target 'https://production.example.invalid' -ConfirmTestEnvironment } '生产目标拒绝'

& $benchmark -Target 'https://127.0.0.1:9999' -ConfirmTestEnvironment -Users 1 -Concurrent 1 -DurationSeconds 1 | Out-Null
if ($null -ne $LASTEXITCODE -and $LASTEXITCODE -ne 0) { throw '无凭据安全边界冒烟失败' }

Write-Output 'acceptance-benchmark safety tests passed'
exit 0
