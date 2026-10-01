<#
.SYNOPSIS
  Verifies click-through on empty overlay areas and interactivity over the pet.
.DESCRIPTION
  Puts a magenta QA backdrop window (our own process) under the topmost overlay, then:
   1. parks the cursor far from the pet -> overlay must become click-through:
      WindowFromPoint at several far points must NOT return the overlay (AC-22),
      a REAL left click at a far point must reach the backdrop (logged by it) (AC-22),
   2. moves the cursor over the pet -> overlay must become interactive within 1.5 s:
      /debug/state window.interactive=true and/or WS_EX_TRANSPARENT cleared, and
      WindowFromPoint(pet centre) = overlay (AC-23),
   3. moves away again -> click-through restored (AC-22).
  Works for both clickThrough modes ("exstyle" and "region"). The movement mode is set to
  "stay" during the test and restored afterwards; the cursor position is always restored.
#>
[CmdletBinding()]
param(
    [string]$DebugAddr = '127.0.0.1:47611',
    [int]$ProcessId = 0,
    [switch]$NoRealClick,
    [string]$ResultsFile = '',
    [string]$ArtifactsDir = '',
    [string]$PidRegistry = '',
    [string]$MockAddr = ''
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
Initialize-QaContext -ScriptName 'Test-ClickThrough' -ResultsFile $ResultsFile -ArtifactsDir $ArtifactsDir -PidRegistry $PidRegistry

$backdrop = $null
$origMode = $null
try {
    $proc = Get-QaPetProcess -ProcessId $ProcessId
    $ov = if ($null -ne $proc) { @(Find-PetOverlay -ProcessId $proc.Id) } else { @() }
    $state = Get-PetState -DebugAddr $DebugAddr
    if ($ov.Count -eq 0 -or $null -eq $state) {
        Add-QaResult -Id 'AC-22' -Status FAIL -Message 'overlay window or /debug/state not available'
        Add-QaResult -Id 'AC-23' -Status FAIL -Message 'overlay window or /debug/state not available'
        return
    }
    $hwnd = $ov[0].Hwnd
    Set-QaClickAllow -Pids @($proc.Id)
    $mode = Get-QaProp $state 'window.clickThrough' 'exstyle'
    $origMode = Get-QaProp $state 'config.movement.mode' $null
    $null = Save-QaCursor
    $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ movement = @{ mode = 'stay' } } -SettleMs 1500

    $wa = Get-QaOverlayWork -Hwnd $hwnd
    $clickLog = Get-QaArtifactPath 'clickthrough-backdrop-clicks.txt'
    $backdrop = Start-QaBackdrop -Work $wa.Work -ClickLog $clickLog
    Start-Sleep -Milliseconds 500

    $box = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
    $far = @(Get-QaFarPoints -Area $backdrop.Rect -Box $box -MinDist 250 -Backdrop $backdrop)
    if ($far.Count -lt 3) {
        Add-QaResult -Id 'AC-22' -Status SKIP -Message 'could not find enough screen points far from the pet'
        return
    }

    # --- 1. cursor far away -> click-through
    $park = $far[0]
    Move-QaCursor $park.X $park.Y
    Start-Sleep -Milliseconds 800
    $rest = Get-QaWindowReport -Hwnd $hwnd
    $restState = Get-PetState -DebugAddr $DebugAddr
    $hits = @()
    foreach ($p in $far | Select-Object -First 10) {
        $root = Get-QaRootWindowAt $p.X $p.Y
        $hits += [pscustomobject]@{ X = $p.X; Y = $p.Y; Root = $root; IsOverlay = ($root -eq $hwnd); IsBackdrop = ($root -eq $backdrop.Hwnd) }
    }
    $overlayHits = @($hits | Where-Object { $_.IsOverlay })
    $ev = @{ clickThroughMode = $mode; exStyleAtRest = $rest.ExStyle; TRANSPARENT = $rest.TRANSPARENT; interactive = (Get-QaProp $restState 'window.interactive'); samples = $hits; petBox = $box }
    if ($overlayHits.Count -eq 0) {
        Add-QaResult -Id 'AC-22' -Status PASS -Message ("WindowFromPoint at {0} empty-area points never returns the overlay (mode={1}, TRANSPARENT={2})" -f $hits.Count, $mode, $rest.TRANSPARENT) -Evidence $ev
    } else {
        Add-QaResult -Id 'AC-22' -Status FAIL -Message ("{0}/{1} empty-area points hit the overlay -> clicks would NOT reach the windows below" -f $overlayHits.Count, $hits.Count) -Evidence $ev
    }
    if ($mode -eq 'exstyle' -and -not $rest.TRANSPARENT) {
        Add-QaResult -Id 'AC-22' -Status FAIL -Message 'contract: WS_EX_TRANSPARENT must be set while the cursor is not over an interactive region' -Evidence @{ exStyle = $rest.ExStyle }
    }

    # Real click on an empty area must land on the backdrop.
    if (-not $NoRealClick) {
        $target = $far[[Math]::Min(1, $far.Count - 1)]
        $before = @(Read-QaClickLog $clickLog).Count
        Invoke-QaClick -X $target.X -Y $target.Y -HoverMs 500
        $got = Wait-QaUntil -TimeoutMs 2000 -Condition { @(Read-QaClickLog $clickLog).Count -gt $before }
        if ($got) {
            Add-QaResult -Id 'AC-22' -Status PASS -Message ("a real left click at ({0},{1}) on an empty overlay area reached the window underneath" -f $target.X, $target.Y) -Evidence @{ clickLog = (Read-QaClickLog $clickLog) }
        } else {
            Add-QaResult -Id 'AC-22' -Status FAIL -Message ("a real left click at ({0},{1}) did NOT reach the window underneath (swallowed by the overlay)" -f $target.X, $target.Y) -Evidence @{ clickLog = (Read-QaClickLog $clickLog) }
        }
    }

    # --- 2. cursor over the pet -> interactive
    $box = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
    Move-QaCursor $box.CX $box.CY
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $became = Wait-QaUntil -TimeoutMs 1500 -IntervalMs 50 -Condition {
        $s = Get-PetState -DebugAddr $DebugAddr -TimeoutSec 2
        $r = Get-QaWindowReport -Hwnd $hwnd
        ((Get-QaProp $s 'window.interactive' $false) -eq $true) -or ($mode -eq 'exstyle' -and -not $r.TRANSPARENT)
    }
    $latency = $sw.ElapsedMilliseconds
    Start-Sleep -Milliseconds 150
    $hover = Get-QaWindowReport -Hwnd $hwnd
    $hoverState = Get-PetState -DebugAddr $DebugAddr
    $rootAtPet = Get-QaRootWindowAt ([int]$box.CX) ([int]$box.CY)
    $ev23 = @{ latencyMs = $latency; exStyleOnHover = $hover.ExStyle; TRANSPARENT = $hover.TRANSPARENT; interactive = (Get-QaProp $hoverState 'window.interactive'); rootAtPetIsOverlay = ($rootAtPet -eq $hwnd); petBox = $box }
    if ($became -and $rootAtPet -eq $hwnd) {
        Add-QaResult -Id 'AC-23' -Status PASS -Message ("overlay became interactive {0} ms after the cursor reached the pet; WindowFromPoint(pet)=overlay" -f $latency) -Evidence $ev23
    } else {
        Add-QaResult -Id 'AC-23' -Status FAIL -Message ("over the pet: interactive-switch={0} (after {1} ms), WindowFromPoint(pet)=overlay: {2}" -f $became, $latency, ($rootAtPet -eq $hwnd)) -Evidence $ev23
    }

    # --- 3. away again -> click-through restored
    Move-QaCursor $park.X $park.Y
    $restored = Wait-QaUntil -TimeoutMs 1500 -IntervalMs 50 -Condition { (Get-QaRootWindowAt $park.X $park.Y) -ne $hwnd }
    $after = Get-QaWindowReport -Hwnd $hwnd
    if ($restored -and ($mode -ne 'exstyle' -or $after.TRANSPARENT)) {
        Add-QaResult -Id 'AC-22' -Status PASS -Message 'click-through restored after the cursor left the pet' -Evidence @{ exStyle = $after.ExStyle }
    } else {
        Add-QaResult -Id 'AC-22' -Status FAIL -Message 'click-through NOT restored within 1.5 s after the cursor left the pet' -Evidence @{ exStyle = $after.ExStyle }
    }
} catch {
    Add-QaResult -Id 'AC-22' -Status FAIL -Message ('Test-ClickThrough error: ' + $_.Exception.Message)
} finally {
    Restore-QaCursor
    if ($null -ne $origMode) { try { $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ movement = @{ mode = $origMode } } -SettleMs 0 } catch { } }
    Stop-QaWindow $backdrop
    Stop-QaStartedProcesses
    Write-QaSummary
}
