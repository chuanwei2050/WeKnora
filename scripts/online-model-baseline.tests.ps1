$ErrorActionPreference = 'Stop'
$script = Join-Path $PSScriptRoot 'online-model-baseline.ps1'
$output = Join-Path ([System.IO.Path]::GetTempPath()) ('weknora-online-baseline-' + [Guid]::NewGuid().ToString('N'))
try {
  & $script -DryRun -SkipVoice -OutputDirectory $output | Out-Null
  if (-not $?) { throw 'dry-run 应成功退出。' }
  $json = Get-Content -Raw -Encoding UTF8 (Join-Path $output 'online-model-baseline.json') | ConvertFrom-Json
  if ($json.gate -ne 'dry_run') { throw "dry-run gate = $($json.gate)" }
  if ($json.formal_acceptance.status -ne 'blocked') { throw 'dry-run 不得伪造正式验收通过。' }
  $jsonText = Get-Content -Raw -Encoding UTF8 (Join-Path $output 'online-model-baseline.json')
  if ($jsonText -match 'sk-[A-Za-z0-9]') { throw '报告不得包含 API key。' }
  if (@($json.probes | Where-Object { $_.status -ne 'dry_run' }).Count -ne 0) { throw 'dry-run 不得发起模型调用。' }
  Write-Output 'online-model-baseline safety tests passed'
} finally {
  Remove-Item -LiteralPath $output -Recurse -Force -ErrorAction SilentlyContinue
}
exit 0
