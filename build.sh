#!/bin/sh
set -eu
mkdir -p bin
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/rmwm_linux_amd64.bin ./cmd/rmwm
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o bin/rmwm_windows_amd64.exe ./cmd/rmwm
