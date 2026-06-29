# run-gateway.ps1 - start admin gateway
Set-Location $PSScriptRoot\..

Write-Host "Gateway :8080" -ForegroundColor Cyan
.\bin\admin.exe serve --http-port 8080
