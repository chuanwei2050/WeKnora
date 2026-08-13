$ErrorActionPreference = 'Stop'
$script = Join-Path $PSScriptRoot 'offline-diff-audit.ps1'
$out = Join-Path ([System.IO.Path]::GetTempPath()) ('weknora-diff-' + [Guid]::NewGuid().ToString('N') + '.json')
try {
  $output = & $script -FrozenInputsFile 'openspec/changes/add-air-gapped-model-deployment/evidence/frozen-inputs-v1.json' -OnlineBaselineFile 'openspec/changes/add-air-gapped-model-deployment/evidence/online-baseline-v5/online-model-baseline.json' -OutputFile $out 2>&1 | Out-String
  if ($LASTEXITCODE -ne 3) { throw "缺少 profile 时必须 blocked，实际为 $LASTEXITCODE。" }
  $report = Get-Content -Raw -Encoding UTF8 $out | ConvertFrom-Json
  if ($report.schema -ne 'weknora-online-offline-diff/v1' -or $report.gate -ne 'blocked') { throw '差异报告 schema/gate 错误。' }
  if (@($report.checks | Where-Object name -eq 'model_identity_diff').Count -ne 1) { throw '缺少模型身份差异检查。' }
  if (@($report.checks | Where-Object name -eq 'package_model_coverage').Count -ne 0) { throw '未提供包时不应虚构模型覆盖检查结果。' }
  Write-Output 'offline-diff-audit safety tests passed'
} finally { Remove-Item -LiteralPath $out -Force -ErrorAction SilentlyContinue }
exit 0
