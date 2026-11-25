@echo off
echo ========================================
echo 行情数据提取工具
echo ========================================
echo.

set /p html_file="请输入HTML日志文件路径: "

if "%html_file%"=="" (
    echo 请指定文件路径
    pause
    exit /b
)

extract_quotes.exe "%html_file%"

echo.
echo 提取完成！
pause