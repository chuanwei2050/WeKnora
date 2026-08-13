# Windows go wrapper that enables CGO (MSYS2 gcc on PATH) then forwards args to go.
# Example:
#   .\scripts\go-windows.ps1 test ./internal/handler/ -run ModelProfile -count=1
#   .\scripts\go-windows.ps1 test ./internal/modelprofile/ ./internal/handler/ -count=1

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
. (Join-Path $scriptDir 'ensure-cgo-windows.ps1')

if ($args.Count -eq 0) {
  Write-Host 'Usage: .\scripts\go-windows.ps1 <go-args...>'
  exit 2
}

& go @args
exit $LASTEXITCODE
