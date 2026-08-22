param(
  [Parameter(Mandatory=$true)][string]$SuiteFile,
  [Parameter(Mandatory=$true)][string]$Target,
  [Parameter(Mandatory=$true)][string]$TokenFile,
  [Parameter(Mandatory=$true)][string]$RunId,
  [ValidateSet('rag','agent')][string]$Mode = 'rag',
  [ValidateSet('desktop-lite','compose-airgap','helm-airgap')][string]$Profile = 'compose-airgap',
  [int]$TTFTLimitMs = 15000,
  [int]$TimeoutSeconds = 120,
  [switch]$ConfirmTestEnvironment,
  [switch]$DryRun,
  [string]$FrozenInputsFile,
  [string]$OutputFile
)

$ErrorActionPreference = 'Stop'

Add-Type -AssemblyName System.Net.Http

function Fail([string]$Message) { throw $Message }

if (-not (Test-Path -LiteralPath $SuiteFile -PathType Leaf)) { Fail "SuiteFile 不存在: $SuiteFile" }
if (-not (Test-Path -LiteralPath $TokenFile -PathType Leaf)) { Fail "TokenFile 不存在: $TokenFile" }
if ($TTFTLimitMs -lt 1 -or $TTFTLimitMs -gt 600000) { Fail 'TTFTLimitMs 必须在 1 到 600000 之间。' }
if ($TimeoutSeconds -lt 1 -or $TimeoutSeconds -gt 600) { Fail 'TimeoutSeconds 必须在 1 到 600 之间。' }
$targetUri = [Uri]$Target
if ($targetUri.Scheme -notin @('http','https') -or [string]::IsNullOrWhiteSpace($targetUri.Host)) { Fail 'Target 必须是带主机名的 HTTP(S) URL。' }
if ($targetUri.Host -match '(?i)(prod|production)') { Fail '默认拒绝名称包含 prod/production 的目标。' }

$suite = Get-Content -LiteralPath $SuiteFile -Raw -Encoding UTF8 | ConvertFrom-Json
$frozenInputs = $null
if ($FrozenInputsFile) {
  if (-not (Test-Path -LiteralPath $FrozenInputsFile -PathType Leaf)) { Fail "FrozenInputsFile 不存在: $FrozenInputsFile" }
  $frozenInputs = Get-Content -LiteralPath $FrozenInputsFile -Raw -Encoding UTF8 | ConvertFrom-Json
  if ($frozenInputs.schema -ne 'weknora-acceptance-frozen-inputs/v1' -or [string]::IsNullOrWhiteSpace([string]$frozenInputs.freeze_sha256)) { Fail 'FrozenInputsFile schema 或 hash 无效。' }
  $suitePath = (Resolve-Path -LiteralPath $SuiteFile).Path
  $suiteHash = (Get-FileHash -LiteralPath $suitePath -Algorithm SHA256).Hash.ToLowerInvariant()
  $rootPath = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
  $expectedSuitePath = (Join-Path $rootPath ([string]$frozenInputs.files.acceptance_suite.path).Replace('/','\')).Replace('/','\')
  if ($suitePath -ne $expectedSuitePath -or [string]$frozenInputs.files.acceptance_suite.sha256 -ne $suiteHash) {
    Fail 'SuiteFile 与冻结输入清单不一致；禁止使用未冻结的验收套件。'
  }
}
$formalStatus = [string]$suite.status
$formalSource = [string]$suite.source
$synthetic = ($formalStatus -match '(?i)synthetic|fixture' -or $formalSource -match '(?i)synthetic|fixture')
$formal = ([bool]$suite.frozen -and -not $synthetic)
$tokens = @(Get-Content -LiteralPath $TokenFile | ForEach-Object { $_.Trim() } | Where-Object { $_ })
if ($tokens.Count -lt 1) { Fail 'TokenFile 必须包含至少一个非空 token。' }

$base = $Target.TrimEnd('/')
$startedAt = [DateTimeOffset]::UtcNow
$results = [System.Collections.Generic.List[object]]::new()

function Get-PropertyValue($Object, [string]$Name) {
  if ($null -eq $Object) { return $null }
  $property = $Object.PSObject.Properties[$Name]
  if ($null -eq $property) { return $null }
  return $property.Value
}

function Get-String($Object, [string]$Name) {
  $value = Get-PropertyValue $Object $Name
  if ($null -eq $value) { return '' }
  return [string]$value
}

function New-Headers([string]$Token) {
  return @{ Authorization = "Bearer $Token"; 'Content-Type' = 'application/json' }
}

function Get-KnowledgeBaseIds() {
  $ids = [System.Collections.Generic.List[string]]::new()
  $binding = Get-PropertyValue $suite 'knowledge_binding'
  $kbId = Get-String $binding 'knowledge_base_id'
  if (-not [string]::IsNullOrWhiteSpace($kbId)) { $ids.Add($kbId) }
  foreach ($extra in @((Get-PropertyValue $binding 'knowledge_base_ids'))) {
    $value = [string]$extra
    if (-not [string]::IsNullOrWhiteSpace($value) -and -not $ids.Contains($value)) { $ids.Add($value) }
  }
  return @($ids)
}

function New-Session([string]$Token, [string]$CaseId) {
  $body = @{ title = "acceptance-$CaseId" } | ConvertTo-Json -Compress
  $response = Invoke-RestMethod -Method Post -Uri "$base/api/v1/sessions" -Headers (New-Headers $Token) -Body $body -TimeoutSec $TimeoutSeconds
  $session = if ($null -ne $response.data) { $response.data } else { $response }
  $id = Get-String $session 'id'
  if ([string]::IsNullOrWhiteSpace($id)) { Fail "案例 $CaseId 创建会话未返回 session id。" }
  return $id
}

function Get-ReferenceIds($Event) {
  $ids = [System.Collections.Generic.List[string]]::new()
  $refs = Get-PropertyValue $Event 'knowledge_references'
  if ($null -eq $refs) {
    $data = Get-PropertyValue $Event 'data'
    $refs = Get-PropertyValue $data 'references'
  }
  foreach ($ref in @($refs)) {
    $id = Get-String $ref 'id'
    if (-not [string]::IsNullOrWhiteSpace($id)) { $ids.Add($id) }
  }
  return @($ids | Select-Object -Unique)
}

function Read-ChatStream([string]$Token, [string]$SessionId, [string]$Question) {
  $accepted = [DateTimeOffset]::UtcNow
  $payload = @{ query = $Question; agent_enabled = ($Mode -eq 'agent') }
  $kbIds = @(Get-KnowledgeBaseIds)
  if ($kbIds.Count -gt 0) { $payload.knowledge_base_ids = @($kbIds) }
  $body = $payload | ConvertTo-Json -Compress
  $client = [System.Net.Http.HttpClient]::new()
  $client.Timeout = [TimeSpan]::FromSeconds($TimeoutSeconds)
  $endpoint = if ($Mode -eq 'agent') { 'agent-chat' } else { 'knowledge-chat' }
  $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, "$base/api/v1/$endpoint/$SessionId")
  $request.Headers.Add('Authorization', "Bearer $Token")
  $request.Headers.Add('Accept', 'text/event-stream')
  $request.Content = [System.Net.Http.StringContent]::new($body, [System.Text.Encoding]::UTF8, 'application/json')
  $cancel = [System.Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds($TimeoutSeconds))
  $answer = [System.Text.StringBuilder]::new()
  $citationIds = [System.Collections.Generic.List[string]]::new()
  $extra = $null
  $completed = $false
  $firstVisible = $null
  $completedAt = $null
  $errorMessage = ''
  try {
    $response = $client.SendAsync($request, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead, $cancel.Token).GetAwaiter().GetResult()
    $response.EnsureSuccessStatusCode() | Out-Null
    $stream = $response.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
    $reader = [System.IO.StreamReader]::new($stream)
    try {
      while ($null -ne ($line = $reader.ReadLineAsync($cancel.Token).GetAwaiter().GetResult())) {
        if (-not $line.StartsWith('data:')) { continue }
        $raw = $line.Substring(5).Trim()
        if ($raw -eq '[DONE]' -or [string]::IsNullOrWhiteSpace($raw)) { continue }
        try { $event = $raw | ConvertFrom-Json } catch { continue }
        $type = Get-String $event 'response_type'
        if ($type -eq 'answer') {
          $text = Get-String $event 'content'
          if (-not [string]::IsNullOrWhiteSpace($text)) {
            if ($null -eq $firstVisible) { $firstVisible = [DateTimeOffset]::UtcNow }
            [void]$answer.Append($text)
          }
        } elseif ($type -eq 'references') {
          foreach ($id in @(Get-ReferenceIds $event)) { $citationIds.Add($id) }
          $data = Get-PropertyValue $event 'data'
          $extra = Get-PropertyValue $data 'extra'
        } elseif ($type -eq 'error' -or $null -ne (Get-PropertyValue $event 'error')) {
          $errorMessage = if ((Get-PropertyValue $event 'error')) { [string](Get-PropertyValue $event 'error') } else { '服务端返回 error 事件。' }
          break
        } elseif ($type -eq 'complete') {
          $completeData = Get-PropertyValue $event 'data'
          $completeExtra = Get-PropertyValue $completeData 'extra'
          if ($null -ne $completeExtra) { $extra = $completeExtra }
          $completeRefs = Get-PropertyValue $completeData 'knowledge_refs'
          foreach ($id in @($completeRefs | ForEach-Object { Get-String $_ 'id' })) { if ($id) { $citationIds.Add($id) } }
          $completed = $true
          $completedAt = [DateTimeOffset]::UtcNow
          break
        }
      }
    } finally {
      $reader.Dispose(); $stream.Dispose(); $response.Dispose()
    }
    if (-not $completed -and [string]::IsNullOrWhiteSpace($errorMessage)) { $errorMessage = '流式响应未返回 complete 事件。' }
  } catch {
    $errorMessage = $_.Exception.Message
  } finally {
    $cancel.Dispose(); $request.Dispose(); $client.Dispose()
  }
  if ($null -eq $completedAt) { $completedAt = [DateTimeOffset]::UtcNow }
  $timedOut = $errorMessage -match '(?i)timeout|timed out|operation was canceled|canceled'
  $timing = [ordered]@{
    accepted_at = $accepted
    first_visible_at = $firstVisible
    completed_at = $completedAt
    ttft_ms = if ($null -ne $firstVisible) { [int64]($firstVisible - $accepted).TotalMilliseconds } else { 0 }
    timed_out = $timedOut
    error = $errorMessage
  }
  return [pscustomobject]@{ answer = $answer.ToString(); citation_ids = @($citationIds | Select-Object -Unique); extra = $extra; timing = $timing; completed = $completed }
}

function Get-NormalizedAnswer([string]$Answer) {
  if ([string]::IsNullOrWhiteSpace($Answer)) { return '' }
  $text = $Answer
  $text = [regex]::Replace($text, '\*\*(.*?)\*\*', '$1')
  $text = [regex]::Replace($text, '\*(.*?)\*', '$1')
  $text = [regex]::Replace($text, '`+', '')
  $text = [regex]::Replace($text, '\s+', '')
  return $text
}

function Evaluate-Case($Case, $Execution) {
  $answer = [string]$Execution.answer
  $normalized = Get-NormalizedAnswer $answer
  $passed = -not [string]::IsNullOrWhiteSpace($answer) -and [string]::IsNullOrWhiteSpace([string]$Execution.timing.error) -and [bool]$Execution.completed
  $kind = [string]$Case.answer_kind
  if ($kind -in @('refuse','unanswerable') -and $answer -notmatch '(?i)无法|不能|不足|不确定|未覆盖|没有足够|cannot|insufficient|refuse|not enough') { $passed = $false }
  foreach ($claim in @($Case.required_claims)) {
    if ([string]::IsNullOrWhiteSpace([string]$claim)) { continue }
    $claimNorm = Get-NormalizedAnswer ([string]$claim)
    if ($normalized -notlike "*$claimNorm*" -and $answer -notlike "*$claim*") { $passed = $false }
  }
  foreach ($claim in @($Case.forbidden_claims)) {
    if ([string]::IsNullOrWhiteSpace([string]$claim)) { continue }
    $claimNorm = Get-NormalizedAnswer ([string]$claim)
    if ($normalized -like "*$claimNorm*" -or $answer -like "*$claim*") { $passed = $false }
  }
  $citationSet = @($Execution.citation_ids)
  foreach ($required in @($Case.evidence_chunk_ids)) { if ($required -and $citationSet -notcontains [string]$required) { $passed = $false } }
  return $passed
}

function Invoke-Case($Case) {
  $caseId = [string]$Case.id
  $question = [string]$Case.question
  if ([string]::IsNullOrWhiteSpace($question)) {
    return [ordered]@{ case_id = $caseId; passed = $false; error = '冻结案例缺少 question，不能调用真实问答入口。'; blocked = $true }
  }
  if ($DryRun) {
    return [ordered]@{ case_id = $caseId; passed = $false; error = 'dry-run 未执行真实 RAG/Agent；按要求不得通过。'; blocked = $true }
  }
  $token = $tokens[0]
  $maxAttempts = 2
  $lastResult = $null
  for ($attempt = 1; $attempt -le $maxAttempts; $attempt++) {
    $sessionId = New-Session $token "$caseId-$attempt"
    $rounds = @($Case.rounds)
    $streams = [System.Collections.Generic.List[object]]::new()
    if ([bool]$Case.multi_turn -and $rounds.Count -gt 0) {
      foreach ($round in $rounds) {
        $roundQuestion = Get-String $round 'question'
        if ([string]::IsNullOrWhiteSpace($roundQuestion)) { $roundQuestion = $question }
        $streams.Add((Read-ChatStream $token $sessionId $roundQuestion))
      }
    } else {
      $streams.Add((Read-ChatStream $token $sessionId $question))
    }
    $stream = $streams[-1]
    $combinedAnswer = (($streams | ForEach-Object { [string]$_.answer }) -join "`n").Trim()
    $combinedCitations = @($streams | ForEach-Object { @($_.citation_ids) } | Select-Object -Unique)
    $firstTiming = $streams[0].timing
    $extra = $stream.extra
    $routing = Get-PropertyValue $extra 'routing'
    $graph = Get-PropertyValue $extra 'graph'
    $completedAll = (@($streams | Where-Object { -not $_.completed }).Count -eq 0)
    $streamError = (($streams | ForEach-Object { [string]$_.timing.error }) | Where-Object { $_ } | Select-Object -First 1)
    if (-not $completedAll -and [string]::IsNullOrWhiteSpace([string]$streamError)) { $streamError = '多轮对话存在未完成流。' }
    $execution = [ordered]@{
      answer = $combinedAnswer
      evidence_chunk_ids = @($combinedCitations)
      citation_chunk_ids = @($combinedCitations)
      timing = [ordered]@{
        accepted_at = $firstTiming.accepted_at
        first_visible_at = $firstTiming.first_visible_at
        completed_at = $stream.timing.completed_at
        ttft_ms = [int64]$firstTiming.ttft_ms
        timed_out = [bool](@($streams | Where-Object { $_.timing.timed_out }).Count -gt 0)
        error = [string]$streamError
        attempts = $attempt
      }
      routing = [ordered]@{
        expected_level = [string]$Case.complexity_level
        expected_subtype = [string]$Case.complexity_subtype
        actual_level = Get-String $routing 'level'
        actual_subtype = Get-String $routing 'subtype'
        needs_entity_relation = [bool](Get-PropertyValue $routing 'needs_entity_relation')
        actual_action = Get-String $routing 'actual_action'
        planned_action = Get-String $routing 'planned_action'
        degradation_reason = Get-String $routing 'degradation_reason'
      }
      graph = [ordered]@{
        requested = [bool](Get-PropertyValue $graph 'requested')
        used = [bool](Get-PropertyValue $graph 'used')
        reason = Get-String $graph 'reason'
      }
      verification_path = Get-String $extra 'verification_path'
      degradation_reason = Get-String $routing 'degradation_reason'
      round_count = $streams.Count
    }
    $evalCase = [pscustomobject]@{
      answer = $combinedAnswer
      citation_ids = @($combinedCitations)
      completed = $completedAll
      timing = [pscustomobject]$execution.timing
    }
    $passed = Evaluate-Case $Case $evalCase
    if ([int64]$firstTiming.ttft_ms -gt $TTFTLimitMs) { $passed = $false }
    $lastResult = [ordered]@{ case_id = $caseId; passed = $passed; timed_out = [bool]$execution.timing.timed_out; error = [string]$execution.timing.error; execution = $execution }
    if ($passed) { break }
    if ($attempt -lt $maxAttempts) { Start-Sleep -Milliseconds 800 }
  }
  return $lastResult
}

if (-not $formal) {
  $gate = 'incomplete'
  $reason = '套件不是已冻结的专家验证数据；当前 synthetic/fixture 不能作为正式验收。'
} elseif (-not $ConfirmTestEnvironment) {
  $gate = 'incomplete'
  $reason = '未使用 ConfirmTestEnvironment，拒绝发起真实问答请求。'
} elseif ($DryRun) {
  $gate = 'incomplete'
  $reason = 'dry-run 未执行真实 RAG/Agent；不得通过。'
} else {
  foreach ($case in @($suite.cases)) { $results.Add((Invoke-Case $case)) }
  $failed = @($results | Where-Object { -not $_.passed }).Count
  $caseCount = @($suite.cases).Count
  $passedCount = $caseCount - $failed
  $accuracy = if ($caseCount -gt 0) { [math]::Round(($passedCount / $caseCount), 4) } else { 0 }
  $gate = if ($failed -eq 0 -and $results.Count -eq $caseCount -and $accuracy -ge 0.9) { 'passed' } else { 'failed' }
  $reason = if ($gate -eq 'passed') { '' } else { "至少一个真实案例未通过或准确率不足 90%（accuracy=$accuracy）；请查看 execution/error/timing。" }
}

$payload = [ordered]@{
  schema = 'weknora-formal-acceptance/v1'
  run_id = $RunId
  suite_file = (Resolve-Path -LiteralPath $SuiteFile).Path
  target = $Target
  profile = $Profile
  mode = $Mode
  frozen_inputs = if ($null -ne $frozenInputs) { [ordered]@{ path = (Resolve-Path -LiteralPath $FrozenInputsFile).Path; freeze_sha256 = [string]$frozenInputs.freeze_sha256 } } else { $null }
  dry_run = [bool]$DryRun
  formal_suite = $formal
  gate = $gate
  reason = $reason
  case_count = @($results).Count
  passed_count = @($results | Where-Object { $_.passed }).Count
  failed_count = @($results | Where-Object { -not $_.passed }).Count
  accuracy = if (@($results).Count -gt 0) { [math]::Round((@($results | Where-Object { $_.passed }).Count / @($results).Count), 4) } else { 0 }
  knowledge_base_ids = @(Get-KnowledgeBaseIds)
  started_at = $startedAt
  completed_at = [DateTimeOffset]::UtcNow
  results = @($results)
}

if ($formal -and -not $DryRun -and $ConfirmTestEnvironment -and @($results).Count -gt 0) {
  $payload.persistence = [ordered]@{ attempted = $true; ok = $false; error = '' }
  try {
    foreach ($result in @($results)) {
      $resultBody = $result | ConvertTo-Json -Depth 20 -Compress
      Invoke-RestMethod -Method Post -Uri "$base/api/v1/acceptance/runs/$RunId/cases/$($result.case_id)" -Headers (New-Headers $tokens[0]) -Body $resultBody -TimeoutSec $TimeoutSeconds | Out-Null
    }
    $final = Invoke-RestMethod -Method Post -Uri "$base/api/v1/acceptance/runs/$RunId/finalize" -Headers (New-Headers $tokens[0]) -Body '{}' -TimeoutSec $TimeoutSeconds
    $payload.finalized = $true
    $payload.server_report = if ($null -ne $final.data) { $final.data } else { $final }
    $payload.persistence.ok = $true
  } catch {
    # Local formal accuracy/TTFT evidence remains authoritative when the server
    # run ledger was not pre-created; keep gate based on executed cases.
    $payload.persistence.error = $_.Exception.Message
    $payload.persistence_warning = "案例结果未写入服务器验收账本: $($_.Exception.Message)"
  }
}

$json = $payload | ConvertTo-Json -Depth 30
if ($OutputFile) { $json | Set-Content -LiteralPath $OutputFile -Encoding UTF8 }
Write-Output $json
if ($payload.gate -in @('failed','incomplete')) { exit 2 }
