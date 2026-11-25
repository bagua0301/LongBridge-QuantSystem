@echo off
setlocal enabledelayedexpansion

set "LOGDIR=logs"
if exist "%LOGDIR%" (
    rmdir /s /q "%LOGDIR%" 2>nul
    echo clear %LOGDIR%
    echo.
) 

for %%f in (test_data\*.xlsx) do (
    echo ===== Processing: %%f =====
    set "xf=%%f"
    set "xf=!xf:\=/!"
    powershell -Command "$c=(Get-Content config.env -Encoding UTF8) -replace '^TestXlsx=.*','TestXlsx=!xf!'; [System.IO.File]::WriteAllLines((Join-Path $pwd 'config.env'),$c,[System.Text.UTF8Encoding]::new($false))"
    LongBridge-QuantSystem.exe
    if errorlevel 1 (
        echo LongBridge-QuantSystem.exe exited with error for %%f
        pause
    )
    echo ===== Done: %%f =====
    echo.
)

extract_logs.exe
if errorlevel 1 (
    echo extract_logs.exe exited with error for %%f
    pause
)
echo All tests completed.
