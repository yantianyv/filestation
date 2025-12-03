@echo off
setlocal

echo Building Go application...

set OUTPUT_NAME=filestation.exe

go build -o %OUTPUT_NAME% main.go

if %ERRORLEVEL% EQU 0 (
    echo Build complete! Output: %OUTPUT_NAME%
) else (
    echo Build failed!
    exit /b 1
)

endlocal
