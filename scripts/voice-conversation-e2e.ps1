param(
  [Parameter(Mandatory=$true)][string]$Target,
  [Parameter(Mandatory=$true)][string]$TokenFile,
  [Parameter(Mandatory=$true)][string]$AudioFile,
  [Parameter(Mandatory=$true)][string]$ASRModel,
  [Parameter(Mandatory=$true)][string]$TTSModel,
  [Parameter(Mandatory=$true)][string]$Voice,
  [string]$AgentID,
  [string]$FirstQuery = '请回答这段语音问题。',
  [string]$FollowupQuery = '请基于上一轮继续回答。',
  [int]$TimeoutSeconds = 120,
  [switch]$ConfirmTestEnvironment,
  [string]$OutputFile
)

$ErrorActionPreference = 'Stop'
if (-not $ConfirmTestEnvironment) { throw '请使用 -ConfirmTestEnvironment 明确确认这是专用测试环境。' }
if (-not (Test-Path -LiteralPath $TokenFile -PathType Leaf)) { throw "TokenFile 不存在: $TokenFile" }
if (-not (Test-Path -LiteralPath $AudioFile -PathType Leaf)) { throw "AudioFile 不存在: $AudioFile" }
if ($TimeoutSeconds -lt 1 -or $TimeoutSeconds -gt 600) { throw 'TimeoutSeconds 必须在 1 到 600 之间。' }
$uri = [Uri]$Target
if ($uri.Scheme -notin @('http','https') -or [string]::IsNullOrWhiteSpace($uri.Host)) { throw 'Target 必须是带主机名的 HTTP(S) URL。' }
if ($uri.Host -match '(?i)(prod|production)') { throw '默认拒绝名称包含 prod/production 的目标。' }
$audioPath = (Resolve-Path -LiteralPath $AudioFile).Path
$audioInfo = Get-Item -LiteralPath $audioPath
if ($audioInfo.Length -le 0 -or $audioInfo.Length -gt 25MB) { throw 'AudioFile 必须在 1 byte 到 25 MiB 之间。' }
$tokens = @(Get-Content -LiteralPath $TokenFile | ForEach-Object { $_.Trim() } | Where-Object { $_ })
if ($tokens.Count -lt 1) { throw 'TokenFile 必须包含至少一个非空 token。' }
$token = $tokens[0]
$base = $Target.TrimEnd('/')
$startedAt = [DateTimeOffset]::UtcNow

function Get-Value($Object, [string]$Name) {
  if ($null -eq $Object) { return $null }
  $property = $Object.PSObject.Properties[$Name]
  if ($null -eq $property) { return $null }
  return $property.Value
}
function Get-String($Object, [string]$Name) {
  $value = Get-Value $Object $Name
  if ($null -eq $value) { return '' }
  return [string]$value
}
function Headers() { return @{ Authorization = "Bearer $token"; 'Content-Type' = 'application/json' } }
function JsonRequest([string]$Method, [string]$Path, $Body = $null) {
  $args = @{ Method = $Method; Uri = "$base$Path"; Headers = (Headers); TimeoutSec = $TimeoutSeconds }
  if ($null -ne $Body) { $args.Body = ($Body | ConvertTo-Json -Compress) }
  $response = Invoke-RestMethod @args
  if ($null -ne $response.data) { return $response.data }
  return $response
}

function Create-Session() {
  $session = JsonRequest 'POST' '/api/v1/sessions' @{ title = 'voice-e2e' }
  $id = Get-String $session 'id'
  if ([string]::IsNullOrWhiteSpace($id)) { throw '创建会话未返回 session id。' }
  return $id
}

function Invoke-ASR([string]$SessionId) {
  $client = [System.Net.Http.HttpClient]::new()
  $content = [System.Net.Http.MultipartFormDataContent]::new()
  $bytes = [System.IO.File]::ReadAllBytes($audioPath)
  $fileContent = [System.Net.Http.ByteArrayContent]::new($bytes)
  $fileContent.Headers.ContentType = [System.Net.Http.Headers.MediaTypeHeaderValue]::Parse('audio/wav')
  $content.Add([System.Net.Http.StringContent]::new($ASRModel), 'model_id')
  $content.Add($fileContent, 'audio', [System.IO.Path]::GetFileName($audioPath))
  $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, "$base/api/v1/sessions/$SessionId/voice/asr")
  $request.Headers.Add('Authorization', "Bearer $token")
  $request.Content = $content
  try {
    $response = $client.SendAsync($request).GetAwaiter().GetResult()
    $response.EnsureSuccessStatusCode() | Out-Null
    $raw = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult() | ConvertFrom-Json
    if ($null -ne $raw.data) { return $raw.data }
    return $raw
  } finally {
    $request.Dispose(); $content.Dispose(); $client.Dispose()
  }
}

function Invoke-Chat([string]$SessionId, [string]$Question) {
  $accepted = [DateTimeOffset]::UtcNow
  $client = [System.Net.Http.HttpClient]::new()
  $client.Timeout = [TimeSpan]::FromSeconds($TimeoutSeconds)
  $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, "$base/api/v1/knowledge-chat/$SessionId")
  $request.Headers.Add('Authorization', "Bearer $token")
  $request.Headers.Add('Accept', 'text/event-stream')
  $payload = @{ query = $Question; agent_enabled = (-not [string]::IsNullOrWhiteSpace($AgentID)) }
  if (-not [string]::IsNullOrWhiteSpace($AgentID)) { $payload.agent_id = $AgentID }
  $request.Content = [System.Net.Http.StringContent]::new(($payload | ConvertTo-Json -Compress), [System.Text.Encoding]::UTF8, 'application/json')
  $answer = [System.Text.StringBuilder]::new()
  $assistantMessageId = ''
  $firstVisible = $null
  $completed = $false
  $errorMessage = ''
  try {
    $response = $client.SendAsync($request, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead).GetAwaiter().GetResult()
    $response.EnsureSuccessStatusCode() | Out-Null
    $stream = $response.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
    $reader = [System.IO.StreamReader]::new($stream)
    try {
      while ($null -ne ($line = $reader.ReadLine())) {
        if (-not $line.StartsWith('data:')) { continue }
        $raw = $line.Substring(5).Trim()
        if ($raw -eq '[DONE]' -or [string]::IsNullOrWhiteSpace($raw)) { continue }
        try { $event = $raw | ConvertFrom-Json } catch { continue }
        $type = Get-String $event 'response_type'
        if ($type -eq 'agent_query') {
          $assistantMessageId = Get-String $event 'assistant_message_id'
          if ($assistantMessageId -eq '') { $assistantMessageId = Get-String (Get-Value $event 'data') 'assistant_message_id' }
        } elseif ($type -eq 'answer') {
          $content = Get-String $event 'content'
          if (-not [string]::IsNullOrWhiteSpace($content)) {
            if ($null -eq $firstVisible) { $firstVisible = [DateTimeOffset]::UtcNow }
            [void]$answer.Append($content)
          }
        } elseif ($type -eq 'error') {
          $errorMessage = Get-String $event 'content'
          break
        } elseif ($type -eq 'complete') {
          $completed = $true
          break
        }
      }
    } finally {
      $reader.Dispose(); $stream.Dispose(); $response.Dispose()
    }
  } catch { $errorMessage = $_.Exception.Message } finally { $request.Dispose(); $client.Dispose() }
  return [pscustomobject]@{
    answer = $answer.ToString(); assistant_message_id = $assistantMessageId; completed = $completed; error = $errorMessage
    ttft_ms = if ($null -ne $firstVisible) { [int64]($firstVisible - $accepted).TotalMilliseconds } else { 0 }
    first_visible = ($null -ne $firstVisible)
  }
}

function Invoke-TTS([string]$SessionId, [string]$MessageId, [switch]$Interrupt) {
  $client = [System.Net.Http.HttpClient]::new()
  $client.Timeout = [TimeSpan]::FromSeconds($TimeoutSeconds)
  $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, "$base/api/v1/sessions/$SessionId/voice/tts")
  $request.Headers.Add('Authorization', "Bearer $token")
  $request.Content = [System.Net.Http.StringContent]::new((@{ message_id = $MessageId; model_id = $TTSModel; voice = $Voice; format = 'mp3' } | ConvertTo-Json -Compress), [System.Text.Encoding]::UTF8, 'application/json')
  $cancel = [System.Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds($TimeoutSeconds))
  $firstBlock = 0
  $totalBytes = 0
  $cancelled = $false
  try {
    $response = $client.SendAsync($request, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead, $cancel.Token).GetAwaiter().GetResult()
    $response.EnsureSuccessStatusCode() | Out-Null
    $stream = $response.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
    try {
      $buffer = New-Object byte[] 8192
      while (($read = $stream.ReadAsync($buffer, 0, $buffer.Length, $cancel.Token).GetAwaiter().GetResult()) -gt 0) {
        $totalBytes += $read
        if ($firstBlock -eq 0) { $firstBlock = $read }
        if ($Interrupt) { $cancel.Cancel(); $cancelled = $true; break }
      }
    } finally { $stream.Dispose(); $response.Dispose() }
  } catch {
    if ($Interrupt) { $cancelled = $true } else { throw }
  } finally { $cancel.Dispose(); $request.Dispose(); $client.Dispose() }
  return [pscustomobject]@{ first_audio_block_bytes = $firstBlock; audio_bytes = $totalBytes; interrupted = [bool]$Interrupt; cancelled_transport = $cancelled }
}

$sessionId = Create-Session
$agentAutoPlayValue = $null
$agentAutoPlayDefaultOff = $true
if (-not [string]::IsNullOrWhiteSpace($AgentID)) {
  $agentData = JsonRequest 'GET' "/api/v1/agents/$AgentID"
  $agentConfig = Get-Value $agentData 'config'
  $agentAutoPlayValue = Get-Value $agentConfig 'voice_auto_play'
  $agentAutoPlayDefaultOff = ($null -ne $agentAutoPlayValue -and -not [bool]$agentAutoPlayValue)
}
$firstASR = Invoke-ASR $sessionId
$firstChat = Invoke-Chat $sessionId ([string]$firstASR.text)
if ([string]::IsNullOrWhiteSpace($firstChat.assistant_message_id)) { throw '第一轮 SSE 未返回 assistant_message_id，无法执行有权限的 TTS。' }
$firstTTS = Invoke-TTS $sessionId $firstChat.assistant_message_id
$interruptedTTS = Invoke-TTS $sessionId $firstChat.assistant_message_id -Interrupt
$followupASR = Invoke-ASR $sessionId
$followupChat = Invoke-Chat $sessionId ([string]$followupASR.text)
$messages = @(JsonRequest 'GET' "/api/v1/messages/$sessionId/load?limit=100")
$assistantMessages = @($messages | Where-Object { (Get-String $_ 'role') -eq 'assistant' -and [bool](Get-Value $_ 'is_completed') })

$checks = [ordered]@{
  first_asr = (-not [string]::IsNullOrWhiteSpace([string]$firstASR.text))
  first_chat = ([bool]$firstChat.completed -and [string]::IsNullOrWhiteSpace([string]$firstChat.error) -and $firstChat.first_visible)
  first_tts = ([int]$firstTTS.first_audio_block_bytes -gt 0)
  playback_interruption = ([bool]$interruptedTTS.interrupted -and [int]$interruptedTTS.first_audio_block_bytes -gt 0 -and [bool]$interruptedTTS.cancelled_transport)
  followup_same_session = (-not [string]::IsNullOrWhiteSpace([string]$followupASR.text) -and [bool]$followupChat.completed)
  text_answers_persisted = ($assistantMessages.Count -ge 2)
  tts_audio_not_persisted = ($firstTTS.audio_bytes -gt 0 -and $interruptedTTS.audio_bytes -gt 0)
  autoplay_default_off = $agentAutoPlayDefaultOff
}
$passed = (@($checks.GetEnumerator() | Where-Object { -not $_.Value }).Count -eq 0)
$payload = [ordered]@{
  schema_version = 'voice-conversation-e2e/v1'
  status = if ($passed) { 'passed' } else { 'failed' }
  target = $Target
  session_id = $sessionId
  asr_model = $ASRModel
  tts_model = $TTSModel
  voice = $Voice
  agent_id = $AgentID
  first_turn = [ordered]@{ asr_text_chars = ([string]$firstASR.text).Length; chat_ttft_ms = $firstChat.ttft_ms; tts_audio_first_block_bytes = $firstTTS.first_audio_block_bytes }
  interruption = $interruptedTTS
  followup_turn = [ordered]@{ same_session = $true; asr_text_chars = ([string]$followupASR.text).Length; chat_ttft_ms = $followupChat.ttft_ms }
  persistence = [ordered]@{ completed_assistant_messages = $assistantMessages.Count; tts_audio_persisted = $false; text_answer_preserved = ($assistantMessages.Count -ge 2); autoplay_default_off = $agentAutoPlayDefaultOff; configured_voice_auto_play = $agentAutoPlayValue }
  checks = $checks
  started_at = $startedAt
  completed_at = [DateTimeOffset]::UtcNow
}
$json = $payload | ConvertTo-Json -Depth 20
if ($OutputFile) { $json | Set-Content -LiteralPath $OutputFile -Encoding UTF8 }
Write-Output $json
if (-not $passed) { exit 2 }
