param(
  [string]$EnvFile = '.env',
  [string]$OutputDirectory = 'openspec/changes/add-air-gapped-model-deployment/evidence/online-baseline',
  [string]$AudioFile = 'internal/assets/asr_test.wav',
  [int]$TimeoutSeconds = 120,
  [switch]$SkipVoice,
  [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

Add-Type -AssemblyName System.Net.Http

function Read-DotEnv {
  param([Parameter(Mandatory=$true)][string]$Path)

  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
    throw "EnvFile 不存在: $Path"
  }

  $values = [ordered]@{}
  foreach ($line in Get-Content -LiteralPath $Path) {
    $trimmed = $line.Trim()
    if ([string]::IsNullOrWhiteSpace($trimmed) -or $trimmed.StartsWith('#')) { continue }
    if ($trimmed -notmatch '^([^=]+)=(.*)$') { continue }
    $key = $matches[1].Trim()
    $value = $matches[2].Trim()
    if ($value.Length -ge 2 -and (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'")))) {
      $value = $value.Substring(1, $value.Length - 2)
    }
    $values[$key] = $value
  }

  for ($pass = 0; $pass -lt 8; $pass++) {
    $changed = $false
    foreach ($key in @($values.Keys)) {
      $old = [string]$values[$key]
      $new = [regex]::Replace($old, '\$\{([^}]+)\}', {
        param($match)
        $reference = $match.Groups[1].Value
        if ($values.Contains($reference)) { return [string]$values[$reference] }
        return $match.Value
      })
      if ($new -ne $old) {
        $values[$key] = $new
        $changed = $true
      }
    }
    if (-not $changed) { break }
  }
  return $values
}

function Get-RequiredValue {
  param([hashtable]$Values, [string]$Name)
  $value = [string]$Values[$Name]
  if ([string]::IsNullOrWhiteSpace($value)) { throw "缺少环境变量: $Name" }
  return $value
}

function Get-OptionalValue {
  param([hashtable]$Values, [string]$Name, [string]$Default = '')
  $value = [string]$Values[$Name]
  if ([string]::IsNullOrWhiteSpace($value)) { return $Default }
  return $value
}

function Get-RoleConfig {
  param([hashtable]$Values, [string]$Role, [string]$Prefix)
  return [ordered]@{
    role = $Role
    model = Get-RequiredValue $Values "${Prefix}_NAME"
    base_url = Get-RequiredValue $Values "${Prefix}_BASE_URL"
    api_key = Get-RequiredValue $Values "${Prefix}_API_KEY"
  }
}

function Get-PublicEndpoint {
  param([string]$BaseUrl)
  try {
    $uri = [Uri]$BaseUrl
    return "$($uri.Scheme)://$($uri.Host)$($uri.AbsolutePath.TrimEnd('/'))"
  } catch {
    return '<invalid-url>'
  }
}

function New-Result {
  param([string]$Name, [string]$Model, [string]$Endpoint)
  return [ordered]@{
    name = $Name
    model = $Model
    endpoint = Get-PublicEndpoint $Endpoint
    status = 'blocked'
    elapsed_ms = $null
    details = $null
    error = $null
  }
}

function Invoke-JsonRequest {
  param(
    [Parameter(Mandatory=$true)][System.Net.Http.HttpClient]$Client,
    [Parameter(Mandatory=$true)][string]$ApiKey,
    [Parameter(Mandatory=$true)][string]$Method,
    [Parameter(Mandatory=$true)][string]$Uri,
    [Parameter(Mandatory=$true)][hashtable]$Payload
  )

  $request = $null
  $response = $null
  $watch = [System.Diagnostics.Stopwatch]::StartNew()
  try {
    $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::$Method, $Uri)
    $request.Headers.Authorization = [System.Net.Http.Headers.AuthenticationHeaderValue]::new('Bearer', $ApiKey)
    $json = $Payload | ConvertTo-Json -Depth 12 -Compress
    $request.Content = [System.Net.Http.StringContent]::new($json, [System.Text.Encoding]::UTF8, 'application/json')
    $response = $Client.SendAsync($request).GetAwaiter().GetResult()
    $body = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
    $watch.Stop()
    $parsed = $null
    try { $parsed = $body | ConvertFrom-Json } catch { }
    if (-not $response.IsSuccessStatusCode) {
      throw "HTTP $([int]$response.StatusCode) $($response.ReasonPhrase): $($body.Substring(0, [Math]::Min(800, $body.Length)))"
    }
    return [pscustomobject]@{ Ok = $true; ElapsedMs = [int64]$watch.Elapsed.TotalMilliseconds; Body = $body; JSON = $parsed }
  } catch {
    $watch.Stop()
    return [pscustomobject]@{ Ok = $false; ElapsedMs = [int64]$watch.Elapsed.TotalMilliseconds; Body = $null; JSON = $null; Error = $_.Exception.Message }
  } finally {
    if ($null -ne $response) { $response.Dispose() }
    if ($null -ne $request) { $request.Dispose() }
  }
}

function Invoke-StreamingChat {
  param(
    [Parameter(Mandatory=$true)][System.Net.Http.HttpClient]$Client,
    [Parameter(Mandatory=$true)][string]$ApiKey,
    [Parameter(Mandatory=$true)][string]$Uri,
    [Parameter(Mandatory=$true)][hashtable]$Payload
  )

  $request = $null
  $response = $null
  $reader = $null
  $stream = $null
  $watch = [System.Diagnostics.Stopwatch]::StartNew()
  $firstVisibleMs = $null
  $builder = [System.Text.StringBuilder]::new()
  $chunkCount = 0
  try {
    $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, $Uri)
    $request.Headers.Authorization = [System.Net.Http.Headers.AuthenticationHeaderValue]::new('Bearer', $ApiKey)
    $request.Headers.Accept.Add([System.Net.Http.Headers.MediaTypeWithQualityHeaderValue]::new('text/event-stream'))
    $json = $Payload | ConvertTo-Json -Depth 12 -Compress
    $request.Content = [System.Net.Http.StringContent]::new($json, [System.Text.Encoding]::UTF8, 'application/json')
    $response = $Client.SendAsync($request, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead).GetAwaiter().GetResult()
    if (-not $response.IsSuccessStatusCode) {
      $body = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
      throw "HTTP $([int]$response.StatusCode) $($response.ReasonPhrase): $($body.Substring(0, [Math]::Min(800, $body.Length)))"
    }
    $stream = $response.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
    $reader = [System.IO.StreamReader]::new($stream)
    while ($null -ne ($line = $reader.ReadLine())) {
      if (-not $line.StartsWith('data:')) { continue }
      $data = $line.Substring(5).Trim()
      if ($data -eq '[DONE]' -or [string]::IsNullOrWhiteSpace($data)) { continue }
      try { $event = $data | ConvertFrom-Json } catch { continue }
      $delta = $event.choices[0].delta
      $content = [string]$delta.content
      if ([string]::IsNullOrWhiteSpace($content)) { continue }
      if ($null -eq $firstVisibleMs) { $firstVisibleMs = [int64]$watch.Elapsed.TotalMilliseconds }
      [void]$builder.Append($content)
      $chunkCount++
    }
    $watch.Stop()
    return [pscustomobject]@{ Ok = $true; ElapsedMs = [int64]$watch.Elapsed.TotalMilliseconds; FirstVisibleMs = $firstVisibleMs; Content = $builder.ToString(); ChunkCount = $chunkCount }
  } catch {
    $watch.Stop()
    return [pscustomobject]@{ Ok = $false; ElapsedMs = [int64]$watch.Elapsed.TotalMilliseconds; FirstVisibleMs = $firstVisibleMs; Content = $builder.ToString(); ChunkCount = $chunkCount; Error = $_.Exception.Message }
  } finally {
    if ($null -ne $reader) { $reader.Dispose() }
    if ($null -ne $stream) { $stream.Dispose() }
    if ($null -ne $response) { $response.Dispose() }
    if ($null -ne $request) { $request.Dispose() }
  }
}

function Invoke-ChatProbe {
  param(
    [System.Net.Http.HttpClient]$Client,
    [string]$ApiKey,
    [string]$BaseUrl,
    [string]$Model,
    [string]$Question,
    [string]$Name,
    [string]$SystemPrompt = '你是一个简洁、可靠的中文助手。只输出最终答案，不输出思维过程。',
    [array]$History = @()
  )
  $result = New-Result $Name $Model $BaseUrl
  $messages = [System.Collections.Generic.List[object]]::new()
  $messages.Add(@{ role = 'system'; content = $SystemPrompt })
  foreach ($message in @($History)) { $messages.Add($message) }
  $messages.Add(@{ role = 'user'; content = $Question })
  $payload = @{
    model = $Model
    temperature = 0
    max_tokens = 512
    stream = $true
    enable_thinking = $false
    messages = @($messages)
  }
  $response = Invoke-StreamingChat $Client $ApiKey "$($BaseUrl.TrimEnd('/'))/chat/completions" $payload
  $result.elapsed_ms = $response.ElapsedMs
  if ($response.Ok) {
    $result.details = [ordered]@{ first_visible_ms = $response.FirstVisibleMs; chunk_count = $response.ChunkCount; answer_chars = $response.Content.Length; answer_preview = if ($response.Content.Length -gt 0) { $response.Content.Substring(0, [Math]::Min(300, $response.Content.Length)) } else { '' } }
    if ($response.Content.Length -eq 0) {
      $result.status = 'failed'
      $result.error = '模型没有返回用户可见的 answer 内容；只有 reasoning 或空流不能算能力探针通过。'
    } else {
      $result.status = 'passed'
    }
  } else {
    $result.status = 'failed'
    $result.error = $response.Error
  }
  return [pscustomobject]$result
}

function Invoke-EmbeddingProbe {
  param([System.Net.Http.HttpClient]$Client, [string]$ApiKey, [string]$BaseUrl, [string]$Model, [int]$ExpectedDimension = 2560)
  $result = New-Result 'embedding' $Model $BaseUrl
  $response = Invoke-JsonRequest $Client $ApiKey 'Post' "$($BaseUrl.TrimEnd('/'))/embeddings" @{ model = $Model; input = '这是一个用于线上 Embedding 能力探针的中文句子。' }
  $result.elapsed_ms = $response.ElapsedMs
  if ($response.Ok) {
    $vector = @($response.JSON.data[0].embedding)
    $result.status = if ($vector.Count -eq $expectedDimension) { 'passed' } else { 'failed' }
    $result.details = [ordered]@{ dimension = $vector.Count; expected_dimension = $expectedDimension }
    if ($result.status -eq 'failed') { $result.error = 'Embedding 维度与冻结索引规格不一致。' }
  } else { $result.status = 'failed'; $result.error = $response.Error }
  return [pscustomobject]$result
}

function Invoke-RerankProbe {
  param([System.Net.Http.HttpClient]$Client, [string]$ApiKey, [string]$BaseUrl, [string]$Model)
  $result = New-Result 'rerank' $Model $BaseUrl
  $response = Invoke-JsonRequest $Client $ApiKey 'Post' "$($BaseUrl.TrimEnd('/'))/rerank" @{ model = $Model; query = '如何验证离线部署没有公网依赖？'; documents = @('检查出站连接和批准端点。', '这是无关的天气信息。') }
  $result.elapsed_ms = $response.ElapsedMs
  if ($response.Ok) {
    $items = @($response.JSON.results)
    $result.status = if ($items.Count -ge 2 -and $null -ne $items[0].relevance_score) { 'passed' } else { 'failed' }
    $result.details = [ordered]@{ result_count = $items.Count; top_index = if ($items.Count -gt 0) { $items[0].index } else { $null } }
    if ($result.status -eq 'failed') { $result.error = 'Rerank 响应缺少可排序结果。' }
  } else { $result.status = 'failed'; $result.error = $response.Error }
  return [pscustomobject]$result
}

function Invoke-ASRProbe {
  param([System.Net.Http.HttpClient]$Client, [string]$ApiKey, [string]$BaseUrl, [string]$Model, [string]$Path)
  $result = New-Result 'asr' $Model $BaseUrl
  $request = $null
  $response = $null
  $watch = [System.Diagnostics.Stopwatch]::StartNew()
  try {
    $bytes = [System.IO.File]::ReadAllBytes((Resolve-Path -LiteralPath $Path).Path)
    $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, "$($BaseUrl.TrimEnd('/'))/audio/transcriptions")
    $request.Headers.Authorization = [System.Net.Http.Headers.AuthenticationHeaderValue]::new('Bearer', $ApiKey)
    $multipart = [System.Net.Http.MultipartFormDataContent]::new()
    $fileContent = [System.Net.Http.ByteArrayContent]::new($bytes)
    $fileContent.Headers.ContentType = [System.Net.Http.Headers.MediaTypeHeaderValue]::Parse('audio/wav')
    $multipart.Add($fileContent, 'file', [System.IO.Path]::GetFileName($Path))
    $multipart.Add([System.Net.Http.StringContent]::new($Model), 'model')
    $multipart.Add([System.Net.Http.StringContent]::new('verbose_json'), 'response_format')
    $request.Content = $multipart
    $response = $Client.SendAsync($request).GetAwaiter().GetResult()
    $body = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
    $watch.Stop()
    $result.elapsed_ms = [int64]$watch.Elapsed.TotalMilliseconds
    if (-not $response.IsSuccessStatusCode) { throw "HTTP $([int]$response.StatusCode) $($response.ReasonPhrase): $($body.Substring(0, [Math]::Min(800, $body.Length)))" }
    $json = $body | ConvertFrom-Json
    $text = [string]$json.text
    if ([string]::IsNullOrWhiteSpace($text)) { throw 'ASR 返回空文本。' }
    $result.status = 'passed'
    $result.details = [ordered]@{ transcript_chars = $text.Length; transcript = $text.Substring(0, [Math]::Min(300, $text.Length)); audio_bytes = $bytes.Length }
  } catch {
    $watch.Stop()
    if ($null -eq $result.elapsed_ms) { $result.elapsed_ms = [int64]$watch.Elapsed.TotalMilliseconds }
    $result.status = 'failed'
    $result.error = $_.Exception.Message
  } finally {
    if ($null -ne $response) { $response.Dispose() }
    if ($null -ne $request) { $request.Dispose() }
  }
  return [pscustomobject]$result
}

function Invoke-TTSProbe {
  param([System.Net.Http.HttpClient]$Client, [string]$ApiKey, [string]$BaseUrl, [string]$Model, [string]$Voice, [string]$OutputPath)
  $result = New-Result 'tts' $Model $BaseUrl
  $response = $null
  $request = $null
  $watch = [System.Diagnostics.Stopwatch]::StartNew()
  try {
    $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, "$($BaseUrl.TrimEnd('/'))/audio/speech")
    $request.Headers.Authorization = [System.Net.Http.Headers.AuthenticationHeaderValue]::new('Bearer', $ApiKey)
    $payload = @{ model = $Model; input = '这是一次线上 TTS 能力探针。'; voice = $Voice; response_format = 'mp3' } | ConvertTo-Json -Compress
    $request.Content = [System.Net.Http.StringContent]::new($payload, [System.Text.Encoding]::UTF8, 'application/json')
    $response = $Client.SendAsync($request).GetAwaiter().GetResult()
    $watch.Stop()
    $result.elapsed_ms = [int64]$watch.Elapsed.TotalMilliseconds
    if (-not $response.IsSuccessStatusCode) {
      $errorBody = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
      throw "HTTP $([int]$response.StatusCode) $($response.ReasonPhrase): $($errorBody.Substring(0, [Math]::Min(800, $errorBody.Length)))"
    }
    $audio = $response.Content.ReadAsByteArrayAsync().GetAwaiter().GetResult()
    if ($audio.Length -eq 0) { throw 'TTS 返回空音频。' }
    [System.IO.File]::WriteAllBytes($OutputPath, $audio)
    $result.status = 'passed'
    $result.details = [ordered]@{ voice = $Voice; format = 'mp3'; audio_bytes = $audio.Length; output_file = $OutputPath }
  } catch {
    $watch.Stop()
    if ($null -eq $result.elapsed_ms) { $result.elapsed_ms = [int64]$watch.Elapsed.TotalMilliseconds }
    $result.status = 'failed'
    $result.error = $_.Exception.Message
  } finally {
    if ($null -ne $response) { $response.Dispose() }
    if ($null -ne $request) { $request.Dispose() }
  }
  return [pscustomobject]$result
}

function Add-MarkdownRow {
  param([System.Collections.Generic.List[string]]$Lines, [string]$Name, [string]$Status, [string]$Details)
  $safe = ([string]$Details).Replace('|', '\|').Replace("`r", ' ').Replace("`n", ' ')
  $Lines.Add("| $Name | $Status | $safe |")
}

$startedAt = [DateTimeOffset]::UtcNow
$values = Read-DotEnv (Resolve-Path -LiteralPath $EnvFile).Path
$output = [System.IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force -Path $output | Out-Null

$rolePrefixes = [ordered]@{
  chat = 'ONLINE_LLM_MODEL'
  verifier_1 = 'ONLINE_VERIFIER_MODEL_1'
  verifier_2 = 'ONLINE_VERIFIER_MODEL_2'
  judge = 'ONLINE_EVALUATION_JUDGE_MODEL'
  embedding = 'ONLINE_EMBEDDING_MODEL'
  rerank = 'ONLINE_RERANK_MODEL'
  asr = 'ONLINE_ASR_MODEL'
  tts = 'ONLINE_TTS_MODEL'
}
$roleConfig = [ordered]@{}
foreach ($entry in $rolePrefixes.GetEnumerator()) {
  $roleConfig[$entry.Key] = Get-RoleConfig $values $entry.Key $entry.Value
}
$modelNames = [ordered]@{}
foreach ($entry in $roleConfig.GetEnumerator()) {
  $modelNames[$entry.Key] = $entry.Value.model
}
$baseUrl = [string]$roleConfig.chat.base_url
$voiceName = Get-OptionalValue $values 'ONLINE_TTS_VOICE' 'default'
$embeddingDimension = [int](Get-OptionalValue $values 'ONLINE_EMBEDDING_MODEL_DIMENSION' '2560')
$audioPath = (Resolve-Path -LiteralPath $AudioFile -ErrorAction Stop).Path
$client = [System.Net.Http.HttpClient]::new()
$client.Timeout = [TimeSpan]::FromSeconds($TimeoutSeconds)
$results = [System.Collections.Generic.List[object]]::new()
$report = [ordered]@{
  schema = 'weknora-online-baseline/v1'
  status = 'engineering_online_model_baseline'
  started_at = $startedAt
  endpoint = Get-PublicEndpoint $baseUrl
  models = $modelNames
  model_endpoints = [ordered]@{}
  probes = @()
  verified_baseline = $null
  voice_baseline = $null
  formal_acceptance = [ordered]@{
    status = 'blocked'
    reason = '应用级知识库、50 个独立会话、正式专家标注和线上目标仍需专用 WeKnora 测试环境。'
  }
}
foreach ($entry in $roleConfig.GetEnumerator()) {
  $report.model_endpoints[$entry.Key] = [ordered]@{ model = $entry.Value.model; endpoint = Get-PublicEndpoint $entry.Value.base_url }
}

try {
  if ($DryRun) {
    $report.probes = @($roleConfig.GetEnumerator() | ForEach-Object { [ordered]@{ name = $_.Key; model = $_.Value.model; endpoint = Get-PublicEndpoint $_.Value.base_url; status = 'dry_run' } })
    $report.verified_baseline = [ordered]@{ status = 'dry_run' }
    $report.voice_baseline = [ordered]@{ status = if ($SkipVoice) { 'skipped' } else { 'dry_run' }; audio_file = $audioPath }
  } else {
    $modelsResponse = Invoke-JsonRequest $client $roleConfig.chat.api_key 'Get' "$($baseUrl.TrimEnd('/'))/models" @{}
    $catalog = @()
    if ($modelsResponse.Ok) { $catalog = @($modelsResponse.JSON.data | ForEach-Object { [string]$_.id }) }
    $catalogResult = [ordered]@{ name = 'model_catalog'; model = $null; endpoint = Get-PublicEndpoint $baseUrl; status = if ($modelsResponse.Ok) { 'passed' } else { 'failed' }; elapsed_ms = $modelsResponse.ElapsedMs; details = [ordered]@{ model_count = $catalog.Count; selected = @($modelNames.GetEnumerator() | ForEach-Object { [ordered]@{ role = $_.Key; model = $_.Value; available = ($catalog -contains $_.Value) } }) }; error = if ($modelsResponse.Ok) { $null } else { $modelsResponse.Error } }
    $results.Add([pscustomobject]$catalogResult)

    $probeQuestion = '请用两句话说明：为什么离线部署必须同时验证模型端点位置、镜像摘要和模型权重校验和？'
    foreach ($role in @('chat', 'verifier_1', 'verifier_2', 'judge')) {
      $config = $roleConfig[$role]
      $results.Add((Invoke-ChatProbe $client $config.api_key $config.base_url $config.model $probeQuestion "chat_$role"))
    }
    $results.Add((Invoke-EmbeddingProbe $client $roleConfig.embedding.api_key $roleConfig.embedding.base_url $modelNames.embedding $embeddingDimension))
    $results.Add((Invoke-RerankProbe $client $roleConfig.rerank.api_key $roleConfig.rerank.base_url $modelNames.rerank))

    $main = $results | Where-Object { $_.name -eq 'chat_chat' } | Select-Object -First 1
    $verificationQuestion = '比较线上模型基线与后续 2×48GB 内网量化部署时，哪些指标必须保持同一验收口径？请给出可操作的检查项。'
    $draft = if ($main.status -eq 'passed') { [string]$main.details.answer_preview } else { '' }
    $verifiedResults = @()
    foreach ($role in @('verifier_1', 'verifier_2')) {
      $system = '你是独立验证模型。检查给定回答是否覆盖问题要求，只输出简短结论和缺失项，不输出思维过程。'
      $question = "$verificationQuestion`n`n待检查回答：$draft"
      $config = $roleConfig[$role]
      $verifiedResults += Invoke-ChatProbe $client $config.api_key $config.base_url $config.model $question "verification_$role" $system
    }
    $distinct = @($modelNames.chat, $modelNames.verifier_1, $modelNames.verifier_2 | Select-Object -Unique)
    $report.verified_baseline = [ordered]@{ status = if (@($verifiedResults | Where-Object status -ne 'passed').Count -eq 0) { 'passed' } else { 'failed' }; scope = 'online_model_contract_smoke'; question = $verificationQuestion; distinct_model_count = $distinct.Count; results = $verifiedResults }
    foreach ($item in $verifiedResults) { $results.Add($item) }

    if (-not $SkipVoice) {
      $asr = Invoke-ASRProbe $client $roleConfig.asr.api_key $roleConfig.asr.base_url $modelNames.asr $audioPath
      $results.Add($asr)
      $ttsPath = Join-Path $output 'online-tts-smoke.mp3'
      $tts = Invoke-TTSProbe $client $roleConfig.tts.api_key $roleConfig.tts.base_url $modelNames.tts $voiceName $ttsPath
      $results.Add($tts)
      $transcript = if ($asr.status -eq 'passed') { [string]$asr.details.transcript } else { '请说明线上模型与内网模型验收时需要保持哪些指标一致。' }
      $answer = Invoke-ChatProbe $client $roleConfig.chat.api_key $roleConfig.chat.base_url $modelNames.chat $transcript 'voice_answer'
      $results.Add($answer)
      $history = @(
        @{ role = 'user'; content = $transcript }
        @{ role = 'assistant'; content = if ($answer.status -eq 'passed') { [string]$answer.details.answer_preview } else { '' } }
      )
      $followup = Invoke-ChatProbe $client $roleConfig.chat.api_key $roleConfig.chat.base_url $modelNames.chat '请基于上一轮语音问题和回答，再用一句话总结重点。' 'voice_followup' '你是一个简洁、可靠的中文助手。只输出最终答案，不输出思维过程。' $history
      $results.Add($followup)
      $report.voice_baseline = [ordered]@{ status = if (@($asr, $tts, $answer, $followup | Where-Object status -ne 'passed').Count -eq 0) { 'passed' } else { 'failed' }; scope = 'online_model_endpoint_chain'; asr_model = $modelNames.asr; tts_model = $modelNames.tts; voice = $voiceName; audio_file = $audioPath; asr = $asr; answer = $answer; tts = $tts; followup = $followup }
    } else {
      $report.voice_baseline = [ordered]@{ status = 'skipped'; reason = 'SkipVoice' }
    }
    $report.probes = @($results)
  }
} finally {
  $client.Dispose()
}

$report.completed_at = [DateTimeOffset]::UtcNow
$failed = @($report.probes | Where-Object { $_.status -eq 'failed' }).Count
$report.gate = if ($DryRun) { 'dry_run' } elseif ($failed -eq 0 -and $report.verified_baseline.status -eq 'passed' -and ($SkipVoice -or $report.voice_baseline.status -eq 'passed')) { 'passed' } else { 'failed' }
$payload = $report | ConvertTo-Json -Depth 20
$sha256 = [System.Security.Cryptography.SHA256]::Create()
try {
  $hash = $sha256.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($payload))
} finally {
  $sha256.Dispose()
}
$report.integrity_sha256 = ([System.BitConverter]::ToString($hash)).Replace('-', '').ToLowerInvariant()
$jsonPath = Join-Path $output 'online-model-baseline.json'
$mdPath = Join-Path $output 'online-model-baseline.md'
$report | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $jsonPath -Encoding UTF8
$md = [System.Collections.Generic.List[string]]::new()
$md.Add('# 线上模型工程基线')
$md.Add('')
$md.Add("- 状态: $($report.status)")
$md.Add("- Gate: **$($report.gate)**")
$md.Add("- Endpoint: $($report.endpoint)")
$md.Add("- Formal acceptance: **$($report.formal_acceptance.status)** — $($report.formal_acceptance.reason)")
$md.Add("- Integrity SHA-256: $($report.integrity_sha256)")
$md.Add('')
$md.Add('| 检查项 | 状态 | 详情 |')
$md.Add('| --- | --- | --- |')
foreach ($item in @($report.probes)) { Add-MarkdownRow $md $item.name $item.status (($item.details | ConvertTo-Json -Compress -Depth 8) + $(if ($item.error) { " error=$($item.error)" } else { '' })) }
$md.Add('')
$md.Add('## 说明')
$md.Add('')
$md.Add('本报告只证明线上 OpenAI-compatible 模型端点和模型级语音链路可用；应用级知识库准确率、50 个独立会话、10 并发、专家标注和三种离线 profile 仍需专用验收环境。')
$md -join "`n" | Set-Content -LiteralPath $mdPath -Encoding UTF8
Write-Output "已生成线上模型基线 JSON: $jsonPath"
Write-Output "已生成线上模型基线 Markdown: $mdPath"
Write-Output ($report | ConvertTo-Json -Depth 8)
if ($report.gate -eq 'failed') { exit 2 }
