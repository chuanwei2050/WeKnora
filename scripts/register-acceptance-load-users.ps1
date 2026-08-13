param(
  [string]$Target = 'http://127.0.0.1:18080',
  [int]$Users = 50,
  [string]$Password = 'OpenSpecTest1!',
  [string]$EmailPrefix = 'codex-load-accept',
  [string]$ExistingEmailList = 'scripts/testdata/codex-load-emails.txt',
  [ValidateSet('register-new','reset-existing')][string]$Mode = 'reset-existing',
  [string]$TokenFile = 'openspec/changes/add-research-acceptance-benchmarks/evidence/load-tokens-50.txt',
  [string]$CredentialFile = 'openspec/changes/add-research-acceptance-benchmarks/evidence/load-users-50.json',
  [string]$PostgresContainer = 'a393146c3baa_WeKnora-postgres',
  [string]$Database = 'weknora_codex_e2e',
  [string]$DbUser = 'postgres',
  [switch]$ConfirmTestEnvironment
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Net.Http

if (-not $ConfirmTestEnvironment) { throw '请使用 -ConfirmTestEnvironment 明确确认这是专用测试环境。' }
if ($Users -lt 1 -or $Users -gt 50) { throw 'Users 必须在 1 到 50 之间。' }

$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
function Resolve-Repo([string]$Path) {
  if ([System.IO.Path]::IsPathRooted($Path)) { return $Path }
  return Join-Path $root $Path
}
function Write-JsonFile([string]$Path, $Object) {
  $full = Resolve-Repo $Path
  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $full) | Out-Null
  ($Object | ConvertTo-Json -Depth 8) | Set-Content -LiteralPath $full -Encoding UTF8
  return $full
}

$base = $Target.TrimEnd('/')
$client = [System.Net.Http.HttpClient]::new()
$client.Timeout = [TimeSpan]::FromSeconds(60)

function Invoke-JsonPost([string]$Url, $Body) {
  $json = $Body | ConvertTo-Json -Compress -Depth 6
  $content = [System.Net.Http.StringContent]::new($json, [System.Text.Encoding]::UTF8, 'application/json')
  try {
    $response = $client.PostAsync($Url, $content).GetAwaiter().GetResult()
    $text = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
    if (-not $response.IsSuccessStatusCode) {
      throw "HTTP $([int]$response.StatusCode) $Url : $text"
    }
    if ([string]::IsNullOrWhiteSpace($text)) { return $null }
    return $text | ConvertFrom-Json
  } finally {
    $content.Dispose()
  }
}

function Get-BcryptHash([string]$Plain) {
  $tmp = Join-Path $env:TEMP ('weknora-bcrypt-' + [Guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Force -Path $tmp | Out-Null
  $main = @'
package main
import (
  "fmt"
  "os"
  "golang.org/x/crypto/bcrypt"
)
func main() {
  h, err := bcrypt.GenerateFromPassword([]byte(os.Args[1]), 10)
  if err != nil { panic(err) }
  fmt.Print(string(h))
}
'@
  Set-Content -LiteralPath (Join-Path $tmp 'main.go') -Value $main -Encoding ascii
  Copy-Item (Join-Path $root 'go.mod') (Join-Path $tmp 'go.mod') -Force
  Copy-Item (Join-Path $root 'go.sum') (Join-Path $tmp 'go.sum') -Force
  Push-Location $tmp
  try {
    $hash = & go run . $Plain
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($hash)) { throw '生成 bcrypt 失败。' }
    return [string]$hash
  } finally {
    Pop-Location
    Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
  }
}

$accounts = [System.Collections.Generic.List[object]]::new()

if ($Mode -eq 'reset-existing') {
  $listPath = Resolve-Repo $ExistingEmailList
  if (-not (Test-Path -LiteralPath $listPath -PathType Leaf)) { throw "ExistingEmailList 不存在: $listPath" }
  $emails = @(Get-Content -LiteralPath $listPath | ForEach-Object { $_.Trim() } | Where-Object { $_ } | Select-Object -First $Users)
  if ($emails.Count -lt $Users) { throw "ExistingEmailList 只有 $($emails.Count) 个邮箱，需要 $Users 个。" }
  $hash = Get-BcryptHash $Password
  $escapedHash = $hash.Replace("'", "''")
  foreach ($email in $emails) {
    $escapedEmail = $email.Replace("'", "''")
    $sql = "UPDATE users SET password_hash = '$escapedHash', updated_at = NOW() WHERE email = '$escapedEmail';"
    docker exec $PostgresContainer psql -U $DbUser -d $Database -c $sql | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "重置密码失败: $email" }
    $accounts.Add([ordered]@{ email = $email; password = $Password; mode = 'reset-existing' })
  }
} else {
  for ($i = 1; $i -le $Users; $i++) {
    $email = ("{0}-{1:D2}-{2}@example.invalid" -f $EmailPrefix, $i, ([Guid]::NewGuid().ToString('N').Substring(0, 8)))
    $username = ("loaduser{0:D2}{1}" -f $i, ([Guid]::NewGuid().ToString('N').Substring(0, 6)))
    try {
      Invoke-JsonPost "$base/api/v1/auth/register" @{ username = $username; email = $email; password = $Password } | Out-Null
    } catch {
      if ($_.Exception.Message -notmatch '(?i)already|exist|已') { throw }
    }
    $accounts.Add([ordered]@{ email = $email; password = $Password; username = $username; mode = 'register-new' })
  }
}

$tokens = [System.Collections.Generic.List[string]]::new()
$failures = [System.Collections.Generic.List[object]]::new()
foreach ($account in $accounts) {
  try {
    $login = Invoke-JsonPost "$base/api/v1/auth/login" @{ email = [string]$account.email; password = [string]$account.password }
    $token = [string]$login.token
    if ([string]::IsNullOrWhiteSpace($token) -and $null -ne $login.data) { $token = [string]$login.data.token }
    if ([string]::IsNullOrWhiteSpace($token)) { throw '登录未返回 token。' }
    $tokens.Add($token)
  } catch {
    $failures.Add([ordered]@{ email = [string]$account.email; error = $_.Exception.Message })
  }
}

$client.Dispose()

if ($tokens.Count -ne $Users) {
  throw "仅获得 $($tokens.Count)/$Users 个 token。失败: $(($failures | ConvertTo-Json -Compress))"
}
if ((@($tokens | Select-Object -Unique)).Count -ne $Users) {
  throw '存在重复 token；单账号多会话不计作多用户。'
}

$tokenPath = Resolve-Repo $TokenFile
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $tokenPath) | Out-Null
$tokens | Set-Content -LiteralPath $tokenPath -Encoding UTF8

$credPath = Write-JsonFile $CredentialFile ([ordered]@{
  schema = 'weknora-acceptance-load-users/v1'
  target = $Target
  users = $Users
  mode = $Mode
  password = $Password
  token_file = $TokenFile.Replace('\','/')
  accounts = @($accounts | ForEach-Object { [ordered]@{ email = $_.email; mode = $_.mode } })
  generated_at = [DateTimeOffset]::UtcNow
})

Write-Output "已写入 $Users 个独立 token: $tokenPath"
Write-Output "账号清单: $credPath"
Write-Output (@{ token_count = $tokens.Count; unique_token_count = @($tokens | Select-Object -Unique).Count; failures = @($failures) } | ConvertTo-Json -Depth 5)
