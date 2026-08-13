param(
  [Parameter(Mandatory=$true)][string]$Target,
  [int]$Users = 50,
  [int]$Concurrent = 10,
  [int]$DurationSeconds = 60,
  [int]$TTFTLimitMs = 15000,
  [switch]$ConfirmTestEnvironment,
  [string]$TokenFile,
  [string]$Question = '验收负载问题：请返回可引用的简短答案。',
  [string]$OutputFile
)

Add-Type -AssemblyName System.Net.Http

if (-not $ConfirmTestEnvironment) { throw '请使用 -ConfirmTestEnvironment 明确确认这是专用测试环境。' }
if ($Users -lt 1 -or $Users -gt 50) { throw 'Users 必须在 1 到 50 之间。' }
if ($Concurrent -lt 1 -or $Concurrent -gt 10 -or $Concurrent -gt $Users) { throw 'Concurrent 必须在 1 到 10 之间且不超过 Users。' }
if ($DurationSeconds -lt 1 -or $DurationSeconds -gt 3600) { throw 'DurationSeconds 必须在 1 到 3600 之间。' }
if ($TTFTLimitMs -lt 1 -or $TTFTLimitMs -gt 600000) { throw 'TTFTLimitMs 必须在 1 到 600000 之间。' }

$uri = [Uri]$Target
if ($uri.Scheme -notin @('http','https') -or [string]::IsNullOrWhiteSpace($uri.Host)) { throw 'Target 必须是带主机名的 HTTP(S) URL。' }
if ($uri.Host -match '(?i)(prod|production)') { throw '默认拒绝名称包含 prod/production 的目标。' }

Write-Host "验收负载目标: $Target"
Write-Host "虚拟用户: $Users; 并发用户: $Concurrent; 持续时间: ${DurationSeconds}s"

# 没有凭据时只执行安全边界检查，避免把占位目标误当成真实负载运行。
if ([string]::IsNullOrWhiteSpace($TokenFile)) {
	Write-Host '未提供 TokenFile；仅完成安全边界检查，未发起网络请求。'
	return
}

if (-not (Test-Path -LiteralPath $TokenFile -PathType Leaf)) { throw "TokenFile 不存在: $TokenFile" }
$tokens = @(Get-Content -LiteralPath $TokenFile | ForEach-Object { $_.Trim() } | Where-Object { $_ })
if ($tokens.Count -lt $Users) { throw "TokenFile 至少需要 $Users 个非空 token。" }
$tokens = @($tokens | Select-Object -First $Users)
if ((@($tokens | Select-Object -Unique)).Count -ne $Users) { throw '每个虚拟用户必须使用独立认证 token，单账号多会话不计作多用户。' }

$base = $Target.TrimEnd('/')
$startedAt = [DateTimeOffset]::UtcNow
$work = 1..$Users | ForEach-Object {
  [pscustomobject]@{ Index = $_; Token = $tokens[$_ - 1] }
}

$sessions = $work | ForEach-Object -Parallel {
  $item = $_
  $headers = @{ Authorization = "Bearer $($item.Token)"; 'Content-Type' = 'application/json' }
  try {
    $sessionResponse = Invoke-RestMethod -Method Post -Uri "$using:base/api/v1/sessions" -Headers $headers -Body (@{ title = "acceptance-$($item.Index)" } | ConvertTo-Json) -TimeoutSec $using:DurationSeconds
    $session = if ($sessionResponse.data) { $sessionResponse.data } else { $sessionResponse }
    $sessionId = [string]$session.id
    if ([string]::IsNullOrWhiteSpace($sessionId)) { throw '创建隔离会话未返回 session id。' }
    [pscustomobject]@{ user = $item.Index; token = $item.Token; session_id = $sessionId; error = $null }
  } catch {
    [pscustomobject]@{ user = $item.Index; token = $item.Token; session_id = $null; error = $_.Exception.Message }
  }
} -ThrottleLimit $Concurrent

# Create all isolated sessions before issuing questions so the run genuinely
# holds the configured virtual-user population, while the question phase is
# still capped by the explicit concurrency limit.
$run = $sessions | ForEach-Object -Parallel {
  $item = $_
  $record = [ordered]@{ user = $item.user; accepted_at = $null; first_visible_at = $null; completed_at = $null; total_ms = $null; ttft_ms = $null; timed_out = $false; completed_event = $false; error = $item.error; error_raw = $null; attempts = 0 }
  try {
    if ([string]::IsNullOrWhiteSpace($item.session_id)) { throw $item.error }
    # Stagger burst starts slightly to reduce provider rate-limit collisions.
    Start-Sleep -Milliseconds (50 * (($item.user - 1) % [Math]::Max(1, $using:Concurrent)))
    $body = @{ query = $using:Question; agent_enabled = $false } | ConvertTo-Json
    $maxAttempts = 2
    for ($attempt = 1; $attempt -le $maxAttempts; $attempt++) {
      $record.attempts = $attempt
      $record.error = $null
      $record.error_raw = $null
      $record.first_visible_at = $null
      $record.ttft_ms = $null
      $record.completed_event = $false
      $record.timed_out = $false
      $record.accepted_at = [DateTimeOffset]::UtcNow
      $client = [System.Net.Http.HttpClient]::new()
      $client.Timeout = [TimeSpan]::FromSeconds($using:DurationSeconds)
      $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, "$using:base/api/v1/knowledge-chat/$($item.session_id)")
      $request.Headers.Add('Authorization', "Bearer $($item.token)")
      $request.Headers.Add('Accept', 'text/event-stream')
      $request.Content = [System.Net.Http.StringContent]::new($body, [System.Text.Encoding]::UTF8, 'application/json')
      $cancel = [System.Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds($using:DurationSeconds))
      try {
        $response = $client.SendAsync($request, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead, $cancel.Token).GetAwaiter().GetResult()
        $response.EnsureSuccessStatusCode() | Out-Null
        $stream = $response.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
        $reader = [System.IO.StreamReader]::new($stream)
        try {
          while ($null -ne ($line = $reader.ReadLineAsync($cancel.Token).GetAwaiter().GetResult())) {
            if (-not $line.StartsWith('data:')) { continue }
            $data = $line.Substring(5).Trim()
            if ($data -eq '[DONE]' -or [string]::IsNullOrWhiteSpace($data)) { continue }
            try { $event = $data | ConvertFrom-Json } catch { continue }
            $eventText = [string]$event.content
            $eventType = [string]$event.response_type
            $eventError = $null
            if ($event.PSObject.Properties.Name -contains 'error' -and $null -ne $event.error -and -not [string]::IsNullOrWhiteSpace([string]$event.error)) {
              $eventError = [string]$event.error
            } elseif ($event.data -and ($event.data.PSObject.Properties.Name -contains 'error') -and $null -ne $event.data.error -and -not [string]::IsNullOrWhiteSpace([string]$event.data.error)) {
              $eventError = [string]$event.data.error
            }
            if ($null -eq $record.first_visible_at -and $eventType -eq 'answer' -and -not [string]::IsNullOrWhiteSpace($eventText)) {
              $record.first_visible_at = [DateTimeOffset]::UtcNow
              $record.ttft_ms = [int64]([DateTimeOffset]$record.first_visible_at - [DateTimeOffset]$record.accepted_at).TotalMilliseconds
            }
            if ($eventType -eq 'error' -or $eventError) {
              $record.error_raw = $data
              $record.error = if ($eventError) { $eventError } else { '服务端返回 error 事件。' }
              break
            }
            if ($eventType -eq 'complete') {
              $record.completed_event = $true
              break
            }
          }
          if (-not $record.completed_event -and [string]::IsNullOrWhiteSpace([string]$record.error)) {
            $record.error = '流式响应未返回 complete 事件。'
          }
        } finally {
          $cancel.Dispose()
          $reader.Dispose()
          $stream.Dispose()
          $response.Dispose()
          $request.Dispose()
          $client.Dispose()
        }
      } catch {
        $cancel.Dispose()
        $request.Dispose()
        $client.Dispose()
        $record.error = $_.Exception.Message
        if ($_.Exception.Message -match '(?i)timeout|timed out|operation was canceled|canceled') { $record.timed_out = $true }
      }
      $record.completed_at = [DateTimeOffset]::UtcNow
      $record.total_ms = [int64]([DateTimeOffset]$record.completed_at - [DateTimeOffset]$record.accepted_at).TotalMilliseconds
      if ($record.completed_event -and -not $record.error -and $record.first_visible_at) { break }
      if ($attempt -lt $maxAttempts) { Start-Sleep -Milliseconds (400 * $attempt) }
    }
  } catch {
    $record.completed_at = [DateTimeOffset]::UtcNow
    $record.error = $_.Exception.Message
    if ($_.Exception.Message -match '(?i)timeout|timed out|operation was canceled|canceled') { $record.timed_out = $true }
    if ($record.accepted_at) { $record.total_ms = [int64]([DateTimeOffset]$record.completed_at - [DateTimeOffset]$record.accepted_at).TotalMilliseconds }
  }
  [pscustomobject]$record
} -ThrottleLimit $Concurrent

$payload = [ordered]@{ target = $Target; users = $Users; concurrent = $Concurrent; duration_seconds = $DurationSeconds; started_at = $startedAt; completed_at = [DateTimeOffset]::UtcNow; results = @($run) }
$failedResults = @($run | Where-Object { $_.timed_out -or $_.error -or -not $_.completed_event -or -not $_.first_visible_at -or $_.ttft_ms -gt $TTFTLimitMs -or -not $_.completed_at -or $_.total_ms -gt ($DurationSeconds * 1000) })
$payload.gate = if ($failedResults.Count -eq 0 -and @($run).Count -eq $Users) { 'passed' } else { 'failed' }
$payload.failed_users = @($failedResults | ForEach-Object { $_.user })
$payload.error_count = @($run | Where-Object { $_.error }).Count
$payload.timeout_count = @($run | Where-Object { $_.timed_out }).Count
$payload.missing_first_visible_count = @($run | Where-Object { -not $_.first_visible_at }).Count
$payload.ttft_limit_ms = $TTFTLimitMs
$payload.ttft_over_limit_count = @($run | Where-Object { $_.ttft_ms -gt $TTFTLimitMs }).Count
$json = $payload | ConvertTo-Json -Depth 8
if ($OutputFile) { $json | Set-Content -LiteralPath $OutputFile -Encoding UTF8 }
Write-Output $json
if ($payload.gate -eq 'failed') { exit 2 }
