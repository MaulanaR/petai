<#
.SYNOPSIS
  Fullscreen / do-not-disturb respect (AC-64).
.DESCRIPTION
  Phase 1 (positive control): two normal QA windows alternate in the foreground for -PhaseSec;
           automatic AI calls are counted (must be > 0 for the test to be conclusive).
  Phase 2: two borderless fullscreen QA windows alternate in the foreground for -PhaseSec;
           SHQueryUserNotificationState must report BUSY/D3D/PRESENTATION (otherwise SKIP),
           then: 0 automatic AI calls and the pet hidden or asleep in >= 80% of samples.
  Phase 3: fullscreen closed -> pet visible again within 15 s.
  Uses respectFullscreen=true, maxCallsPerHour=60, watchActivity=true. Config restored afterwards.
#>
[CmdletBinding()]
param(
    [string]$DebugAddr = '127.0.0.1:47611',
    [string]$MockAddr = '127.0.0.1:47700',
    [int]$PhaseSec = 40,
    [int]$SwitchSec = 5,
    [int]$ProcessId = 0,
    [string]$ResultsFile = '',
    [string]$ArtifactsDir = '',
    [string]$PidRegistry = ''
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
Initialize-QaContext -ScriptName 'Test-Fullscreen' -ResultsFile $ResultsFile -ArtifactsDir $ArtifactsDir -PidRegistry $PidRegistry

function Invoke-Phase {
    param($A, $B, [int]$Seconds)
    $since = Get-MockLastSeq -MockAddr $MockAddr
    $samples = @(); $quns = @()
    $sw = [Diagnostics.Stopwatch]::StartNew(); $k = 0
    while ($sw.Elapsed.TotalSeconds -lt $Seconds) {
        $null = Set-QaForeground -Hwnd $(if ($k % 2 -eq 0) { $A.Hwnd } else { $B.Hwnd })
        $k++
        $until = $sw.Elapsed.TotalSeconds + $SwitchSec
        while ($sw.Elapsed.TotalSeconds -lt [Math]::Min($until, $Seconds)) {
            $b = Get-PetBox (Get-PetState -DebugAddr $DebugAddr -TimeoutSec 3)
            if ($null -ne $b) { $samples += [pscustomobject]@{ Visible = $b.Visible; State = $b.State; Animation = $b.Animation } }
            $quns += (Get-QaNotificationState)
            Start-Sleep -Milliseconds 1000
        }
    }
    $reqs = @(Get-MockAiRequests -MockAddr $MockAddr -Since $since | Where-Object { $_.taskTag -eq 'pet_action' })
    return [pscustomobject]@{ Requests = $reqs; Samples = $samples; Quns = $quns }
}

$orig = $null; $wins = @()
try {
    $st = Get-PetState -DebugAddr $DebugAddr
    if ($null -eq $st) { Add-QaResult -Id 'AC-64' -Status FAIL -Message '/debug/state not available'; return }
    $orig = $st.config
    $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ ai = @{ enabled = $true; maxCallsPerHour = 60 }; privacy = @{ watchActivity = $true; screenshots = $false }; general = @{ respectFullscreen = $true }; movement = @{ activity = 1.0 } } -SettleMs 500
    $null = Set-MockScenario -MockAddr $MockAddr -Scenario 'normal'

    # phase 1: control
    $n1 = Start-QaWindow -App QaWindow -Title 'QA fullscreen control A' -X 200 -Y 200 -W 600 -H 360 -Color 'FFF3C4' -Foreground
    $n2 = Start-QaWindow -App QaEditor -Title 'QA fullscreen control B' -X 480 -Y 300 -W 600 -H 360 -Color 'D7F5D7' -Foreground
    $wins += $n1; $wins += $n2
    Write-Host "phase 1 (normal windows) $PhaseSec s ..."
    $p1 = Invoke-Phase -A $n1 -B $n2 -Seconds $PhaseSec
    Stop-QaWindow $n1; Stop-QaWindow $n2

    # phase 2: fullscreen
    $f1 = Start-QaWindow -App QaWindow -Title 'QA fullscreen presentation A' -Fullscreen -Color '202020' -Foreground
    $f2 = Start-QaWindow -App QaEditor -Title 'QA fullscreen game B' -Fullscreen -Color '303050' -Foreground
    $wins += $f1; $wins += $f2
    $null = Set-QaForeground -Hwnd $f1.Hwnd
    $busy = Wait-QaUntil -TimeoutMs 4000 -Condition { @(2, 3, 4) -contains (Get-QaNotificationState) }
    if (-not $busy) {
        Add-QaResult -Id 'AC-64' -Status SKIP -Message ("could not put Windows into a fullscreen/busy notification state (SHQueryUserNotificationState={0}); verify manually M-17" -f (Get-QaNotificationState))
        return
    }
    Start-Sleep -Seconds 3
    Write-Host "phase 2 (fullscreen windows) $PhaseSec s ..."
    $p2 = Invoke-Phase -A $f1 -B $f2 -Seconds $PhaseSec
    Stop-QaWindow $f1; Stop-QaWindow $f2

    # phase 3: back to normal
    $back = Wait-QaUntil -TimeoutMs 15000 -IntervalMs 500 -Condition { (Get-QaProp (Get-PetState -DebugAddr $DebugAddr -TimeoutSec 3) 'pet.visible' $true) -eq $true }

    $quiet = @($p2.Samples | Where-Object { $_.Visible -eq $false -or @('sleep', 'hidden', 'dnd', 'sleeping') -contains "$($_.State)" }).Count
    $quietShare = if ($p2.Samples.Count -gt 0) { $quiet / $p2.Samples.Count } else { 0 }
    $ev = @{ controlCalls = $p1.Requests.Count; controlOccasions = @($p1.Requests | ForEach-Object { $_.occasion }); fullscreenCalls = $p2.Requests.Count
        fullscreenOccasions = @($p2.Requests | ForEach-Object { $_.occasion }); hiddenOrAsleepShare = [Math]::Round($quietShare, 2)
        statesDuringFullscreen = @($p2.Samples | ForEach-Object { '{0}/{1}' -f $_.Visible, $_.State } | Select-Object -Unique); qunsValues = @($p2.Quns | Select-Object -Unique); visibleAgain = $back }
    $f = @()
    if ($p2.Requests.Count -gt 0) { $f += ("{0} AI comment(s) while a fullscreen app was in the foreground" -f $p2.Requests.Count) }
    if ($quietShare -lt 0.8) { $f += ("pet hidden/asleep in only {0:P0} of fullscreen samples" -f $quietShare) }
    if (-not $back) { $f += 'pet did not become visible again within 15 s after fullscreen ended' }
    if ($f.Count -gt 0) { Add-QaResult -Id 'AC-64' -Status FAIL -Message ($f -join '; ') -Evidence $ev }
    elseif ($p1.Requests.Count -eq 0) { Add-QaResult -Id 'AC-64' -Status WARN -Message 'no AI calls during fullscreen and pet hidden/asleep, but the positive control produced no calls either (inconclusive for the AI part)' -Evidence $ev }
    else { Add-QaResult -Id 'AC-64' -Status PASS -Message ("control: {0} automatic call(s); fullscreen: 0 calls, pet hidden/asleep {1:P0}; visible again afterwards" -f $p1.Requests.Count, $quietShare) -Evidence $ev }
} catch {
    Add-QaResult -Id 'AC-64' -Status FAIL -Message ('Test-Fullscreen error: ' + $_.Exception.Message)
} finally {
    foreach ($w in $wins) { Stop-QaWindow $w }
    Stop-QaStartedProcesses
    if ($null -ne $orig) {
        try {
            $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ ai = @{ maxCallsPerHour = (Get-QaProp $orig 'ai.maxCallsPerHour' 12) }; privacy = @{ watchActivity = (Get-QaProp $orig 'privacy.watchActivity' $false) }; general = @{ respectFullscreen = (Get-QaProp $orig 'general.respectFullscreen' $true) }; movement = @{ activity = (Get-QaProp $orig 'movement.activity' 0.5) } } -SettleMs 0
        } catch { }
    }
    Write-QaSummary
}
