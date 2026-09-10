@echo off
setlocal

cd /d "%~dp0"
pwsh.exe -NoProfile -File "%~dp0scripts\quick-dev.ps1" %*
exit /b %errorlevel%
