param(
  [string]$ContainerName = 'WeKnora-neo4j-e2e',
  [string]$Network = 'weknora-development_WeKnora-network',
  [string]$Image = 'neo4j:5',
  [string]$Username = 'neo4j',
  [string]$Password = 'password',
  [string]$HttpPort = '7474',
  [string]$BoltPort = '7687',
  [int]$ReadyTimeoutSeconds = 240,
  [switch]$Recreate
)

$ErrorActionPreference = 'Stop'

function Write-Info([string]$Message) { Write-Host $Message }

$existing = docker ps -a --filter "name=^/${ContainerName}$" --format '{{.Names}}'
if ($existing -and -not $Recreate) {
  $running = docker ps --filter "name=^/${ContainerName}$" --format '{{.Names}}'
  if (-not $running) {
    Write-Info "Starting existing container $ContainerName"
    docker start $ContainerName | Out-Null
  }
} else {
  if ($existing) {
    Write-Info "Removing $ContainerName for recreate"
    docker rm -f $ContainerName | Out-Null
  }

  $envFile = Join-Path $env:TEMP 'weknora-neo4j-apoc.env'
  # LF-only env file. PowerShell -e NEO4J_PLUGINS=["apoc"] strips quotes and breaks jq.
  $utf8 = New-Object System.Text.UTF8Encoding $false
  $content = @(
    "NEO4J_AUTH=${Username}/${Password}"
    'NEO4J_PLUGINS=["apoc"]'
    'NEO4JLABS_PLUGINS=["apoc"]'
    'NEO4J_apoc_export_file_enabled=true'
    'NEO4J_apoc_import_file_enabled=true'
    'NEO4J_apoc_import_file_use__neo4j__config=true'
    'NEO4J_dbms_security_procedures_unrestricted=apoc.*'
    'NEO4J_dbms_security_procedures_allowlist=apoc.*'
  ) -join "`n"
  [System.IO.File]::WriteAllText($envFile, $content + "`n", $utf8)
  Write-Info "Wrote $envFile"

  docker pull $Image | Out-Null
  docker run -d `
    --name $ContainerName `
    --hostname neo4j `
    --network $Network `
    --network-alias neo4j `
    -p "${HttpPort}:7474" `
    -p "${BoltPort}:7687" `
    --env-file $envFile `
    $Image | Out-Null
  Write-Info "Started $ContainerName from $Image"
}

$deadline = [DateTime]::UtcNow.AddSeconds($ReadyTimeoutSeconds)
$apocVersion = $null
while ([DateTime]::UtcNow -lt $deadline) {
  $plugins = (docker exec $ContainerName ls /var/lib/neo4j/plugins 2>$null | Out-String).Trim()
  $probe = docker exec $ContainerName cypher-shell -u $Username -p $Password "RETURN apoc.version() AS v;" 2>&1
  if ($LASTEXITCODE -eq 0) {
    $apocVersion = (($probe | Out-String) -replace '(?s).*\"([^\"]+)\".*', '$1').Trim()
    if ([string]::IsNullOrWhiteSpace($apocVersion)) { $apocVersion = ($probe | Out-String).Trim() }
    break
  }
  Write-Info "Waiting for APOC... plugins=$plugins"
  Start-Sleep -Seconds 3
}

if ([string]::IsNullOrWhiteSpace($apocVersion)) {
  docker logs $ContainerName 2>&1 | Select-Object -Last 40
  throw "APOC not ready on $ContainerName within ${ReadyTimeoutSeconds}s"
}

$required = @(
  "CALL apoc.help('merge.node') YIELD name RETURN name LIMIT 1;",
  "RETURN size(apoc.coll.union([1],[1,2])) AS n;",
  "CALL apoc.help('periodic.iterate') YIELD name RETURN name LIMIT 1;"
)
foreach ($q in $required) {
  docker exec $ContainerName cypher-shell -u $Username -p $Password $q | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "Required APOC probe failed: $q" }
}

Write-Output "NEO4J_APOC_READY=true container=$ContainerName apoc=$apocVersion"
exit 0
