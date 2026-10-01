<#
.SYNOPSIS
  Spontaneous comments + rate limit: with no debug calls at all, the pet must comment on its own
  (AC-60) but never exceed ai.maxCallsPerHour automatic calls (AC-63).
.DESCRIPTION
  Run early in a session (before many debug calls) with PETAI_FAST=1. Sets watchActivity=true,
  maxCallsPerHour=-MaxCalls, then alternates the foreground between two QA "apps" (QaWindow.exe /
  QaEditor.exe) for -WindowSec seconds to give app_switch/long_focus triggers a chance, and counts
  the AI requests the mock receives. -WindowSec < 60 keeps the check valid whether or not FAST mode
  also shrinks the rolling hour.
#>
[CmdletBinding()]
param(
    [string]$DebugAddr = '127.0.0.1:47611',
    [string]$MockAddr = '127.0.0.1:47700',
    [int]$WindowSec = 55,
    [int]$MaxCalls = 3,
    [int]$SwitchSec = 6,
    [int]$ProcessId = 0,
    [string]$ResultsFile = '',
    [string]$ArtifactsDir = '',
    [string]$PidRegistry = ''
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
Initialize-QaContext -ScriptName 'Test-Autonomy' -ResultsFile $ResultsFile -ArtifactsDir $ArtifactsDir -PidRegistry $PidRegistry

$orig = $null; $w1 = $null; $w2 = $null
try {
    $st = Get-PetState -DebugAddr $DebugAddr
    if ($null -eq $st) { Add-QaResult -Id 'AC-60' -Status FAIL -Message '/debug/state not available'; return }
    $orig = $st.config
    $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ ai = @{ enabled = $true; maxCallsPerHour = $MaxCalls }; privacy = @{ watchActivity = $true; screenshots = $false }; movement = @{ activity = 1.0 } } -SettleMs 500
    $null = Set-MockScenario -MockAddr $MockAddr -Scenario 'normal'
    $w1 = Start-QaWindow -App QaWindow -Title 'QA autonomy - writing report' -X 200 -Y 200 -W 640 -H 380 -Color 'FFF3C4' -Foreground
    $w2 = Start-QaWindow -App QaEditor -Title 'QA autonomy - editor main.go' -X 520 -Y 300 -W 640 -H 380 -Color 'D7F5D7' -Foreground
    $since = Get-MockLastSeq -MockAddr $MockAddr
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $k = 0; $maxReported = 0
    while ($sw.Elapsed.TotalSeconds -lt $WindowSec) {
        $target = if ($k % 2 -eq 0) { $w1 } else { $w2 }
        $null = Set-QaForeground -Hwnd $target.Hwnd
        $k++
        $until = $sw.Elapsed.TotalSeconds + $SwitchSec
        while ($sw.Elapsed.TotalSeconds -lt [Math]::Min($until, $WindowSec)) {
            $s = Get-PetState -DebugAddr $DebugAddr -TimeoutSec 3
            $c = [int](Get-QaProp $s 'ai.callsLastHour' 0)
            if ($c -gt $maxReported) { $maxReported = $c }
            Start-Sleep -Milliseconds 1000
        }
    }
    $reqs = @(Get-MockAiRequests -MockAddr $MockAddr -Since $since)
    $pa = @($reqs | Where-Object { $_.taskTag -eq 'pet_action' })
    $ev = @{ windowSec = $WindowSec; maxCallsPerHour = $MaxCalls; requests = $reqs.Count; petActionRequests = $pa.Count
        occasions = @($pa | ForEach-Object { $_.occasion }); withActivity = @($pa | Where-Object { $_.hasActivity }).Count; maxCallsLastHourReported = $maxReported }
    if ($pa.Count -ge 1) {
        Add-QaResult -Id 'AC-60' -Status PASS -Message ("pet asked the AI on its own {0}x in {1}s without any user/debug action (occasions: {2})" -f $pa.Count, $WindowSec, (($ev.occasions | Select-Object -Unique) -join ',')) -Evidence $ev
    } else {
        Add-QaResult -Id 'AC-60' -Status FAIL -Message ("no spontaneous AI comment in {0}s with PETAI_FAST=1, watchActivity=true and app switching" -f $WindowSec) -Evidence $ev
    }
    if ($pa.Count -le $MaxCalls) {
        Add-QaResult -Id 'AC-63' -Status PASS -Message ("{0} automatic call(s) <= maxCallsPerHour={1}" -f $pa.Count, $MaxCalls) -Evidence $ev
    } else {
        Add-QaResult -Id 'AC-63' -Status FAIL -Message ("{0} automatic calls in {1}s > maxCallsPerHour={2}" -f $pa.Count, $WindowSec, $MaxCalls) -Evidence $ev
    }
    $act = @($pa | Where-Object { $_.hasActivity })
    if ($act.Count -gt 0) { Write-Host ("  activity seen in automatic calls: " + (Format-QaJson $act[0].activity)) }
} catch {
    Add-QaResult -Id 'AC-60' -Status FAIL -Message ('Test-Autonomy error: ' + $_.Exception.Message)
} finally {
    Stop-QaWindow $w1; Stop-QaWindow $w2
    Stop-QaStartedProcesses
    if ($null -ne $orig) {
        try {
            $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ ai = @{ maxCallsPerHour = (Get-QaProp $orig 'ai.maxCallsPerHour' 12) }; privacy = @{ watchActivity = (Get-QaProp $orig 'privacy.watchActivity' $false) }; movement = @{ activity = (Get-QaProp $orig 'movement.activity' 0.5) } } -SettleMs 0
        } catch { }
    }
    Write-QaSummary
}
