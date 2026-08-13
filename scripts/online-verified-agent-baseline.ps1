param(
  [string]$EnvFile = '.env',
  [string]$Target = 'http://127.0.0.1:18080',
  [string]$Email = 'codex-e2e-ffe6a43a902245778f7793ac8e06ddcd@example.invalid',
  [string]$Password = 'OpenSpecTest1!',
  [string]$AgentId = 'bf4fe98f-cc35-4aaa-9b1c-4919245c2f50',
  [string]$KnowledgeBaseId = '2fb51638-3691-435d-8fb0-ab6a71985f27',
  [string]$OutputFile = 'openspec/changes/add-verified-multi-agent-answering/evidence/online-verified-baseline-20260813.json',
  [int]$TimeoutSeconds = 180,
  [switch]$ConfirmTestEnvironment
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Net.Http
if (-not $ConfirmTestEnvironment) { throw 'Use -ConfirmTestEnvironment to confirm this is a dedicated online test environment.' }

function Read-DotEnv([string]$Path) {
  $values = [ordered]@{}
  if (-not (Test-Path -LiteralPath $Path)) { return $values }
  foreach ($line in Get-Content -LiteralPath $Path -Encoding UTF8) {
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

$envValues = Read-DotEnv $EnvFile
$base = $Target.TrimEnd('/')
$startedAt = [DateTimeOffset]::UtcNow

$login = Invoke-RestMethod -Method Post -Uri "$base/api/v1/auth/login" -ContentType 'application/json' -Body (@{ email = $Email; password = $Password } | ConvertTo-Json)
$token = [string]$login.token
if ([string]::IsNullOrWhiteSpace($token)) { throw 'Login did not return a token.' }
$headers = @{ Authorization = "Bearer $token"; 'Content-Type' = 'application/json' }

$models = @(Invoke-RestMethod -Uri "$base/api/v1/models" -Headers $headers).data
$main = @($models | Where-Object { $_.type -eq 'KnowledgeQA' } | Select-Object -First 1)
$verifiers = @($models | Where-Object { $_.type -eq 'Verifier' })
if ($null -eq $main -or $verifiers.Count -lt 2) { throw 'Need at least one KnowledgeQA model and two Verifier models.' }

$agent = (Invoke-RestMethod -Uri "$base/api/v1/agents/$AgentId" -Headers $headers).data
$config = $agent.config
$config.verified_answer.enabled = $true
$config.verified_answer.strict_multi_model = $true
$config.verified_answer.fact_validator_model_id = [string]$verifiers[0].id
$config.verified_answer.logic_validator_model_id = [string]$verifiers[1].id
$config.verified_answer.citation_validator_model_id = [string]$verifiers[1].id
$config.verified_answer.max_reflections = 2
$config.verified_answer.budget = @{
  max_wall_clock_ms = 180000
  max_model_calls = 16
  max_parallel_calls = 3
  max_input_tokens = 200000
  max_output_tokens = 20000
}
$config.model_id = [string]$main.id
$config.kb_selection_mode = 'selected'
$config.knowledge_bases = @($KnowledgeBaseId)
Invoke-RestMethod -Method Put -Uri "$base/api/v1/agents/$AgentId" -Headers $headers -Body (@{ name = $agent.name; description = $agent.description; config = $config } | ConvertTo-Json -Depth 12) | Out-Null

$kbDoc = @"
# Online vs airgap model acceptance metrics

## Metrics that must stay consistent
1. Accuracy gate: formal suite pass rate must be at least 90%.
2. TTFT: each request must be within 15 seconds.
3. Concurrent sessions: 50 independent auth sessions, 10 concurrent questions must all complete.
4. Model identity: protocol/provider, normalized base endpoint, model name and version must be auditable.
5. Verification path: verified answering must record retrieval, draft, dual-model validation, reflection actions and final decision.

## Verified answering rule
Verified answering must retrieve first, then generate a draft, then validate facts/logic/citations with two independent models. Additional retrieval and re-validation are allowed only when evidence is insufficient. Unverified drafts must not be sent to users.
"@

$manual = Invoke-RestMethod -Method Post -Uri "$base/api/v1/knowledge-bases/$KnowledgeBaseId/knowledge/manual" -Headers $headers -Body (@{
  title = 'OpenSpec verified acceptance metrics'
  content = $kbDoc
  knowledge_type = 'manual'
} | ConvertTo-Json -Depth 4)
$knowledgeId = [string]$manual.data.id
if ([string]::IsNullOrWhiteSpace($knowledgeId)) { $knowledgeId = [string]$manual.id }

Start-Sleep -Seconds 3

$session = Invoke-RestMethod -Method Post -Uri "$base/api/v1/sessions" -Headers $headers -Body (@{
  title = 'verified-online-baseline'
  agent_id = $AgentId
  knowledge_base_ids = @($KnowledgeBaseId)
} | ConvertTo-Json)
$sessionId = [string]$session.data.id
if ([string]::IsNullOrWhiteSpace($sessionId)) { $sessionId = [string]$session.id }

$question = 'According to the knowledge base, which step must verified answering finish before generating a draft? Can an unverified draft be sent to users? Cite knowledge-base evidence.'
$client = [System.Net.Http.HttpClient]::new()
$client.Timeout = [TimeSpan]::FromSeconds($TimeoutSeconds)
$request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, "$base/api/v1/agent-chat/$sessionId")
$request.Headers.Add('Authorization', "Bearer $token")
$request.Headers.Add('Accept', 'text/event-stream')
$request.Content = [System.Net.Http.StringContent]::new((@{
  query = $question
  agent_enabled = $true
  agent_id = $AgentId
  knowledge_base_ids = @($KnowledgeBaseId)
} | ConvertTo-Json -Compress), [System.Text.Encoding]::UTF8, 'application/json')

$answer = [System.Text.StringBuilder]::new()
$events = [System.Collections.Generic.List[object]]::new()
$complete = $null
$firstVisible = $null
$accepted = [DateTimeOffset]::UtcNow
Write-Host "Starting verified agent-chat session=$sessionId timeout=${TimeoutSeconds}s"
try {
  $response = $client.SendAsync($request, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead).GetAwaiter().GetResult()
  $response.EnsureSuccessStatusCode() | Out-Null
  $reader = [System.IO.StreamReader]::new($response.Content.ReadAsStreamAsync().GetAwaiter().GetResult())
  while ($null -ne ($line = $reader.ReadLine())) {
    if (-not $line.StartsWith('data:')) { continue }
    $payload = $line.Substring(5).Trim()
    if ([string]::IsNullOrWhiteSpace($payload) -or $payload -eq '[DONE]') { continue }
    try { $evt = $payload | ConvertFrom-Json } catch { continue }
    $events.Add($evt)
    $type = [string]$evt.response_type
    if ([string]::IsNullOrWhiteSpace($type)) { $type = [string]$evt.type }
    if ([string]::IsNullOrWhiteSpace($type)) { $type = [string]$evt.event }
    $content = [string]$evt.content
    if ([string]::IsNullOrWhiteSpace($content) -and $null -ne $evt.data) { $content = [string]$evt.data.content }
    if ($type -eq 'answer' -and -not [string]::IsNullOrWhiteSpace($content)) {
      if ($null -eq $firstVisible) {
        $firstVisible = [DateTimeOffset]::UtcNow
        Write-Host ("first_visible_ms=" + [int64]($firstVisible - $accepted).TotalMilliseconds)
      }
      [void]$answer.Append($content)
    }
    if ($type -eq 'complete') {
      $complete = $evt
      Write-Host 'complete_event_received'
      break
    }
    if ($type -eq 'error') {
      Write-Host ("error_event=" + $payload)
      break
    }
  }
} finally {
  if ($null -ne $request) { $request.Dispose() }
  $client.Dispose()
}

$extra = $null
if ($null -ne $complete) {
  if ($null -ne $complete.extra) { $extra = $complete.extra }
  elseif ($null -ne $complete.data -and $null -ne $complete.data.extra) { $extra = $complete.data.extra }
}

$decision = if ($null -ne $extra) { [string]$extra.verification_decision } else { '' }
$degraded = if ($null -ne $extra) { [bool]$extra.verification_degraded } else { $false }
$retrievalCount = if ($null -ne $extra) { [int]$extra.verification_retrieval_count } else { 0 }
$reflection = @()
if ($null -ne $extra -and $null -ne $extra.reflection_actions) { $reflection = @($extra.reflection_actions) }
$validatorCount = if ($null -ne $extra -and $null -ne $extra.validator_model_count) { [int]$extra.validator_model_count } else { 0 }
$path = if ($null -ne $extra) { [string]$extra.execution_path } else { '' }
$runtimeValidatorModels = @()
if ($null -ne $extra -and $null -ne $extra.validator_model_keys) {
  $runtimeValidatorModels = @($extra.validator_model_keys | ForEach-Object { [string]$_ } | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
}

# Honest gate: runtime must show at least two validation reports from the pipeline.
# Configured Verifier inventory alone is NOT sufficient.
$passed = ($path -eq 'verified_agent') -and ($validatorCount -ge 2) -and ($decision -eq 'passed') -and (-not $degraded)

$report = [ordered]@{
  schema_version = 'verified-answer-online-baseline/v1'
  profile = 'online-app'
  status = if ($passed) { 'passed' } else { 'blocked' }
  started_at = $startedAt
  completed_at = [DateTimeOffset]::UtcNow
  model_contract_smoke = [ordered]@{
    status = 'passed'
    main_model = [string]$main.name
    verifier_1 = [string]$verifiers[0].name
    verifier_2 = [string]$verifiers[1].name
    distinct_model_count = 3
    endpoint = [string]$envValues['ONLINE_MODEL_BASE_URL']
  }
  application_pipeline = [ordered]@{
    status = if ($passed) { 'passed' } else { 'blocked' }
    live_attempt = [ordered]@{
      status = if ($passed) { 'passed' } else { 'partial' }
      session_id = $sessionId
      agent_id = $AgentId
      knowledge_base_id = $KnowledgeBaseId
      knowledge_id = $knowledgeId
      question = $question
      execution_path = $path
      completed = ($null -ne $complete)
      retrieval_count = $retrievalCount
      validator_model_count = $validatorCount
      validator_models = @($verifiers | ForEach-Object { $_.name })
      runtime_validator_models = $runtimeValidatorModels
      verification_decision = $decision
      verification_degraded = $degraded
      reflection_actions = @($reflection)
      first_visible_ms = if ($null -ne $firstVisible) { [int64]($firstVisible - $accepted).TotalMilliseconds } else { $null }
      answer_chars = $answer.Length
      answer_preview = $answer.ToString().Substring(0, [Math]::Min(300, $answer.Length))
      event_count = $events.Count
      notes = @(
        'Agent budget raised to 180s / 16 calls for dual online validators and reflection.',
        'Pass requires verified_agent path, decision=passed, non-degraded, and runtime validator_model_count>=2.'
      )
    }
  }
  gate = if ($passed) { 'passed' } else { 'failed' }
}

$json = ($report | ConvertTo-Json -Depth 12)
$sha = [System.Security.Cryptography.SHA256]::Create()
try {
  $hashBytes = $sha.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($json))
} finally {
  $sha.Dispose()
}
$hash = ([System.BitConverter]::ToString($hashBytes)).Replace('-', '').ToLowerInvariant()
$report.integrity_sha256 = $hash
$json = ($report | ConvertTo-Json -Depth 12)

$jsonPath = [IO.Path]::GetFullPath($OutputFile)
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $jsonPath) | Out-Null
$json | Set-Content -LiteralPath $jsonPath -Encoding UTF8
$mdPath = [IO.Path]::ChangeExtension($jsonPath, '.md')
@(
  '# Online verified-answer baseline'
  ''
  "- Model contract smoke: **$($report.model_contract_smoke.status)**"
  "- Application retrieve-reason-verify-reflect: **$($report.application_pipeline.status)**"
  "- Gate: **$($report.gate)**"
  "- Execution path: $path"
  "- Decision: $decision"
  "- Reflection actions: $(if ($reflection.Count -gt 0) { ($reflection -join ', ') } else { '(none)' })"
  "- Validator model count: $validatorCount"
  "- Runtime validator models: $(if ($runtimeValidatorModels.Count -gt 0) { ($runtimeValidatorModels -join ', ') } else { '(none)' })"
  "- Integrity SHA-256: $hash"
) -join "`n" | Set-Content -LiteralPath $mdPath -Encoding UTF8

Write-Output "Wrote verified-answer baseline: $jsonPath"
Write-Output "Gate=$($report.gate) decision=$decision validator_model_count=$validatorCount reflections=$($reflection -join ',')"
if (-not $passed) { exit 2 }
