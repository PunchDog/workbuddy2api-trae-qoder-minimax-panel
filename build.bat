@echo off
rem One-shot build. Outputs:
rem   ..\wb2api.exe              server (scheduled task / headless)       [CGO_ENABLED=0]
rem   ..\dist\WorkBuddy2API.exe  desktop shell (Wails v3)                [CGO_ENABLED=1]
rem Keep linker flags identical for both outputs: -trimpath -s -w -H=windowsgui
rem
rem Usage:
rem   build.bat            desktop exe (default, T05) + server exe
rem   build.bat server     server exe only (legacy/CI path, no CGO needed)
rem Cross-platform chain lives in the root Taskfile.yml.
setlocal
cd /d "%~dp0"
set GOROOT=D:\gotool\go
set GOPROXY=https://goproxy.cn,direct
set PATH=%GOROOT%\bin;%PATH%

if /I "%~1"=="server" goto server

echo [1/4] run tests...
set CGO_ENABLED=1
go test ./internal/... -count=1
if errorlevel 1 goto err

echo [2/4] build Vue dist (go:embed source)...
call npm --prefix frontend run build
if errorlevel 1 goto err

echo [3/4] build desktop ..\dist\WorkBuddy2API.exe (cmd/app, Wails shell)...
set CGO_ENABLED=1
go build -trimpath -tags production -ldflags "-s -w -H=windowsgui" -o ..\dist\WorkBuddy2API.exe .\cmd\app
if errorlevel 1 goto err

echo [4/4] build server ..\wb2api.exe (cmd/server)...
set CGO_ENABLED=0
go build -trimpath -ldflags "-s -w -H=windowsgui" -o ..\wb2api.exe .\cmd\server
if errorlevel 1 goto err

echo.
echo OK. Desktop: ..\dist\WorkBuddy2API.exe
echo     Server : ..\wb2api.exe
exit /b 0

:server
set CGO_ENABLED=0
echo build server ..\wb2api.exe (cmd/server)
go build -trimpath -ldflags "-s -w -H=windowsgui" -o ..\wb2api.exe .\cmd\server
if errorlevel 1 goto err
exit /b 0

:err
echo.
echo BUILD FAILED (no artifact overwritten)
exit /b 1
