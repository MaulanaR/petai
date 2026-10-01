<#
.SYNOPSIS
  Starts PetAI in a safe sandbox for the manual checklist: mock AI (no real keys, no internet,
  no cost), fake test keys, a temporary data folder. Your real settings are not touched.
.DESCRIPTION
  Press Enter in this window to stop the app and the mock again (cleanup also runs on Ctrl+C).
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File qa\scripts\Start-ManualSession.ps1 -Scenario xss
#>
[CmdletBinding()]
param(
    [string]$AppExe = '',
    [string]$Scenario = 'normal',
    [string]$MockAddr = '127.0.0.1:47700',
    [string]$DebugAddr = '127.0.0.1:47611',
    [switch]$Fast,
    [switch]$KeepData
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
if (-not $AppExe) { $AppExe = Join-Path $script:QaRepoRoot 'build\bin\petai.exe' }
if (-not (Test-Path $AppExe)) { Write-Host "app not found: $AppExe (run 'wails build')" -ForegroundColor Red; return }
$dir = Join-Path $script:QaArtifactsRoot ('manual-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
Initialize-QaContext -ScriptName 'Start-ManualSession' -ArtifactsDir $dir
$bin = Join-Path (Get-QaTempRoot) ((Split-Path -Leaf $dir) + '-bin')
New-Item -ItemType Directory -Force -Path $bin | Out-Null
$mockExe = Join-Path $bin 'mockai.exe'
$mock = $null; $app = $null
try {
    Set-QaGoEnv
    Push-Location (Join-Path $script:QaRoot 'mockai')
    try { & go build -buildvcs=false -o $mockExe . } finally { Pop-Location }
    $mock = Start-Process -FilePath $mockExe -ArgumentList @('-addr', $MockAddr, '-scenario', $Scenario, '-log', ('"{0}"' -f (Join-Path $dir 'mockai.jsonl'))) -PassThru -WindowStyle Hidden
    Register-QaPid -Process $mock -Role 'mockai'
    if (-not (Wait-QaUntil -TimeoutMs 8000 -Condition { $null -ne (Get-MockHealth -MockAddr $MockAddr) })) { throw "mock did not start on $MockAddr" }
    $data = Join-Path (Get-QaTempRoot) ((Split-Path -Leaf $dir) + '-data')
    $app = Start-PetApp -Exe $AppExe -DataDir $data -DebugAddr $DebugAddr -MockAddr $MockAddr -NoFast:(-not $Fast) -LogPrefix (Join-Path $dir 'petai')
    if (-not (Wait-PetReady -DebugAddr $DebugAddr -Process $app -TimeoutSec 60)) { throw 'app did not become ready (see petai.*.txt in the session folder)' }
    Write-Host ''
    Write-Host "PetAI sandbox running (mock scenario '$Scenario', data: $data)" -ForegroundColor Green
    Write-Host 'Switch the mock answer (examples: normal, xss, long_speech, new_anim, memory, http401, http429, slow, refusal):'
    Write-Host ("  Invoke-RestMethod -Method Post -Uri http://{0}/mock/scenario -ContentType application/json -Body '{{""scenario"":""xss""}}'" -f $MockAddr)
    Write-Host 'Make the pet comment right now:'
    Write-Host ("  Invoke-RestMethod -Method Post -Uri http://{0}/debug/trigger -ContentType application/json -Body '{{""occasion"":""random_chatter""}}'" -f $DebugAddr)
    Write-Host 'Turn on activity watching for M-11 / M-19:'
    Write-Host ("  Invoke-RestMethod -Method Post -Uri http://{0}/debug/config -ContentType application/json -Body '{{""privacy"":{{""watchActivity"":true}}}}'" -f $DebugAddr)
    Write-Host ''
    $null = Read-Host 'Press Enter to stop the sandbox'
} catch {
    Write-Host ('error: ' + $_.Exception.Message) -ForegroundColor Red
} finally {
    if ($null -ne $app) { $null = Stop-PetApp -Process $app }
    Stop-QaStartedProcesses
    if (-not $KeepData) { Remove-QaTempDir -Path $data; Remove-QaTempDir -Path $dir }
    Remove-QaTempDir -Path $bin
    Write-Host 'sandbox stopped'
}
