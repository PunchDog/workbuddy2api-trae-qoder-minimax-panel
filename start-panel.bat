@echo off
chcp 65001 >nul
title WorkBuddy2API Panel

cd /d "%~dp0"

if not exist "dist\WorkBuddy2API.exe" (
    echo [ERROR] dist\WorkBuddy2API.exe not found. Run build.bat first.
    pause
    exit /b 1
)

echo ============================================
echo   WorkBuddy2API Desktop Panel
echo   Launcher starts a hidden gateway, then opens the WebView window.
echo   Panel is only available in the Wails window (page gate token required).
echo   Closing the window hides it to tray; use tray menu to stop gateway or quit.
echo ============================================
echo.

start "" "dist\WorkBuddy2API.exe"

echo [OK] desktop panel launched. This command window can be closed.
timeout /t 3 >nul
exit /b 0
