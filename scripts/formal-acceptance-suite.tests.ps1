$ErrorActionPreference = 'Stop'
$script = Join-Path $PSScriptRoot 'formal-acceptance-suite.ps1'
$suite = Join-Path $PSScriptRoot 'testdata/baseline-v1-acceptance-suite.json'
$freezeScript = Join-Path $PSScriptRoot 'freeze-acceptance-inputs.ps1'
$freeze = Join-Path ([System.IO.Path]::GetTempPath()) ('weknora-frozen-inputs-' + [Guid]::NewGuid().ToString('N') + '.json')
$token = Join-Path $PSScriptRoot 'testdata/formal-acceptance-token.test'
Set-Content -LiteralPath $token -Value 'test-token' -Encoding ascii
try {
  & $freezeScript -OutputFile $freeze | Out-Null
  $frozenOutput = & $script -SuiteFile $suite -Target 'http://127.0.0.1:8081' -TokenFile $token -RunId 'dry-run-frozen-test' -FrozenInputsFile $freeze -DryRun 2>&1 | Out-String
  if ($LASTEXITCODE -ne 2 -or $frozenOutput -notmatch '"frozen_inputs"') { throw "冻结输入 dry-run 未正确进入报告: $frozenOutput" }
  $output = & $script -SuiteFile $suite -Target 'http://127.0.0.1:8081' -TokenFile $token -RunId 'dry-run-test' -DryRun 2>&1 | Out-String
  if ($LASTEXITCODE -ne 2) { throw "dry-run 必须以 2 退出，实际为 $LASTEXITCODE。输出: $output" }
  if ($output -notmatch '"gate"\s*:\s*"incomplete"' -or $output -match '"gate"\s*:\s*"passed"') { throw "dry-run gate 不正确: $output" }
} finally {
  Remove-Item -LiteralPath $token -Force -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath $freeze -Force -ErrorAction SilentlyContinue
}

foreach ($args in @(
  @('-Target', 'http://production.example.invalid'),
  @('-Target', 'not-a-url'),
  @('-Target', 'http://127.0.0.1:8081', '-TokenFile', 'missing-token-file')
)) {
  try {
    & $script -SuiteFile $suite -RunId 'safety-test' -DryRun @args 2>&1 | Out-Null
    throw "安全边界测试应失败: $($args -join ' ')"
  } catch {
    if ($_.Exception.Message -like '安全边界测试应失败*') { throw }
  }
}

Write-Output 'formal-acceptance-suite safety tests passed'
exit 0
