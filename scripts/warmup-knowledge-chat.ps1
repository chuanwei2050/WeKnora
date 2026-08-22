param(
  [string]$Target = 'http://127.0.0.1:18080',
  [string]$Email = 'codex-e2e-ffe6a43a902245778f7793ac8e06ddcd@example.invalid',
  [string]$Password = 'OpenSpecTest1!',
  [string]$Question = '验证式回答必须先完成哪一步？'
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Net.Http
$base = $Target.TrimEnd('/')
$login = Invoke-RestMethod -Method Post -Uri "$base/api/v1/auth/login" -ContentType 'application/json' -Body (@{ email = $Email; password = $Password } | ConvertTo-Json)
$token = [string]$login.token
$headers = @{ Authorization = "Bearer $token"; 'Content-Type' = 'application/json' }

$kid = '626aed21-d0a5-4765-a2de-67f239835b49'
$doc = @"
# 线上与内网模型验收口径
验证式回答必须先检索，再生成草稿，再由两个独立模型验证事实、逻辑和引用；证据不足时才能补充检索并重新验证。未经验证的草稿不能发送给用户。
必须保持一致的指标：准确率不低于90%；TTFT不超过15秒；50独立会话与10并发全部完成；模型身份可审计。
可操作检查项：对比冻结套件准确率；对比TTFT分布；Embedding维度变化强制重建索引；非same-host时single-node失败；验证失败不得回退公共云。
"@
try {
  Invoke-RestMethod -Method Put -Uri "$base/api/v1/knowledge/manual/$kid" -Headers $headers -Body (@{ title = 'OpenSpec verified acceptance metrics'; content = $doc } | ConvertTo-Json) | Out-Null
  Write-Output 'MANUAL_UPDATE=ok'
} catch { Write-Output ("MANUAL_UPDATE_FAIL=" + $_.Exception.Message) }

foreach ($path in @("/api/v1/knowledge/$kid/process", "/api/v1/knowledge/$kid/enable", "/api/v1/knowledge/$kid/reprocess")) {
  try {
    Invoke-RestMethod -Method Post -Uri ($base + $path) -Headers $headers -Body '{}' | Out-Null
    Write-Output "POST $path => ok"
  } catch { Write-Output ("POST $path => " + $_.Exception.Message) }
}

$session = Invoke-RestMethod -Method Post -Uri "$base/api/v1/sessions" -Headers $headers -Body (@{ title = 'warmup' } | ConvertTo-Json)
$sid = [string]$session.data.id
Write-Output "SESSION=$sid"
$client = [System.Net.Http.HttpClient]::new()
$client.Timeout = [TimeSpan]::FromSeconds(90)
$req = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, "$base/api/v1/knowledge-chat/$sid")
$req.Headers.Add('Authorization', "Bearer $token")
$req.Headers.Add('Accept', 'text/event-stream')
$req.Content = [System.Net.Http.StringContent]::new((@{ query = $Question; agent_enabled = $false } | ConvertTo-Json -Compress), [Text.Encoding]::UTF8, 'application/json')
try {
  $resp = $client.SendAsync($req, [Net.Http.HttpCompletionOption]::ResponseHeadersRead).GetAwaiter().GetResult()
  Write-Output ("STATUS=" + [int]$resp.StatusCode)
  $reader = [IO.StreamReader]::new($resp.Content.ReadAsStreamAsync().GetAwaiter().GetResult())
  $ans = ''
  $n = 0
  while ($null -ne ($line = $reader.ReadLine())) {
    if (-not $line.StartsWith('data:')) { continue }
    $n++
    $p = $line.Substring(5).Trim()
    if ([string]::IsNullOrWhiteSpace($p) -or $p -eq '[DONE]') { continue }
    try { $e = $p | ConvertFrom-Json } catch { continue }
    $c = [string]$e.content
    if (-not $c -and $e.data) { $c = [string]$e.data.content }
    if ($c) { $ans += $c }
  }
  Write-Output ("EVENTS=$n ANS=" + $ans.Substring(0, [Math]::Min(160, $ans.Length)))
} catch {
  Write-Output ("WARM_FAIL=" + $_.Exception.Message)
} finally {
  $client.Dispose()
}
