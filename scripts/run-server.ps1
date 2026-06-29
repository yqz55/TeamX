# run-server.ps1 - start server
Set-Location $PSScriptRoot\..

Write-Host "Server :50051" -ForegroundColor Cyan
Write-Host "Press Ctrl+C to stop" -ForegroundColor Yellow
.\bin\server.exe --port 50051
