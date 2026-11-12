@echo off
rem qoder-capture.bat — 在 Windows 宿主机收录 Qoder 客户端登录态到共享号池。
rem 面板跑在 Linux 容器时读不了 DPAPI 加密的登录态（DPAPI 仅 Windows 可用），
rem 用本工具在 Windows 侧收录：号池 data/ide_accounts 已挂载进容器，
rem 收录后面板刷新即可签到/续期。前提：对应版本客户端已在本机登录。
rem 用法：qoder-capture.bat [-realm cn|intl]（缺省自动探测活跃客户端）
rem 源码更新后请删除 qodercapture.exe 让本脚本重新编译。
chcp 65001 >nul
setlocal
cd /d "%~dp0"

if not exist qodercapture.exe (
    echo [1/2] 编译 qodercapture.exe ...
    where go >nul 2>nul
    if errorlevel 1 (
        if exist "D:\gotool\go\bin\go.exe" (
            set "PATH=D:\gotool\go\bin;%PATH%"
        ) else (
            echo [ERROR] 未找到 go。请安装 Go 后重试，或手动编译：
            echo         go build -o qodercapture.exe ./cmd/qodercapture
            pause
            exit /b 1
        )
    )
    go build -o qodercapture.exe ./cmd/qodercapture
    if errorlevel 1 goto err
)

qodercapture.exe %*
if errorlevel 1 goto err
echo.
pause
exit /b 0

:err
echo.
echo 收录失败（见上方报错）。
pause
exit /b 1