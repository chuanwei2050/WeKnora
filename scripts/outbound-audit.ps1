param(
  [Parameter(Mandatory=$true)][string]$ProbeUrl,
  [string]$OutputFile = 'outbound-audit.json',
  [int]$TimeoutSeconds = 10,
  [switch]$NoNetwork
)

$ErrorActionPreference = 'Stop'

function Is-PrivateIp([System.Net.IPAddress]$Ip) {
  if ($Ip.IsLoopback -or $Ip.IsIPv6LinkLocal -or $Ip.IsIPv6SiteLocal) { return $true }
  $bytes = $Ip.GetAddressBytes()
  if ($Ip.AddressFamily -eq [System.Net.Sockets.AddressFamily]::InterNetwork) {
    return (($bytes[0] -eq 10) -or ($bytes[0] -eq 127) -or ($bytes[0] -eq 192 -and $bytes[1] -eq 168) -or ($bytes[0] -eq 172 -and $bytes[1] -ge 16 -and $bytes[1] -le 31))
  }
  return $false
}

$uri = [Uri]$ProbeUrl
if ($uri.Scheme -notin @('http','https') -or [string]::IsNullOrWhiteSpace($uri.Host)) { throw 'ProbeUrl 只支持带 host 的 http/https URL。' }
$resolved = @()
try { $resolved = @([System.Net.Dns]::GetHostAddresses($uri.Host) | ForEach-Object { $_.IPAddressToString }) } catch { }
$publicIps = @($resolved | Where-Object { -not (Is-PrivateIp ([System.Net.IPAddress]::Parse($_))) })
$events = [System.Collections.Generic.List[object]]::new()
$attempted = 0
$successful = 0
$blocked = 0

if ($NoNetwork) {
  $events.Add([ordered]@{ mode='fixture'; url=$uri.GetLeftPart([System.UriPartial]::Path); outcome='not_attempted'; reason='NoNetwork' })
} else {
  $attempted = 1
  try {
    $response = Invoke-WebRequest -Uri $uri.AbsoluteUri -Method Head -TimeoutSec $TimeoutSeconds -MaximumRedirection 0 -UseBasicParsing
    if ($publicIps.Count -gt 0) { $successful++ }
    $events.Add([ordered]@{ mode='probe'; url=$uri.GetLeftPart([System.UriPartial]::Path); outcome='success'; status_code=[int]$response.StatusCode; resolved_ips=$resolved })
  } catch {
    $blocked++
    $events.Add([ordered]@{ mode='probe'; url=$uri.GetLeftPart([System.UriPartial]::Path); outcome='blocked_or_failed'; error=$_.Exception.Message; resolved_ips=$resolved })
  }
}

$report = [ordered]@{
  schema='weknora-outbound-audit/v1'
  status=if ($successful -eq 0) { 'passed' } else { 'failed' }
  probe_url=$uri.GetLeftPart([System.UriPartial]::Path)
  resolved_ips=$resolved
  public_ips=$publicIps
  attempted_public_connections=$attempted
  successful_public_connections=$successful
  blocked_or_failed_connections=$blocked
  events=@($events)
  generated_at=[DateTimeOffset]::UtcNow
}
$outPath = [System.IO.Path]::GetFullPath($OutputFile)
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $outPath) | Out-Null
$report | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $outPath -Encoding UTF8
Write-Output ($report | ConvertTo-Json -Depth 8)
if ($report.status -eq 'failed') { exit 2 }
exit 0
