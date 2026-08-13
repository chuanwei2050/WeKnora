param(
  [string]$EnvFile = '.env',
  [string]$DatasetFile = 'scripts/testdata/baseline-v1-routing-dataset-expert.json',
  [string]$OutputFile = 'openspec/changes/add-question-complexity-routing/evidence/online-routing-baseline-v3.json',
  [int]$TimeoutSeconds = 60
)

$ErrorActionPreference = 'Stop'

function Read-DotEnv([string]$Path) {
  $values = [ordered]@{}
  foreach ($line in Get-Content -LiteralPath $Path) {
    $trimmed = $line.Trim()
    if ([string]::IsNullOrWhiteSpace($trimmed) -or $trimmed.StartsWith('#')) { continue }
    if ($trimmed -notmatch '^([^=]+)=(.*)$') { continue }
    $values[$matches[1].Trim()] = $matches[2].Trim().Trim('"').Trim("'")
  }
  for ($pass = 0; $pass -lt 8; $pass++) {
    $changed = $false
    foreach ($key in @($values.Keys)) {
      $old = [string]$values[$key]
      $new = [regex]::Replace($old, '\$\{([^}]+)\}', { param($m) if ($values.Contains($m.Groups[1].Value)) { [string]$values[$m.Groups[1].Value] } else { $m.Value } })
      if ($new -ne $old) { $values[$key] = $new; $changed = $true }
    }
    if (-not $changed) { break }
  }
  return $values
}

$values = Read-DotEnv $EnvFile
foreach ($name in @('ONLINE_LLM_MODEL_API_KEY', 'ONLINE_LLM_MODEL_BASE_URL', 'ONLINE_LLM_MODEL_NAME')) {
  if ([string]::IsNullOrWhiteSpace([string]$values[$name])) { throw "缺少环境变量: $name" }
}

$tokenFile = Join-Path ([IO.Path]::GetTempPath()) ('openspec-routing-token-' + [Guid]::NewGuid().ToString('N') + '.txt')
try {
  [string]$values['ONLINE_LLM_MODEL_API_KEY'] | Set-Content -LiteralPath $tokenFile -NoNewline
  & (Join-Path $PSScriptRoot 'complexity-routing-benchmark.ps1') `
    -Target $values['ONLINE_LLM_MODEL_BASE_URL'] `
    -Model $values['ONLINE_LLM_MODEL_NAME'] `
    -DatasetFile $DatasetFile `
    -TokenFile $tokenFile `
    -TimeoutSeconds $TimeoutSeconds `
    -OutputFile $OutputFile `
    -ConfirmTestEnvironment
  exit $LASTEXITCODE
} finally {
  Remove-Item -LiteralPath $tokenFile -Force -ErrorAction SilentlyContinue
}
