param([switch]$Installer)
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)
$wails = 'github.com/wailsapp/wails/v2/cmd/wails@v2.16.0'
$wargs = @('build', '-o', 'petai.exe', '-trimpath')
if ($Installer) { $wargs += '-nsis' }
go run $wails @wargs
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Host "OK -> build\bin\petai.exe"
