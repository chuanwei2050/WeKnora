@echo off
setlocal

cd /d "%~dp0"
bash ./scripts/quick-dev.sh %*
exit /b %errorlevel%
