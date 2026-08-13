param(
  [string]$FreezeFile = 'scripts/testdata/baseline-v1-freeze.json',
  [string]$OutputFile = 'openspec/changes/add-air-gapped-model-deployment/evidence/frozen-inputs-v1.json'
)

$ErrorActionPreference = 'Stop'

function Resolve-RepositoryPath([string]$Path) {
  $root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
  $candidate = if ([System.IO.Path]::IsPathRooted($Path)) { $Path } else { Join-Path $root $Path }
  return (Resolve-Path $candidate).Path
}

function Get-Sha256([string]$Path) {
  return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

$freezePath = Resolve-RepositoryPath $FreezeFile
$freeze = Get-Content -LiteralPath $freezePath -Raw -Encoding UTF8 | ConvertFrom-Json
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$paths = [ordered]@{
  freeze = $FreezeFile
  routing_dataset = [string]$freeze.datasets.routing
  acceptance_suite = [string]$freeze.datasets.acceptance
  query_understand_prompt = [string]$freeze.prompts.query_understand.source
  complexity_classifier_prompt = [string]$freeze.prompts.complexity_classifier.source
  verified_answer_prompt = [string]$freeze.prompts.verified_answer.source
  acceptance_judge_prompt = [string]$freeze.prompts.acceptance_judge.source
  voice_prompt = [string]$freeze.prompts.voice.source
}
$files = [ordered]@{}
foreach ($entry in $paths.GetEnumerator()) {
  $path = Resolve-RepositoryPath $entry.Value
  $files[$entry.Key] = [ordered]@{ path = $entry.Value.Replace('\','/'); sha256 = Get-Sha256 $path }
}
$publicFreeze = $freeze | ConvertTo-Json -Depth 30 -Compress
$input = [ordered]@{
  schema = 'weknora-acceptance-frozen-inputs/v1'
  baseline_id = [string]$freeze.baseline_id
  status = [string]$freeze.status
  taxonomy = $freeze.routing
  thresholds = [ordered]@{ acceptance_gates = $freeze.acceptance_gates; verification = $freeze.verification }
  models = $freeze.models
  prompts = $freeze.prompts
  files = $files
  freeze_sha256 = ([System.BitConverter]::ToString([System.Security.Cryptography.SHA256]::HashData([System.Text.Encoding]::UTF8.GetBytes($publicFreeze)))).Replace('-','').ToLowerInvariant()
  generated_at = [DateTimeOffset]::UtcNow
}
$outputPath = if ([System.IO.Path]::IsPathRooted($OutputFile)) { $OutputFile } else { Join-Path $root $OutputFile }
$jsonPath = [System.IO.Path]::GetFullPath($outputPath)
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $jsonPath) | Out-Null
$input | ConvertTo-Json -Depth 30 | Set-Content -LiteralPath $jsonPath -Encoding UTF8
Write-Output "已生成冻结输入清单: $jsonPath"
Write-Output ($input | ConvertTo-Json -Depth 8)
