<#
.SYNOPSIS
  Black-box probe of the PetAI overlay window (Win32): ex-styles, rect vs monitor,
  taskbar/Alt-Tab presence, visibility, display affinity.
.DESCRIPTION
  Finds the overlay (class wailsWindow, title "PetAI Overlay", owned by petai), parks the
  cursor away from the pet (unless -NoInput) so the "at rest" ex-style can be read, and
  reports/asserts: AC-20 (no taskbar/Alt-Tab), AC-24 (topmost), AC-25 (work area of the monitor,
  frameless), AC-26 (LAYERED/NOACTIVATE/TOOLWINDOW/TOPMOST + TRANSPARENT at rest),
  AC-28 (display affinity when -ExpectExcludeCapture), AC-10 (overlay visible).
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File qa\scripts\Probe-Window.ps1 -DebugAddr 127.0.0.1:47611
#>
[CmdletBinding()]
param(
    [string]$DebugAddr = '127.0.0.1:47611',
    [int]$ProcessId = 0,
    [switch]$ExpectExcludeCapture,
    [switch]$NoInput,
    [string]$ResultsFile = '',
    [string]$ArtifactsDir = '',
    [string]$PidRegistry = '',
    [string]$MockAddr = ''
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
Initialize-QaContext -ScriptName 'Probe-Window' -ResultsFile $ResultsFile -ArtifactsDir $ArtifactsDir -PidRegistry $PidRegistry

$ids = @('AC-20', 'AC-24', 'AC-25', 'AC-26')
try {
    $proc = Get-QaPetProcess -ProcessId $ProcessId
    if ($null -eq $proc) {
        $byName = @(Get-Process -Name petai -ErrorAction SilentlyContinue)
        if ($byName.Count -gt 0) { $proc = $byName[0] }
    }
    if ($null -eq $proc) {
        foreach ($i in $ids) { Add-QaResult -Id $i -Status FAIL -Message 'no running petai process / overlay window found' }
        return
    }
    $overlays = @(Find-PetOverlay -ProcessId $proc.Id)
    $allOverlays = @(Find-PetOverlay)
    if ($overlays.Count -eq 0) {
        $wins = @(Get-QaProcessWindows -ProcessId $proc.Id | Select-Object ClassName, Title, Visible, ExStyle)
        foreach ($i in $ids) {
            Add-QaResult -Id $i -Status FAIL -Message ("petai pid {0} has no window class '{1}' titled '{2}' (contract)" -f $proc.Id, $script:QaOverlayClass, $script:QaOverlayTitle) -Evidence @{ windows = $wins }
        }
        return
    }
    if ($overlays.Count -gt 1 -or $allOverlays.Count -gt 1) {
        Add-QaResult -Id 'AC-25' -Status FAIL -Message ("expected exactly one overlay window, found {0} for pid {1} ({2} system-wide)" -f $overlays.Count, $proc.Id, $allOverlays.Count)
    }
    $hwnd = $overlays[0].Hwnd

    # Park the cursor away from the pet so we read the "at rest" state.
    $state = Get-PetState -DebugAddr $DebugAddr
    $box = Get-PetBox $state
    $mon = $overlays[0].Monitor
    if (-not $NoInput) {
        $null = Save-QaCursor
        $px = $mon.X + 5; $py = $mon.Y + 5
        if ($null -ne $box -and $box.CX -lt ($mon.X + $mon.W / 2) -and $box.CY -lt ($mon.Y + $mon.H / 2)) { $px = $mon.X + $mon.W - 6 }
        Move-QaCursor $px $py
        Start-Sleep -Milliseconds 700
    }
    $rep = Get-QaWindowReport -Hwnd $hwnd
    if (-not $NoInput) { Restore-QaCursor }

    $others = @(Get-QaProcessWindows -ProcessId $proc.Id | Where-Object { $_.Hwnd -ne $hwnd })
    $taskbarOthers = @($others | Where-Object { $_.HasTaskbarButton -and $_.Cloaked -eq 0 -and $_.Rect.W -gt 0 -and $_.Rect.H -gt 0 })

    $report = [ordered]@{
        process = @{ id = $proc.Id; path = $(try { $proc.Path } catch { '' }) }
        overlay = $rep
        otherWindows = @($others | Select-Object Hwnd, ClassName, Title, Visible, Cloaked, ExStyle, HasTaskbarButton, Rect)
        debugWindow = $(if ($null -ne $state) { $state.window } else { $null })
        cursorParked = (-not $NoInput)
    }
    $jsonPath = Write-QaArtifact -Name 'window-probe.json' -Text (ConvertTo-Json -InputObject $report -Depth 8)
    Write-Host ($rep | Format-List | Out-String)

    # AC-20 no taskbar button / Alt-Tab entry
    $ev20 = @{ exStyle = $rep.ExStyle; TOOLWINDOW = $rep.TOOLWINDOW; APPWINDOW = $rep.APPWINDOW; owner = $rep.Owner; otherTaskbarWindows = @($taskbarOthers | Select-Object ClassName, Title, ExStyle); report = $jsonPath }
    if ($rep.TOOLWINDOW -and -not $rep.APPWINDOW -and -not $rep.HasTaskbarButton -and $taskbarOthers.Count -eq 0) {
        Add-QaResult -Id 'AC-20' -Status PASS -Message 'overlay is TOOLWINDOW without APPWINDOW; no other petai window would get a taskbar/Alt-Tab entry' -Evidence $ev20
    } else {
        Add-QaResult -Id 'AC-20' -Status FAIL -Message ("taskbar/Alt-Tab exposure: overlay TOOLWINDOW={0} APPWINDOW={1}; {2} other visible petai window(s) without TOOLWINDOW" -f $rep.TOOLWINDOW, $rep.APPWINDOW, $taskbarOthers.Count) -Evidence $ev20
    }

    # AC-24 always on top
    if ($rep.TOPMOST) { Add-QaResult -Id 'AC-24' -Status PASS -Message 'WS_EX_TOPMOST set' -Evidence @{ exStyle = $rep.ExStyle } }
    else { Add-QaResult -Id 'AC-24' -Status FAIL -Message 'WS_EX_TOPMOST not set' -Evidence @{ exStyle = $rep.ExStyle } }

    # AC-25 overlay = WORK AREA of the selected monitor (1px shorter than the monitor when the taskbar
    # auto-hides), never exactly monitor-sized (Windows would treat it as a fullscreen app); frameless.
    $tol = 2
    $r = $rep.Rect; $m = $rep.Monitor; $wk = $rep.Work
    $exp = [pscustomobject]@{ X = $wk.X; Y = $wk.Y; W = $wk.W; H = $wk.H }
    $autoHide = ($wk.X -eq $m.X -and $wk.Y -eq $m.Y -and $wk.W -eq $m.W -and $wk.H -eq $m.H)
    if ($autoHide) { $exp.H = $m.H - 1 }
    $covers = ([Math]::Abs($r.X - $exp.X) -le $tol) -and ([Math]::Abs($r.Y - $exp.Y) -le $tol) -and ([Math]::Abs($r.W - $exp.W) -le $tol) -and ([Math]::Abs($r.H - $exp.H) -le $tol)
    $monitorSized = ($r.X -eq $m.X -and $r.Y -eq $m.Y -and $r.W -eq $m.W -and $r.H -eq $m.H)
    $cfgMonitor = Get-QaProp $state 'config.general.monitor' 0
    $primaryOk = ($cfgMonitor -ne 0) -or $m.Primary
    $frameless = (-not $rep.HasCaption) -and (-not $rep.HasThickFrame)
    $dbgRect = Get-QaProp $state 'window.rect'
    $dbgOk = ($null -ne $dbgRect) -and ([Math]::Abs([double]$dbgRect.x - $r.X) -le $tol) -and ([Math]::Abs([double]$dbgRect.y - $r.Y) -le $tol) -and ([Math]::Abs([double]$dbgRect.w - $r.W) -le $tol) -and ([Math]::Abs([double]$dbgRect.h - $r.H) -le $tol)
    $ev25 = @{ rect = $r; expected = $exp; work = $wk; monitor = $m; taskbarAutoHide = $autoHide; frameless = $frameless; style = $rep.Style; configMonitor = $cfgMonitor; debugRect = $dbgRect; notification = (Get-QaNotificationState) }
    $f25 = @()
    if (-not $covers) { $f25 += ("rect {0}x{1}@{2},{3} != work area {4}x{5}@{6},{7}" -f $r.W, $r.H, $r.X, $r.Y, $exp.W, $exp.H, $exp.X, $exp.Y) }
    if ($monitorSized) { $f25 += 'overlay is exactly monitor-sized (Windows reports a fullscreen app -> QUNS_BUSY, taskbar hidden)' }
    if (-not $primaryOk) { $f25 += 'not on the primary monitor although general.monitor=0' }
    if (-not $frameless) { $f25 += "has caption/frame (style $($rep.Style))" }
    if (-not $dbgOk) { $f25 += ('/debug/state window.rect {0} does not match the real rect' -f (Format-QaJson $dbgRect)) }
    if ($f25.Count -eq 0) {
        Add-QaResult -Id 'AC-25' -Status PASS -Message ("overlay {0}x{1}@{2},{3} = work area of the primary monitor; frameless; matches /debug/state" -f $r.W, $r.H, $r.X, $r.Y) -Evidence $ev25
    } else {
        Add-QaResult -Id 'AC-25' -Status FAIL -Message ($f25 -join '; ') -Evidence $ev25
    }
    if ((Get-QaNotificationState) -eq 2) {
        Add-QaResult -Id 'AC-25' -Status WARN -Message 'SHQueryUserNotificationState=BUSY while the pet is running (is the overlay treated as a fullscreen app?)'
    }

    # Coordinate diagnostics: CSS viewport x dpr must match the window, page must not be scrolled.
    $vp = Get-QaProp $state 'pet.viewport'
    if ($null -ne $vp) {
        $dpr = [double](Get-QaProp $vp 'dpr' 1)
        $vw = [double](Get-QaProp $vp 'w' 0) * $dpr; $vh = [double](Get-QaProp $vp 'h' 0) * $dpr
        $sy = [double](Get-QaProp $vp 'scrollY' 0)
        $vpOk = ([Math]::Abs($vw - $r.W) -le 3) -and ([Math]::Abs($vh - $r.H) -le 3) -and ($sy -eq 0) -and ([Math]::Abs($dpr - ($rep.Dpi / 96.0)) -lt 0.01)
        if (-not $vpOk) {
            Add-QaResult -Id 'AC-23' -Status WARN -Message ("viewport {0}x{1} css @dpr {2} (= {3}x{4} px), scrollY {5}; window {6}x{7} px @ {8} dpi -> drawing/hit areas may be offset" -f $vp.w, $vp.h, $dpr, [int]$vw, [int]$vh, $sy, $r.W, $r.H, $rep.Dpi) -Evidence @{ viewport = $vp }
        }
    }

    # AC-26 ex-styles at rest
    $mode = Get-QaProp $state 'window.clickThrough' 'unknown'
    $missing = @()
    foreach ($f in 'LAYERED', 'NOACTIVATE', 'TOOLWINDOW', 'TOPMOST') { if (-not $rep.$f) { $missing += $f } }
    if ($mode -ne 'region' -and -not $NoInput -and -not $rep.TRANSPARENT) { $missing += 'TRANSPARENT(at rest, cursor away from pet)' }
    $ev26 = @{ exStyle = $rep.ExStyle; flags = @{ LAYERED = $rep.LAYERED; NOACTIVATE = $rep.NOACTIVATE; TOOLWINDOW = $rep.TOOLWINDOW; TOPMOST = $rep.TOPMOST; TRANSPARENT = $rep.TRANSPARENT }; clickThrough = $mode; layered = $rep.LayeredAttr; debugExStyle = (Get-QaProp $state 'window.exStyle') }
    if ($missing.Count -eq 0) { Add-QaResult -Id 'AC-26' -Status PASS -Message ("ex-style {0} (clickThrough={1})" -f $rep.ExStyle, $mode) -Evidence $ev26 }
    else { Add-QaResult -Id 'AC-26' -Status FAIL -Message ('missing: ' + ($missing -join ', ')) -Evidence $ev26 }

    # Debug API consistency (contract: window.hwnd / exStyle / rect)
    if ($null -ne $state) {
        $dh = [long](Get-QaProp $state 'window.hwnd' 0)
        $consistent = ($dh -eq $hwnd)
        if (-not $consistent) { Add-QaResult -Id 'AC-26' -Status WARN -Message ("/debug/state window.hwnd={0} differs from real overlay hwnd {1}" -f $dh, $hwnd) }
    }

    # AC-10 (part): overlay visible and pet visible
    $petVisible = Get-QaProp $state 'pet.visible' $null
    if ($rep.Visible -and -not $rep.Iconic -and $rep.Cloaked -eq 0 -and $petVisible -ne $false) {
        Add-QaResult -Id 'AC-10' -Status PASS -Message ("overlay window visible (not minimized/cloaked); /debug pet.visible={0}" -f $petVisible) -Evidence @{ visible = $rep.Visible; cloaked = $rep.Cloaked; pet = (Get-QaProp $state 'pet') }
    } else {
        Add-QaResult -Id 'AC-10' -Status FAIL -Message ("overlay visible={0} iconic={1} cloaked={2} pet.visible={3}" -f $rep.Visible, $rep.Iconic, $rep.Cloaked, $petVisible)
    }

    # AC-28 exclude from capture (only meaningful when launched without PETAI_EXCLUDE_CAPTURE=0)
    if ($ExpectExcludeCapture) {
        $cfgEx = Get-QaProp $state 'config.privacy.excludeFromCapture' $null
        if ($rep.DisplayAffinityValue -eq 0x11) {
            Add-QaResult -Id 'AC-28' -Status PASS -Message 'display affinity WDA_EXCLUDEFROMCAPTURE (0x11) by default' -Evidence @{ affinity = $rep.DisplayAffinity; config = $cfgEx }
        } elseif ($rep.DisplayAffinityValue -eq 0x1) {
            Add-QaResult -Id 'AC-28' -Status WARN -Message 'display affinity WDA_MONITOR (0x1): pet appears black in captures instead of excluded' -Evidence @{ affinity = $rep.DisplayAffinity; config = $cfgEx }
        } else {
            Add-QaResult -Id 'AC-28' -Status FAIL -Message ("display affinity {0}; expected WDA_EXCLUDEFROMCAPTURE with default excludeFromCapture={1}" -f $rep.DisplayAffinity, $cfgEx) -Evidence @{ affinity = $rep.DisplayAffinity; config = $cfgEx }
        }
    } else {
        Write-Host ("display affinity: {0} (PETAI_EXCLUDE_CAPTURE=0 expected -> 0x0)" -f $rep.DisplayAffinity)
    }
} catch {
    Add-QaResult -Id 'AC-26' -Status FAIL -Message ("Probe-Window error: " + $_.Exception.Message)
} finally {
    if (-not $NoInput) { Restore-QaCursor }
    Write-QaSummary
}
