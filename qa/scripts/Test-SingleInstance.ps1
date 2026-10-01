<#
.SYNOPSIS
  Launching petai a second time must not create a second overlay (AC-90).
.DESCRIPTION
  Starts a second copy of -AppExe with the same QA environment as the running instance,
  waits up to -WaitSec for it to exit, then asserts: still exactly one overlay window
  system-wide, the first instance is alive and its debug API answers. If the second copy is
  still running after the wait it is killed (it is a process THIS script started).
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$AppExe,
    [Parameter(Mandatory = $true)][string]$DataDir,
    [string]$DebugAddr = '127.0.0.1:47611',
    [string]$MockAddr = '127.0.0.1:47700',
    [string]$AnthropicKey = 'sk-test-qa-anthropic-FAKE-7d1e',
    [string]$OpenAIKey = 'sk-test-qa-openai-FAKE-9c2b',
    [int]$ProcessId = 0,
    [int]$WaitSec = 10,
    [string]$ResultsFile = '',
    [string]$ArtifactsDir = '',
    [string]$PidRegistry = ''
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
Initialize-QaContext -ScriptName 'Test-SingleInstance' -ResultsFile $ResultsFile -ArtifactsDir $ArtifactsDir -PidRegistry $PidRegistry

$second = $null
try {
    $first = Get-QaPetProcess -ProcessId $ProcessId
    $before = @(Find-PetOverlay)
    if ($null -eq $first -or $before.Count -eq 0) { Add-QaResult -Id 'AC-90' -Status FAIL -Message 'first instance / overlay not found'; return }
    $second = Start-PetApp -Exe $AppExe -DataDir $DataDir -DebugAddr $DebugAddr -MockAddr $MockAddr -AnthropicKey $AnthropicKey -OpenAIKey $OpenAIKey -ExcludeCaptureOff -LogPrefix (Get-QaArtifactPath 'second-instance')
    $exited = $second.WaitForExit($WaitSec * 1000)
    Start-Sleep -Milliseconds 800
    $after = @(Find-PetOverlay)
    $secondOverlays = @($after | Where-Object { $_.Pid -eq $second.Id })
    $alive = Test-PetAlive -DebugAddr $DebugAddr
    $firstAlive = -not $first.HasExited
    $ev = @{ secondPid = $second.Id; secondExited = $exited; secondExitCode = $(if ($exited) { $second.ExitCode } else { $null }); overlaysBefore = $before.Count; overlaysAfter = $after.Count; firstAlive = $firstAlive; debugAlive = $alive.Alive }
    if ($exited -and $after.Count -eq 1 -and $secondOverlays.Count -eq 0 -and $firstAlive -and $alive.Alive) {
        Add-QaResult -Id 'AC-90' -Status PASS -Message ("second launch exited (code {0}); still exactly one overlay; first instance healthy" -f $second.ExitCode) -Evidence $ev
    } else {
        Add-QaResult -Id 'AC-90' -Status FAIL -Message ("second launch: exited={0}, overlays now={1} (second's={2}), first alive={3}, debug alive={4}" -f $exited, $after.Count, $secondOverlays.Count, $firstAlive, $alive.Alive) -Evidence $ev
    }
} catch {
    Add-QaResult -Id 'AC-90' -Status FAIL -Message ('Test-SingleInstance error: ' + $_.Exception.Message)
} finally {
    if ($null -ne $second -and -not $second.HasExited) { try { $second.Kill(); $null = $second.WaitForExit(5000) } catch { } }
    Write-QaSummary
}
