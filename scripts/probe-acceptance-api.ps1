$token = (Get-Content 'openspec/changes/add-research-acceptance-benchmarks/evidence/tenant10000-token.txt' -Encoding UTF8 | Select-Object -First 1).Trim()
$h = @{ Authorization = "Bearer $token"; 'Content-Type' = 'application/json' }
foreach ($path in @('/api/v1/acceptance/suites', '/api/v1/acceptance/runs', '/api/v1/models')) {
  try {
    $r = Invoke-RestMethod -Uri "http://127.0.0.1:18080$path" -Headers $h -TimeoutSec 10
    Write-Output ("{0} OK count={1}" -f $path, @($r.data).Count)
  } catch {
    Write-Output ("{0} FAIL {1}" -f $path, $_.Exception.Message)
  }
}
