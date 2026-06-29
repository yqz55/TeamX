# build.ps1 - build all binaries
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot\..
$env:GOPROXY = "https://goproxy.cn,direct"

Write-Host "=== Build ===" -ForegroundColor Cyan

Write-Host "[1/4] server..." -NoNewline
go build -o bin\server.exe .\cmd\server\
Write-Host " OK" -ForegroundColor Green

Write-Host "[2/4] admin..." -NoNewline
go build -o bin\admin.exe .\cmd\admin\
Write-Host " OK" -ForegroundColor Green

Write-Host "[3/4] client (win)..." -NoNewline
go build -o bin\client.exe .\cmd\client\
Write-Host " OK" -ForegroundColor Green

Write-Host "[4/4] client (linux)..." -NoNewline
$env:GOOS = "linux"; $env:GOARCH = "amd64"
go build -o bin\client-linux .\cmd\client\
$env:GOOS = ""; $env:GOARCH = ""
Write-Host " OK" -ForegroundColor Green

Write-Host "Build done" -ForegroundColor Green
