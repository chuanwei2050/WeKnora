$ErrorActionPreference = 'Stop'
$script = Join-Path $PSScriptRoot 'freeze-acceptance-inputs.ps1'
$output = Join-Path ([System.IO.Path]::GetTempPath()) ('weknora-frozen-inputs-' + [Guid]::NewGuid().ToString('N') + '.json')
try {
  & $script -OutputFile $output | Out-Null
  $input = Get-Content -Raw -Encoding UTF8 $output | ConvertFrom-Json
  if ($input.schema -ne 'weknora-acceptance-frozen-inputs/v1') { throw '冻结清单 schema 错误。' }
  if ([string]::IsNullOrWhiteSpace([string]$input.freeze_sha256)) { throw '冻结清单缺少 freeze_sha256。' }
  if ($input.files.routing_dataset.sha256.Length -ne 64) { throw '数据集未生成 SHA-256。' }
  Write-Output 'freeze-acceptance-inputs safety tests passed'
} finally {
  Remove-Item -LiteralPath $output -Force -ErrorAction SilentlyContinue
}
exit 0
