@echo off
setlocal

cd /d "%~dp0"
rem Prefer Git Bash via ps1; PATH "bash" often resolves to WSL.
pwsh.exe -NoProfile -File "%~dp0scripts\quick-dev.ps1" %*
set "EXIT_CODE=%ERRORLEVEL%"
if not "%EXIT_CODE%"=="0" (
    echo.
    echo [ERROR] quick-dev failed, exit=%EXIT_CODE%
    pause
)
exit /b %EXIT_CODE%
