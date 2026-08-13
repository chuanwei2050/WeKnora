$ErrorActionPreference = 'Stop'

function Assert-True([bool]$Condition, [string]$Message) {
  if (-not $Condition) { throw $Message }
}

$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $root

$freeze = 'openspec/changes/add-air-gapped-model-deployment/evidence/frozen-inputs-v1.json'
$online = 'openspec/changes/add-air-gapped-model-deployment/evidence/online-baseline-v5/online-model-baseline.json'
$private = 'openspec/changes/add-air-gapped-model-deployment/evidence/desktop-lite-private-e2e-20260812.json'
$suite = 'scripts/testdata/baseline-v1-acceptance-suite-expert.json'
$freezeManifest = 'scripts/testdata/baseline-v1-freeze.json'

Assert-True (Test-Path $freeze) '缺少冻结输入证据。'
Assert-True (Test-Path $online) '缺少线上基线证据。'
Assert-True (Test-Path $private) '缺少 private-network single-node 证据。'
Assert-True (Test-Path $suite) '缺少专家冻结验收套件。'

$frozen = Get-Content -Raw -Encoding UTF8 $freeze | ConvertFrom-Json
$baseline = Get-Content -Raw -Encoding UTF8 $online | ConvertFrom-Json
$manifest = Get-Content -Raw -Encoding UTF8 $freezeManifest | ConvertFrom-Json
$privateEvidence = Get-Content -Raw -Encoding UTF8 $private | ConvertFrom-Json

# 1) online/offline freeze input consistency
Assert-True ($frozen.schema -eq 'weknora-acceptance-frozen-inputs/v1') '冻结输入 schema 错误。'
Assert-True (-not [string]::IsNullOrWhiteSpace([string]$frozen.freeze_sha256)) '冻结输入缺少 freeze_sha256。'
Assert-True ($frozen.files.routing_dataset.path -eq [string]$manifest.datasets.routing) '冻结清单与 freeze 声明的 routing 数据集不一致。'
Assert-True ($frozen.files.acceptance_suite.path -eq [string]$manifest.datasets.acceptance) '冻结清单与 freeze 声明的 acceptance 套件不一致。'
$suiteHash = (Get-FileHash -LiteralPath $suite -Algorithm SHA256).Hash.ToLowerInvariant()
Assert-True ($frozen.files.acceptance_suite.sha256 -eq $suiteHash) 'acceptance suite SHA-256 与冻结输入不一致。'
$routingPath = [string]$manifest.datasets.routing
$routingHash = (Get-FileHash -LiteralPath $routingPath -Algorithm SHA256).Hash.ToLowerInvariant()
Assert-True ($frozen.files.routing_dataset.sha256 -eq $routingHash) 'routing dataset SHA-256 与冻结输入不一致。'
Assert-True ($baseline.gate -eq 'passed' -and $baseline.formal_acceptance.status -eq 'blocked') '线上基线不能同时满足工程通过和正式验收阻断契约。'
Assert-True ($frozen.models.chat.base_url_env -eq 'ONLINE_LLM_MODEL_BASE_URL') 'chat 未绑定角色化 endpoint。'
Assert-True ($frozen.models.verifier_1.base_url_env -eq 'ONLINE_VERIFIER_MODEL_1_BASE_URL') 'verifier_1 未绑定角色化 endpoint。'
Assert-True ($frozen.models.tts.base_url_env -eq 'ONLINE_TTS_MODEL_BASE_URL') 'tts 未绑定角色化 endpoint。'

$appDockerfile = Get-Content -Raw -Encoding UTF8 'docker/Dockerfile.app'
Assert-True ($appDockerfile -notmatch 'astral\.sh/uv/install\.sh') 'Dockerfile.app 仍通过网络脚本安装 uv。'
Assert-True ($appDockerfile -match '(?m)^ARG UV_IMAGE=') 'Dockerfile.app 未声明可镜像/可缓存的 UV_IMAGE。'
Assert-True ($appDockerfile -match '(?m)^ARG UV_IMAGE=.*@sha256:[a-f0-9]{64}$') 'Dockerfile.app 的默认 UV_IMAGE 必须固定 digest。'
Assert-True ($appDockerfile -match '(?m)^COPY --from=uv /usr/local/bin/uvx /usr/local/bin/uvx') 'Dockerfile.app 未从显式 uv stage 提供 uvx。'

# 2) disconnected / dry-run full-chain gate semantics + server-load isolation
$desktopDry = Join-Path ([System.IO.Path]::GetTempPath()) ('weknora-contract-desktop-' + [Guid]::NewGuid().ToString('N') + '.json')
$composeDryPath = Join-Path ([System.IO.Path]::GetTempPath()) ('weknora-contract-compose-' + [Guid]::NewGuid().ToString('N') + '.json')
$diffOut = Join-Path ([System.IO.Path]::GetTempPath()) ('weknora-diff-contract-' + [Guid]::NewGuid().ToString('N') + '.json')
try {
  & (Join-Path $PSScriptRoot 'offline-frozen-suite.ps1') -FrozenInputsFile $freeze -SuiteFile $suite -Profile desktop-lite -DryRun -OutputFile $desktopDry | Out-Null
  $offline = Get-Content -Raw -Encoding UTF8 $desktopDry | ConvertFrom-Json
  Assert-True ($offline.single_node_gate -eq 'blocked') 'desktop-lite dry-run 必须独立阻断 single-node。'
  Assert-True ($offline.server_load_gate -eq 'not_applicable') 'desktop-lite 必须将 server-load 标记为 not_applicable，不得用 single-node 结果替代。'
  Assert-True ($offline.frozen_inputs.freeze_sha256 -eq $frozen.freeze_sha256) 'offline 报告未复用同一冻结输入 hash。'

  & (Join-Path $PSScriptRoot 'offline-frozen-suite.ps1') -FrozenInputsFile $freeze -SuiteFile $suite -Profile compose-airgap -DryRun -OutputFile $composeDryPath | Out-Null
  $composeDry = Get-Content -Raw -Encoding UTF8 $composeDryPath | ConvertFrom-Json
  Assert-True ($composeDry.server_load_gate -eq 'blocked') 'compose-airgap dry-run 的 server-load 应 blocked，而不是 not_applicable。'
  Assert-True ($composeDry.single_node_gate -eq 'blocked') 'compose-airgap dry-run 缺少部署清单时 single-node 应 blocked。'

  # 3) private-network model forces single-node failure
  Assert-True ($privateEvidence.single_node_gate.status -eq 'failed_as_expected') 'private-network 模型必须使 single-node 失败。'
  Assert-True (@($privateEvidence.single_node_gate.non_same_host | Where-Object { $_.location -eq 'private-network' }).Count -ge 1) 'private-network 证据必须列出非 same-host 模型端点。'

  # 4) diff report contract
  & (Join-Path $PSScriptRoot 'offline-diff-audit.ps1') `
    -FrozenInputsFile $freeze `
    -OnlineBaselineFile $online `
    -DesktopLiteReport $desktopDry `
    -OutputFile $diffOut | Out-Null
  if ($LASTEXITCODE -notin @(0, 2, 3)) { throw "offline-diff-audit 异常退出码: $LASTEXITCODE" }
  $diff = Get-Content -Raw -Encoding UTF8 $diffOut | ConvertFrom-Json
  Assert-True ($diff.schema -eq 'weknora-online-offline-diff/v1') '差异报告 schema 错误。'
  Assert-True ($diff.frozen_inputs.freeze_sha256 -eq $frozen.freeze_sha256) '差异报告冻结 hash 不一致。'
  Assert-True (@($diff.checks | Where-Object name -eq 'model_identity_diff').Count -eq 1) '差异报告缺少模型身份检查。'
  Assert-True ($diff.gate -in @('passed', 'blocked', 'failed')) '差异报告缺少可穷举 gate。'

  Write-Output 'airgap-acceptance-contract tests passed'
} finally {
  Remove-Item -LiteralPath $desktopDry, $composeDryPath, $diffOut -Force -ErrorAction SilentlyContinue
}
exit 0
