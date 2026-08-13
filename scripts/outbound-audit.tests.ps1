$ErrorActionPreference = 'Stop'
$script = Join-Path $PSScriptRoot 'outbound-audit.ps1'
$output = Join-Path ([System.IO.Path]::GetTempPath()) ('weknora-outbound-' + [Guid]::NewGuid().ToString('N') + '.json')
try {
  & $script -ProbeUrl 'https://example.invalid/health' -NoNetwork -OutputFile $output | Out-Null
  if ($LASTEXITCODE -ne 0) { throw 'NoNetwork fixture 不应失败。' }
  $report = Get-Content -Raw -Encoding UTF8 $output | ConvertFrom-Json
  if ($report.schema -ne 'weknora-outbound-audit/v1' -or $report.status -ne 'passed' -or $report.successful_public_connections -ne 0) { throw '出站审计 fixture 契约错误。' }
  Write-Output 'outbound-audit safety tests passed'
} finally { Remove-Item -LiteralPath $output -Force -ErrorAction SilentlyContinue }
exit 0
