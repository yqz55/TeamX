# run-web.ps1 - start web dev server
Set-Location $PSScriptRoot\..\web

Write-Host "Web :5173" -ForegroundColor Cyan
npm run dev
