<#
.SYNOPSIS
  Samples the pet position (/debug/state) for ~20 s in each movement mode, without any user input.
.DESCRIPTION
  stay   -> position constant (AC-32)
  free   -> moves on its own, uses vertical space / not glued to the floor (AC-33, AC-30)
  ground -> moves on its own along the work-area bottom (taskbar top), unless perched (AC-34, AC-30)
  all    -> bbox always inside the monitor (AC-35)
  -PerchSeconds N additionally opens a QA window in the middle of the screen and watches ground
  mode for a perch on its top edge (AC-37, best effort: not observed = SKIP).
  The cursor is parked in a screen corner (unless -NoInput). Config is restored afterwards.
  CSV of every sample is written to the artifacts dir.
#>
[CmdletBinding()]
param(
    [string]$DebugAddr = '127.0.0.1:47611',
    [int]$ProcessId = 0,
    [string[]]$Modes = @('stay', 'free', 'ground'),
    [int]$SecondsPerMode = 20,
    [int]$SampleMs = 400,
    [int]$PerchSeconds = 0,
    [switch]$NoInput,
    [string]$ResultsFile = '',
    [string]$ArtifactsDir = '',
    [string]$PidRegistry = '',
    [string]$MockAddr = ''
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
Initialize-QaContext -ScriptName 'Test-Wander' -ResultsFile $ResultsFile -ArtifactsDir $ArtifactsDir -PidRegistry $PidRegistry

function Get-Samples {
    param([int]$Seconds, [string]$Label)
    $rows = @()
    $sw = [Diagnostics.Stopwatch]::StartNew()
    while ($sw.Elapsed.TotalSeconds -lt $Seconds) {
        $s = Get-PetState -DebugAddr $DebugAddr -TimeoutSec 3
        $b = Get-PetBox $s
        $fg = Get-QaForeground
        if ($null -ne $b) {
            $rows += [pscustomobject]@{ t = [int]$sw.ElapsedMilliseconds; x = [int]$b.X; y = [int]$b.Y; w = [int]$b.W; h = [int]$b.H; cx = [int]$b.CX; cy = [int]$b.CY
                bottom = [int]$b.Bottom; state = $b.State; animation = $b.Animation; mode = $b.Mode; visible = $b.Visible
                fgTop = $(if ($null -ne $fg) { $fg.Y } else { $null }); fgLeft = $(if ($null -ne $fg) { $fg.X } else { $null }); fgRight = $(if ($null -ne $fg) { $fg.X + $fg.W } else { $null }) }
        }
        Start-Sleep -Milliseconds $SampleMs
    }
    $csv = Get-QaArtifactPath ("wander-{0}.csv" -f $Label)
    $rows | Export-Csv -LiteralPath $csv -NoTypeInformation -Encoding UTF8
    return [pscustomobject]@{ Rows = $rows; Csv = $csv }
}

function Get-Stats {
    param($Rows)
    $path = 0.0; $maxDisp = 0.0
    $first = $Rows[0]
    for ($i = 1; $i -lt $Rows.Count; $i++) {
        $dx = $Rows[$i].cx - $Rows[$i - 1].cx; $dy = $Rows[$i].cy - $Rows[$i - 1].cy
        $path += [Math]::Sqrt($dx * $dx + $dy * $dy)
        $ddx = $Rows[$i].cx - $first.cx; $ddy = $Rows[$i].cy - $first.cy
        $maxDisp = [Math]::Max($maxDisp, [Math]::Sqrt($ddx * $ddx + $ddy * $ddy))
    }
    $xs = $Rows | ForEach-Object { $_.cx } | Measure-Object -Minimum -Maximum
    $ys = $Rows | ForEach-Object { $_.cy } | Measure-Object -Minimum -Maximum
    $bot = $Rows | ForEach-Object { $_.bottom } | Measure-Object -Average
    return [pscustomobject]@{ Samples = $Rows.Count; Path = [int]$path; MaxDisp = [int]$maxDisp; XRange = [int]($xs.Maximum - $xs.Minimum); YRange = [int]($ys.Maximum - $ys.Minimum); MeanBottom = [int]$bot.Average
        States = (($Rows | ForEach-Object { $_.state } | Select-Object -Unique) -join ',') }
}

$origCfg = $null
$qw = $null
$moved = @{}
try {
    $proc = Get-QaPetProcess -ProcessId $ProcessId
    $ov = if ($null -ne $proc) { @(Find-PetOverlay -ProcessId $proc.Id) } else { @() }
    $st = Get-PetState -DebugAddr $DebugAddr
    if ($null -eq $st) {
        foreach ($i in 'AC-30', 'AC-32', 'AC-33', 'AC-34', 'AC-35') { Add-QaResult -Id $i -Status FAIL -Message '/debug/state not available' }
        return
    }
    $origCfg = $st.config
    $wa = Get-QaOverlayWork -Hwnd $(if ($ov.Count -gt 0) { $ov[0].Hwnd } else { 0 })
    $mon = $wa.Monitor; $work = $wa.Work
    $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ movement = @{ activity = 1.0 } } -SettleMs 200
    if (-not $NoInput) {
        $null = Save-QaCursor
        Move-QaCursor ($mon.X + $mon.W - 3) ($mon.Y + 3)
    }
    $outside = @()

    foreach ($mode in $Modes) {
        $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ movement = @{ mode = $mode } } -SettleMs 2500
        Write-Host "sampling mode=$mode for $SecondsPerMode s ..."
        $smp = Get-Samples -Seconds $SecondsPerMode -Label $mode
        $rows = @($smp.Rows)
        if ($rows.Count -lt 5) {
            Add-QaResult -Id 'AC-30' -Status FAIL -Message "[$mode] too few /debug/state samples ($($rows.Count))"
            continue
        }
        $stats = Get-Stats $rows
        $reportedModes = @($rows | ForEach-Object { $_.mode } | Select-Object -Unique)
        $out = @($rows | Where-Object { $_.x -lt $mon.X - 2 -or $_.y -lt $mon.Y - 2 -or ($_.x + $_.w) -gt ($mon.X + $mon.W + 2) -or ($_.y + $_.h) -gt ($mon.Y + $mon.H + 2) })
        if ($out.Count -gt 0) { $outside += "[$mode] $($out.Count) samples, e.g. x=$($out[0].x) y=$($out[0].y) w=$($out[0].w) h=$($out[0].h)" }
        $ev = @{ stats = $stats; csv = $smp.Csv; reportedMode = $reportedModes; monitor = $mon; work = $work }
        Write-Host ("  {0}" -f ($stats | ConvertTo-Json -Compress))
        if ($reportedModes -notcontains $mode) {
            Add-QaResult -Id 'AC-36' -Status FAIL -Message ("POST /debug/config movement.mode={0} not applied live (/debug/state pet.mode={1})" -f $mode, ($reportedModes -join ',')) -Evidence $ev
        }
        switch ($mode) {
            'stay' {
                $tol = [Math]::Max(16, 0.15 * $rows[0].w)
                if ($stats.MaxDisp -le $tol) {
                    Add-QaResult -Id 'AC-32' -Status PASS -Message ("[stay] max displacement {0}px over {1}s (tolerance {2}px)" -f $stats.MaxDisp, $SecondsPerMode, [int]$tol) -Evidence $ev
                } else {
                    Add-QaResult -Id 'AC-32' -Status FAIL -Message ("[stay] pet moved {0}px (path {1}px) although mode=stay (tolerance {2}px)" -f $stats.MaxDisp, $stats.Path, [int]$tol) -Evidence $ev
                }
            }
            'free' {
                $moved['free'] = $stats.Path
                $onFloor = @($rows | Where-Object { [Math]::Abs($_.bottom - $work.Bottom) -le 12 }).Count
                $floorShare = $onFloor / $rows.Count
                if ($stats.Path -ge 150 -and ($stats.YRange -ge 60 -or $floorShare -lt 0.5)) {
                    Add-QaResult -Id 'AC-33' -Status PASS -Message ("[free] floated {0}px (x-range {1}, y-range {2}); on floor {3:P0} of the time" -f $stats.Path, $stats.XRange, $stats.YRange, $floorShare) -Evidence $ev
                } else {
                    Add-QaResult -Id 'AC-33' -Status FAIL -Message ("[free] path {0}px, y-range {1}px, on floor {2:P0} -> does not float freely" -f $stats.Path, $stats.YRange, $floorShare) -Evidence $ev
                }
            }
            'ground' {
                $moved['ground'] = $stats.Path
                $airStates = @('perched', 'falling', 'fall', 'jump', 'dragged', 'dangle', 'land')
                $judged = @($rows | Where-Object { $airStates -notcontains $_.state })
                $onFloor = @($judged | Where-Object {
                        ([Math]::Abs($_.bottom - $work.Bottom) -le 16) -or
                        ($null -ne $_.fgTop -and [Math]::Abs($_.bottom - $_.fgTop) -le 16 -and $_.cx -ge $_.fgLeft -and $_.cx -le $_.fgRight)
                    })
                $share = if ($judged.Count -gt 0) { $onFloor.Count / $judged.Count } else { 0 }
                if ($share -ge 0.8 -and $stats.Path -ge 80) {
                    Add-QaResult -Id 'AC-34' -Status PASS -Message ("[ground] walked {0}px; bottom on the taskbar line / a window top in {1:P0} of grounded samples" -f $stats.Path, $share) -Evidence $ev
                } else {
                    Add-QaResult -Id 'AC-34' -Status FAIL -Message ("[ground] path {0}px; on floor only {1:P0} of grounded samples (mean bottom {2} vs work-area bottom {3})" -f $stats.Path, $share, $stats.MeanBottom, $work.Bottom) -Evidence $ev
                }
            }
        }
        if (@($rows | Where-Object { $_.state -eq 'sleep' }).Count -gt 0) { Write-Host "  observed state=sleep in mode $mode" }
    }

    if ($moved.ContainsKey('free') -or $moved.ContainsKey('ground')) {
        $vals = @($moved.Values)
        $ok = @($vals | Where-Object { $_ -ge 80 }).Count -eq $vals.Count
        $msg = ($moved.Keys | ForEach-Object { '{0}={1}px' -f $_, $moved[$_] }) -join ', '
        if ($ok) { Add-QaResult -Id 'AC-30' -Status PASS -Message ("pet wandered on its own with no input: $msg") }
        else { Add-QaResult -Id 'AC-30' -Status FAIL -Message ("pet did not wander on its own (>=80px in ${SecondsPerMode}s expected): $msg") }
    }
    if ($outside.Count -eq 0) { Add-QaResult -Id 'AC-35' -Status PASS -Message ("bbox stayed inside the monitor in modes: " + ($Modes -join ',')) }
    else { Add-QaResult -Id 'AC-35' -Status FAIL -Message ('pet left the monitor bounds: ' + ($outside -join '; ')) }

    # Optional: perch on the foreground window (ground mode)
    if ($PerchSeconds -gt 0) {
        $wx = [int]($work.X + $work.W * 0.25); $wy = [int]($work.Y + $work.H * 0.45)
        $qw = Start-QaWindow -Title 'PetAI QA perch target' -X $wx -Y $wy -W ([int]($work.W * 0.5)) -H ([int]($work.H * 0.4)) -Color 'DDDDDD' -Foreground
        $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ movement = @{ mode = 'ground' } } -SettleMs 500
        $smp = Get-Samples -Seconds $PerchSeconds -Label 'perch'
        $perched = @($smp.Rows | Where-Object { [Math]::Abs($_.bottom - $wy) -le 16 -and $_.cx -ge $wx -and $_.cx -le ($wx + [int]($work.W * 0.5)) })
        if ($perched.Count -gt 0) { Add-QaResult -Id 'AC-37' -Status PASS -Message ("pet perched on the foreground window's top edge ({0} samples)" -f $perched.Count) -Evidence @{ csv = $smp.Csv } }
        else { Add-QaResult -Id 'AC-37' -Status SKIP -Message ("no perch on the foreground window observed in {0}s (probabilistic; verify manually M-13)" -f $PerchSeconds) -Evidence @{ csv = $smp.Csv } }
    }
} catch {
    Add-QaResult -Id 'AC-30' -Status FAIL -Message ('Test-Wander error: ' + $_.Exception.Message)
} finally {
    if (-not $NoInput) { Restore-QaCursor }
    if ($null -ne $origCfg) {
        try { $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ movement = @{ mode = (Get-QaProp $origCfg 'movement.mode' 'ground'); activity = (Get-QaProp $origCfg 'movement.activity' 0.5) } } -SettleMs 0 } catch { }
    }
    Stop-QaWindow $qw
    Stop-QaStartedProcesses
    Write-QaSummary
}
