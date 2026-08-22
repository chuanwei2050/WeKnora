param(
  [Parameter(Mandatory=$true)][string]$Target,
  [Parameter(Mandatory=$true)][string]$Model,
  [Parameter(Mandatory=$true)][string]$DatasetFile,
  [Parameter(Mandatory=$true)][string]$TokenFile,
  [int]$TimeoutSeconds = 60,
  [string]$OutputFile,
  [switch]$ConfirmTestEnvironment
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Net.Http
$levels = @('L1', 'L2', 'L3', 'L4')
$actions = [ordered]@{
  L1 = 'quick_rag'
  L2 = 'contextual_rag'
  L3 = 'graph_reasoning'
  L4 = 'verified_agent'
}

if (-not $ConfirmTestEnvironment) { throw '请使用 -ConfirmTestEnvironment 明确确认这是专用线上测试环境。' }
if ([string]::IsNullOrWhiteSpace($Model)) { throw 'Model 不能为空。' }
if ($TimeoutSeconds -lt 1 -or $TimeoutSeconds -gt 600) { throw 'TimeoutSeconds 必须在 1 到 600 之间。' }
if (-not (Test-Path -LiteralPath $DatasetFile -PathType Leaf)) { throw "DatasetFile 不存在: $DatasetFile" }
if (-not (Test-Path -LiteralPath $TokenFile -PathType Leaf)) { throw "TokenFile 不存在: $TokenFile" }

$uri = [Uri]$Target
if ($uri.Scheme -notin @('http', 'https') -or [string]::IsNullOrWhiteSpace($uri.Host)) {
  throw 'Target 必须是带主机名的 HTTP(S) URL。'
}
if (-not [string]::IsNullOrWhiteSpace($uri.UserInfo)) { throw 'Target 不得包含用户名或密码。' }
$targetAddress = $null
$isLiteralAddress = [System.Net.IPAddress]::TryParse($uri.Host, [ref]$targetAddress)
if ($uri.Host -match '(?i)^(localhost|host\.docker\.internal)$' -or ($isLiteralAddress -and (
    [System.Net.IPAddress]::IsLoopback($targetAddress) -or
    $targetAddress.Equals([System.Net.IPAddress]::Any) -or
    $targetAddress.Equals([System.Net.IPAddress]::IPv6Any)))) {
  throw 'Target 不得指向本机或 Docker host 别名；本脚本只接受线上模型端点。'
}
if ($isLiteralAddress -and $targetAddress.AddressFamily -eq [System.Net.Sockets.AddressFamily]::InterNetwork) {
  $octets = $targetAddress.GetAddressBytes()
  $privateIPv4 = ($octets[0] -eq 10) -or
    ($octets[0] -eq 172 -and $octets[1] -ge 16 -and $octets[1] -le 31) -or
    ($octets[0] -eq 192 -and $octets[1] -eq 168) -or
    ($octets[0] -eq 169 -and $octets[1] -eq 254)
  if ($privateIPv4) { throw 'Target 不得指向 RFC1918、链路本地或其他私网地址；本脚本只接受线上模型端点。' }
}
if ($uri.Host -match '(?i)(prod|production)') { throw '默认拒绝名称包含 prod/production 的目标。' }

$token = @(Get-Content -LiteralPath $TokenFile | ForEach-Object { $_.Trim() } | Where-Object { $_ }) | Select-Object -First 1
if ([string]::IsNullOrWhiteSpace($token)) { throw 'TokenFile 必须包含一个非空 token。' }
if ($token -match '^(?i)bearer\s+') { $token = $token -replace '^(?i)bearer\s+', '' }

try {
  $datasetRoot = Get-Content -LiteralPath $DatasetFile -Raw -Encoding UTF8 | ConvertFrom-Json
} catch {
  throw "DatasetFile 不是有效 JSON: $($_.Exception.Message)"
}
$datasetStatus = [string]$datasetRoot.dataset_status
$datasetSource = [string]$datasetRoot.source
if ($datasetStatus -match '(?i)synthetic|fixture|engineering' -or $datasetSource -match '(?i)synthetic|fixture') {
  throw 'Dataset 仍是 synthetic/fixture 工程数据，不能生成专家标注正式路由验收报告。'
}
$cases = if ($datasetRoot -is [System.Array]) { @($datasetRoot) } else { @($datasetRoot.cases) }
if ($cases.Count -lt 4) { throw 'Dataset 至少需要 4 个案例。' }
$seenIDs = @{}
$presentLevels = @{}
foreach ($case in $cases) {
  $caseID = [string]$case.id
  $question = [string]$case.question
  $expertLevel = [string]$case.expert_level
  if ([string]::IsNullOrWhiteSpace($caseID) -or [string]::IsNullOrWhiteSpace($question)) {
    throw '每个案例必须包含非空 id 和 question。'
  }
  if ($seenIDs.ContainsKey($caseID)) { throw "案例 id 重复: $caseID" }
  $seenIDs[$caseID] = $true
  if ($expertLevel -notin $levels) { throw "案例 $caseID 的 expert_level 必须是 L1-L4。" }
  $presentLevels[$expertLevel] = $true
}
foreach ($level in $levels) {
  if (-not $presentLevels.ContainsKey($level)) { throw "Dataset 缺少 $level 专家标签。" }
}

$endpoint = $Target.TrimEnd('/')
if ($endpoint -notmatch '(?i)/v1$') { $endpoint += '/v1' }
$endpoint += '/chat/completions'
$systemPrompt = @'
You are a question-complexity classifier. Classify the user question into exactly one of L1, L2, L3, or L4.
Use the primary operation required to answer, not the presence of words such as "why" or "how" alone.
L1: one explicit fact or direct lookup; no missing context and no reasoning.
L2: a contextual or implicit fact lookup that needs surrounding document context, an entity link, or a definition, but does not require explaining causes, comparing alternatives, or chaining multiple facts.
L3: explicit explanation, comparison, trade-off, multi-hop synthesis, or a conclusion that must be supported by multiple facts. A simple "why" that only asks for a stated reason is L2; choose L3 when the answer must derive or compare.
L4: hypothetical, counterfactual, causal-impact, transfer, or policy/action reasoning that asks what would happen, what should change, or how a conclusion transfers to a new situation.
Examples: "What is the refund period?" -> L1; "Which section defines the refund exception?" -> L2; "Compare the two refund policies and explain the difference." -> L3; "If the policy changes, what downstream actions should we take?" -> L4.
Return exactly one JSON object and no markdown or prose with these fields: complexity_level, reasoning_subtype, confidence, rationale_summary.
The rationale_summary must be one short sentence and must not contain chain-of-thought.
'@
$startedAt = [DateTimeOffset]::UtcNow
$httpClient = [System.Net.Http.HttpClient]::new()
$httpClient.Timeout = [TimeSpan]::FromSeconds($TimeoutSeconds)
$results = [System.Collections.Generic.List[object]]::new()

try {
  foreach ($case in $cases) {
    $acceptedAt = [DateTimeOffset]::UtcNow
    $record = [ordered]@{
      id = [string]$case.id
      expert_level = [string]$case.expert_level
      predicted_level = $null
      predicted_subtype = $null
      confidence = $null
      predicted_action = $null
      expected_action = $actions[[string]$case.expert_level]
      classification_ms = $null
      valid = $false
      error = $null
    }
    try {
      $body = [ordered]@{
        model = $Model
        temperature = 0
        max_tokens = 256
        enable_thinking = $false
        messages = @(
          [ordered]@{ role = 'system'; content = $systemPrompt }
          [ordered]@{ role = 'user'; content = "Classify this question:`n$([string]$case.question)" }
        )
      } | ConvertTo-Json -Depth 8
      $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, $endpoint)
      $request.Headers.Authorization = [System.Net.Http.Headers.AuthenticationHeaderValue]::new('Bearer', $token)
      $request.Headers.Accept.Add([System.Net.Http.Headers.MediaTypeWithQualityHeaderValue]::new('application/json'))
      $request.Content = [System.Net.Http.StringContent]::new($body, [System.Text.Encoding]::UTF8, 'application/json')
      $response = $httpClient.SendAsync($request).GetAwaiter().GetResult()
      $responseBody = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
      $response.EnsureSuccessStatusCode() | Out-Null
      $responseJSON = $responseBody | ConvertFrom-Json
      $content = [string]$responseJSON.choices[0].message.content
      if ([string]::IsNullOrWhiteSpace($content)) { throw '模型返回空内容。' }
      $classification = $content | ConvertFrom-Json
      $predictedLevel = [string]$classification.complexity_level
      $predictedSubtype = [string]$classification.reasoning_subtype
      $confidence = [double]$classification.confidence
      if ($predictedLevel -notin $levels) { throw "模型返回未知复杂度级别: $predictedLevel" }
      if ($confidence -lt 0 -or $confidence -gt 1) { throw "模型返回非法 confidence: $confidence" }
      if ([string]::IsNullOrWhiteSpace($predictedSubtype)) { throw '模型未返回 reasoning_subtype。' }
      $record.predicted_level = $predictedLevel
      $record.predicted_subtype = $predictedSubtype
      $record.confidence = $confidence
      $record.predicted_action = $actions[$predictedLevel]
      $record.classification_ms = [int64]([DateTimeOffset]::UtcNow - $acceptedAt).TotalMilliseconds
      $record.valid = $true
    } catch {
      $record.error = $_.Exception.Message
      $record.classification_ms = [int64]([DateTimeOffset]::UtcNow - $acceptedAt).TotalMilliseconds
    } finally {
      if ($null -ne $request) { $request.Dispose() }
      if ($null -ne $response) { $response.Dispose() }
    }
    $results.Add([pscustomobject]$record)
  }
} finally {
  $httpClient.Dispose()
}

$matrix = [ordered]@{}
foreach ($expertLevel in $levels) {
  $row = [ordered]@{}
  foreach ($predictedLevel in $levels) { $row[$predictedLevel] = 0 }
  $matrix[$expertLevel] = $row
}
$correct = 0
foreach ($result in $results) {
  if ($result.valid) {
    $matrix[$result.expert_level][$result.predicted_level]++
    if ($result.expert_level -eq $result.predicted_level) { $correct++ }
  }
}

$perLevel = [ordered]@{}
foreach ($level in $levels) {
  $rowTotal = 0
  $columnTotal = 0
  foreach ($item in $levels) {
    $rowTotal += [int]$matrix[$level][$item]
    $columnTotal += [int]$matrix[$item][$level]
  }
  $truePositive = [int]$matrix[$level][$level]
  $perLevel[$level] = [ordered]@{
    support = [int]$cases.Where({ $_.expert_level -eq $level }).Count
    precision = if ($columnTotal -gt 0) { $truePositive / $columnTotal } else { 0 }
    recall = if ($rowTotal -gt 0) { $truePositive / $rowTotal } else { 0 }
  }
}

$routeMatrix = [ordered]@{}
foreach ($expectedAction in $actions.Values | Select-Object -Unique) {
  $routeMatrix[$expectedAction] = [ordered]@{}
  foreach ($predictedAction in $actions.Values | Select-Object -Unique) { $routeMatrix[$expectedAction][$predictedAction] = 0 }
}
$routeCorrect = 0
foreach ($result in $results) {
  if ($result.valid) {
    $routeMatrix[$result.expected_action][$result.predicted_action]++
    if ($result.expected_action -eq $result.predicted_action) { $routeCorrect++ }
  }
}

$accuracy = if ($cases.Count -gt 0) { $correct / $cases.Count } else { 0 }
$routeAccuracy = if ($cases.Count -gt 0) { $routeCorrect / $cases.Count } else { 0 }
$invalidCount = @($results | Where-Object { -not $_.valid }).Count
$gate = if ($invalidCount -eq 0 -and $accuracy -ge 0.9) { 'passed' } else { 'failed' }
$report = [ordered]@{
  schema_version = 'question-complexity-routing-benchmark/v1'
  profile = 'online'
  taxonomy = [ordered]@{ id = 'question-complexity'; version = '1.0' }
  model = [ordered]@{ protocol = 'openai-compatible'; endpoint = $Target.TrimEnd('/'); name = $Model }
  started_at = $startedAt
  completed_at = [DateTimeOffset]::UtcNow
  sample_count = $cases.Count
  valid_count = @($results | Where-Object { $_.valid }).Count
  invalid_count = $invalidCount
  accuracy = $accuracy
  confusion_matrix = $matrix
  per_level = $perLevel
  routing_baseline = [ordered]@{
    action_by_level = $actions
    action_accuracy = $routeAccuracy
    action_confusion_matrix = $routeMatrix
  }
  results = @($results)
  gate = $gate
}
$reportPayload = $report | ConvertTo-Json -Depth 12
$hashBytes = [System.Security.Cryptography.SHA256]::HashData([System.Text.Encoding]::UTF8.GetBytes($reportPayload))
$report.integrity_sha256 = ([System.BitConverter]::ToString($hashBytes)).Replace('-', '').ToLowerInvariant()
$json = $report | ConvertTo-Json -Depth 12

if ($OutputFile) {
  $jsonPath = [System.IO.Path]::GetFullPath($OutputFile)
  $mdPath = [System.IO.Path]::ChangeExtension($jsonPath, '.md')
  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $jsonPath) | Out-Null
  $json | Set-Content -LiteralPath $jsonPath -Encoding UTF8
  $markdown = @(
    '# Question complexity routing benchmark'
    ''
    "- Profile: $($report.profile)"
    "- Model: $Model"
    "- Samples: $($report.sample_count)"
    "- Accuracy: $('{0:P2}' -f $accuracy)"
    "- Routing baseline accuracy: $('{0:P2}' -f $routeAccuracy)"
    "- Invalid outputs: $invalidCount"
    "- Gate: **$gate**"
    "- Integrity SHA-256: $($report.integrity_sha256)"
    ''
    '## Confusion matrix'
    ''
    '| Expert \\ Predicted | L1 | L2 | L3 | L4 |'
    '| --- | ---: | ---: | ---: | ---: |'
  )
  foreach ($level in $levels) {
    $markdown += "| $level | $($matrix[$level].L1) | $($matrix[$level].L2) | $($matrix[$level].L3) | $($matrix[$level].L4) |"
  }
  $markdown -join "`n" | Set-Content -LiteralPath $mdPath -Encoding UTF8
  Write-Host "已生成复杂度路由基准报告: $jsonPath"
  Write-Host "已生成复杂度路由基准 Markdown: $mdPath"
}
Write-Output $json
if ($gate -eq 'failed') { exit 2 }
