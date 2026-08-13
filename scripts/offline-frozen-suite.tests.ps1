$ErrorActionPreference = 'Stop'
$script = Join-Path $PSScriptRoot 'offline-frozen-suite.ps1'
$freeze = Join-Path $PSScriptRoot 'testdata/baseline-v1-freeze.json'
$frozen = Join-Path ([System.IO.Path]::GetTempPath()) ('weknora-offline-frozen-' + [Guid]::NewGuid().ToString('N') + '.json')
$reportPath = Join-Path ([System.IO.Path]::GetTempPath()) ('weknora-offline-report-' + [Guid]::NewGuid().ToString('N') + '.json')
$suite = Join-Path $PSScriptRoot 'testdata/baseline-v1-acceptance-suite-expert.json'
$audit = Join-Path ([System.IO.Path]::GetTempPath()) ('weknora-offline-audit-' + [Guid]::NewGuid().ToString('N') + '.json')
try {
  & (Join-Path $PSScriptRoot 'freeze-acceptance-inputs.ps1') -OutputFile $frozen | Out-Null
  & (Join-Path $PSScriptRoot 'outbound-audit.ps1') -ProbeUrl 'https://example.invalid/health' -NoNetwork -OutputFile $audit | Out-Null
  $output = & $script -FrozenInputsFile $frozen -SuiteFile $suite -Profile desktop-lite -OutboundAuditFile $audit -DryRun -OutputFile $reportPath 2>&1 | Out-String
  if ($LASTEXITCODE -ne 3) { throw "dry-run 应明确 blocked，实际为 $LASTEXITCODE。" }
  $report = Get-Content -Raw -Encoding UTF8 $reportPath | ConvertFrom-Json
  if ($report.schema -ne 'weknora-offline-frozen-suite/v1' -or $report.single_node_gate -ne 'blocked' -or $report.server_load_gate -ne 'not_applicable') { throw '冻结套件报告缺少独立 profile 门禁语义。' }
  if (@($report.checks | Where-Object { $_.name -eq 'outbound_audit' -and $_.status -eq 'passed' }).Count -ne 1) { throw '冻结套件未接入可复现出站审计结果。' }
  Write-Output 'offline-frozen-suite safety tests passed'
} finally { Remove-Item -LiteralPath $frozen,$reportPath,$audit -Force -ErrorAction SilentlyContinue }
exit 0
