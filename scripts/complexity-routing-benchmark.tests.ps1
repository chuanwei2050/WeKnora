$ErrorActionPreference = 'Stop'
$benchmark = Join-Path $PSScriptRoot 'complexity-routing-benchmark.ps1'

function Assert-Fails([scriptblock]$action, [string]$name) {
  try {
    & $action
    throw "测试应失败但未失败: $name"
  } catch {
    if ($_.Exception.Message -like '测试应失败*') { throw }
  }
}

$dataset = Join-Path ([System.IO.Path]::GetTempPath()) ('complexity-routing-' + [Guid]::NewGuid().ToString('N') + '.json')
$token = Join-Path ([System.IO.Path]::GetTempPath()) ('complexity-routing-' + [Guid]::NewGuid().ToString('N') + '.token')
try {
  @(
    @{ id = 'l1'; question = '谁是项目负责人？'; expert_level = 'L1' }
    @{ id = 'l2'; question = '这个项目为什么延期？'; expert_level = 'L2' }
    @{ id = 'l3'; question = '比较两个方案并说明差异。'; expert_level = 'L3' }
    @{ id = 'l4'; question = '如果预算减半，方案应如何迁移？'; expert_level = 'L4' }
  ) | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $dataset -Encoding UTF8
  'test-token' | Set-Content -LiteralPath $token -Encoding UTF8

  Assert-Fails { & $benchmark -Target 'https://127.0.0.1:9999' -Model 'test' -DatasetFile $dataset -TokenFile $token } '未确认测试环境'
  Assert-Fails { & $benchmark -Target 'http://localhost:11434' -Model 'test' -DatasetFile $dataset -TokenFile $token -ConfirmTestEnvironment } '本地模型误用为线上模型'
  Assert-Fails { & $benchmark -Target 'http://10.0.0.1:11434' -Model 'test' -DatasetFile $dataset -TokenFile $token -ConfirmTestEnvironment } '私网模型误用为线上模型'
  Assert-Fails { & $benchmark -Target 'https://production.example.invalid' -Model 'test' -DatasetFile $dataset -TokenFile $token -ConfirmTestEnvironment } '生产目标拒绝'
  Assert-Fails { & $benchmark -Target 'https://example.invalid' -Model 'test' -DatasetFile $dataset -TokenFile $token -ConfirmTestEnvironment -TimeoutSeconds 0 } '超时参数边界'
  Write-Output 'complexity-routing-benchmark safety tests passed'
} finally {
  Remove-Item -LiteralPath $dataset, $token -Force -ErrorAction SilentlyContinue
}
exit 0
