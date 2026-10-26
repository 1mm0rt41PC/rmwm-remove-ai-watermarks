@echo off
setlocal
if not exist bin mkdir bin
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
go build -o bin/rmwm_windows_amd64.exe ./cmd/rmwm
if errorlevel 1 exit /b 1
set GOOS=linux
go build -o bin/rmwm_linux_amd64.bin ./cmd/rmwm
if errorlevel 1 exit /b 1
endlocal
