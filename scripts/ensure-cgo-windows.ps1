# Ensures Windows CGO can compile packages that depend on pg_query_go
# (internal/utils SQL helpers, and anything importing them such as internal/handler).
#
# Root cause: go env CC may point at MSYS2 gcc, but cgo still fails with
# "cgo.exe: exit status 2" when that gcc's bin directory is not on PATH.
#
# Usage (current PowerShell session):
#   . .\scripts\ensure-cgo-windows.ps1
#   go test ./internal/handler/ -run ModelProfile -count=1
#
# Or one-shot:
#   .\scripts\go-windows.ps1 test ./internal/handler/ -run ModelProfile -count=1
#
# Optional: write local Cursor/VS Code terminal env (gitignored):
#   . .\scripts\ensure-cgo-windows.ps1 -WriteVSCodeSettings

[CmdletBinding()]
param(
  [switch]$WriteVSCodeSettings
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Resolve-MsysGccBin {
  $candidates = @()

  try {
    $cc = (& go env CC 2>$null | Out-String).Trim()
    if ($cc -and (Test-Path -LiteralPath $cc)) {
      $candidates += (Split-Path -Parent $cc)
    }
  } catch {}

  $candidates += @(
    'F:\AI\MSYS2\ucrt64\bin',
    'C:\msys64\ucrt64\bin',
    'C:\msys64\mingw64\bin',
    'D:\msys64\ucrt64\bin',
    'D:\msys64\mingw64\bin'
  )

  foreach ($bin in $candidates | Select-Object -Unique) {
    if (-not $bin) { continue }
    $gcc = Join-Path $bin 'gcc.exe'
    if (Test-Path -LiteralPath $gcc) {
      return @{ Bin = $bin; Gcc = $gcc; Gxx = (Join-Path $bin 'g++.exe') }
    }
  }
  return $null
}

$resolved = Resolve-MsysGccBin
if (-not $resolved) {
  Write-Error @"
Could not find MSYS2/MinGW gcc for CGO.

Install MSYS2 and the ucrt64 toolchain, then either:
  1) set go env -w CC=C:\msys64\ucrt64\bin\gcc.exe
  2) or put that bin directory on PATH

Packages such as github.com/pganalyze/pg_query_go require a working C compiler.
"@
}

$bin = $resolved.Bin
$usrBin = Join-Path (Split-Path -Parent (Split-Path -Parent $bin)) 'usr\bin'
$prepend = @($bin)
if (Test-Path -LiteralPath $usrBin) {
  $prepend += $usrBin
}

$parts = $env:PATH -split ';' | Where-Object { $_ -and ($_ -notin $prepend) }
$env:PATH = (($prepend + $parts) -join ';')
$env:CGO_ENABLED = '1'
$env:CC = $resolved.Gcc
if (Test-Path -LiteralPath $resolved.Gxx) {
  $env:CXX = $resolved.Gxx
}

Write-Host "CGO ready: CC=$env:CC"
Write-Host "Prepended PATH: $($prepend -join '; ')"

if ($WriteVSCodeSettings) {
  $scriptRoot = $PSScriptRoot
  if (-not $scriptRoot) {
    $scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
  }
  $repoRoot = Split-Path -Parent $scriptRoot
  $vscodeDir = Join-Path $repoRoot '.vscode'
  New-Item -ItemType Directory -Force -Path $vscodeDir | Out-Null
  $settingsPath = Join-Path $vscodeDir 'settings.json'
  $settings = [ordered]@{
    'terminal.integrated.env.windows' = [ordered]@{
      CGO_ENABLED = '1'
      CC          = $env:CC
      CXX         = $env:CXX
      PATH        = "$($prepend -join ';');`${env:PATH}"
    }
  }
  ($settings | ConvertTo-Json -Depth 5) | Set-Content -LiteralPath $settingsPath -Encoding UTF8
  Write-Host "Wrote local Cursor/VS Code settings: $settingsPath"
  Write-Host "(Typically gitignored via '.*'; regenerate anytime with -WriteVSCodeSettings)"
}
