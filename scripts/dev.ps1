$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)
go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 dev
