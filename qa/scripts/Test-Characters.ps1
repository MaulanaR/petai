<#
.SYNOPSIS
  Switches through the characters (blob, cat, chick), checks each is applied live and that they
  look different on screen (silhouette + colour), saving a screenshot of each.
.DESCRIPTION
  AC-31: >= 3 characters selectable and visually distinct. A magenta backdrop is placed under the
  overlay; for every character the pet box is captured twice (to measure animation noise) and a
  32x32 silhouette mask + mean colour is computed. Two characters count as distinct when their
  silhouette IoU is clearly below each one's own frame-to-frame IoU, or their colours differ.
  Requires PETAI_EXCLUDE_CAPTURE=0. Restores the original character.
#>
[CmdletBinding()]
param(
    [string]$DebugAddr = '127.0.0.1:47611',
    [int]$ProcessId = 0,
    [string[]]$Characters = @('blob', 'cat', 'chick'),
    [string]$ResultsFile = '',
    [string]$ArtifactsDir = '',
    [string]$PidRegistry = '',
    [string]$MockAddr = ''
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
Initialize-QaContext -ScriptName 'Test-Characters' -ResultsFile $ResultsFile -ArtifactsDir $ArtifactsDir -PidRegistry $PidRegistry

function Get-ColorDist { param([string]$A, [string]$B)
    $a = $A.Split(',') | ForEach-Object { [double]$_ }; $b = $B.Split(',') | ForEach-Object { [double]$_ }
    return [Math]::Sqrt(($a[0] - $b[0]) * ($a[0] - $b[0]) + ($a[1] - $b[1]) * ($a[1] - $b[1]) + ($a[2] - $b[2]) * ($a[2] - $b[2]))
}

$bd = $null
$origCfg = $null
try {
    $proc = Get-QaPetProcess -ProcessId $ProcessId
    $ov = if ($null -ne $proc) { @(Find-PetOverlay -ProcessId $proc.Id) } else { @() }
    $st = Get-PetState -DebugAddr $DebugAddr
    if ($ov.Count -eq 0 -or $null -eq $st) { Add-QaResult -Id 'AC-31' -Status FAIL -Message 'overlay window or /debug/state not available'; return }
    $origCfg = $st.config
    $rep = Get-QaWindowReport -Hwnd $ov[0].Hwnd
    $mon = $rep.Monitor
    $canCapture = ($rep.DisplayAffinityValue -eq 0)
    $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ movement = @{ mode = 'stay' } } -SettleMs 800
    if ($canCapture) { $bd = Start-QaBackdrop -Work $rep.Work -NoActivate }

    $info = @{}
    $applied = @()
    foreach ($c in $Characters) {
        $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ pet = @{ character = $c } } -SettleMs 2500
        $s = Get-PetState -DebugAddr $DebugAddr
        $b = Get-PetBox $s
        $applied += [pscustomobject]@{ Requested = $c; Reported = $b.Character; Config = (Get-QaProp $s 'config.pet.character') }
        if (-not $canCapture) { continue }
        $masks = @(); $colors = @(); $fracs = @()
        foreach ($k in 1, 2) {
            $b = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
            $r = Get-QaClampedRect -X $b.X -Y $b.Y -W $b.W -H $b.H -Monitor $mon
            $shot = Save-QaScreenshot -X $r.X -Y $r.Y -W $r.W -H $r.H -Name ("character-{0}-{1}.png" -f $c, $k) -KeepBitmap
            $full = New-Object System.Drawing.Rectangle(0, 0, $r.W, $r.H)
            $masks += [PetQa.Img]::Mask($shot.Bitmap, $full, 255, 0, 255, 24, 32)
            $colors += [PetQa.Img]::MeanForegroundColor($shot.Bitmap, $full, 255, 0, 255, 24)
            $fracs += [PetQa.Img]::ForegroundFraction($shot.Bitmap, $full, 255, 0, 255, 24)
            $shot.Bitmap.Dispose()
            Start-Sleep -Milliseconds 700
        }
        $info[$c] = [pscustomobject]@{ Masks = $masks; Color = $colors[0]; Intra = [PetQa.Img]::IoU($masks[0], $masks[1]); Fill = [Math]::Round($fracs[0], 3); Box = "$([int]$b.W)x$([int]$b.H)" }
    }

    $notApplied = @($applied | Where-Object { $_.Reported -ne $_.Requested })
    if ($notApplied.Count -gt 0) {
        Add-QaResult -Id 'AC-31' -Status FAIL -Message ('character not applied live: ' + (($notApplied | ForEach-Object { "$($_.Requested)->$($_.Reported)" }) -join ', ')) -Evidence @{ applied = $applied }
    }
    if (-not $canCapture) {
        Add-QaResult -Id 'AC-31' -Status SKIP -Message 'visual distinctness not measurable: pet excluded from capture (run with PETAI_EXCLUDE_CAPTURE=0); see M-03'
        return
    }
    $empty = @($Characters | Where-Object { $info[$_].Fill -lt 0.03 })
    $pairs = @()
    for ($i = 0; $i -lt $Characters.Count; $i++) {
        for ($j = $i + 1; $j -lt $Characters.Count; $j++) {
            $a = $info[$Characters[$i]]; $b2 = $info[$Characters[$j]]
            $inter = [PetQa.Img]::IoU($a.Masks[0], $b2.Masks[0])
            $intra = [Math]::Min($a.Intra, $b2.Intra)
            $cd = Get-ColorDist $a.Color $b2.Color
            $distinct = ($inter -lt ($intra - 0.08)) -or ($cd -gt 40)
            $pairs += [pscustomobject]@{ Pair = "$($Characters[$i])/$($Characters[$j])"; IoU = [Math]::Round($inter, 3); IntraIoU = [Math]::Round($intra, 3); ColorDist = [int]$cd; Distinct = $distinct }
        }
    }
    $ev = @{ characters = ($Characters | ForEach-Object { @{ name = $_; color = $info[$_].Color; fill = $info[$_].Fill; box = $info[$_].Box; intraIoU = [Math]::Round($info[$_].Intra, 3) } }); pairs = $pairs; applied = $applied; screenshots = (Get-QaArtifactPath 'character-*.png') }
    $same = @($pairs | Where-Object { -not $_.Distinct })
    if ($Characters.Count -lt 3) {
        Add-QaResult -Id 'AC-31' -Status FAIL -Message 'fewer than 3 characters tested' -Evidence $ev
    } elseif ($empty.Count -gt 0) {
        Add-QaResult -Id 'AC-31' -Status FAIL -Message ('character(s) not visible on screen: ' + ($empty -join ', ')) -Evidence $ev
    } elseif ($same.Count -eq 0 -and $notApplied.Count -eq 0) {
        Add-QaResult -Id 'AC-31' -Status PASS -Message ('3 characters applied live and visually distinct: ' + (($pairs | ForEach-Object { "$($_.Pair) IoU=$($_.IoU) dColor=$($_.ColorDist)" }) -join '; ')) -Evidence $ev
    } else {
        Add-QaResult -Id 'AC-31' -Status FAIL -Message ('characters look the same on screen: ' + (($same | ForEach-Object { $_.Pair }) -join ', ')) -Evidence $ev
    }
} catch {
    Add-QaResult -Id 'AC-31' -Status FAIL -Message ('Test-Characters error: ' + $_.Exception.Message)
} finally {
    if ($null -ne $origCfg) {
        try { $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ pet = @{ character = (Get-QaProp $origCfg 'pet.character' 'blob') }; movement = @{ mode = (Get-QaProp $origCfg 'movement.mode' 'ground') } } -SettleMs 0 } catch { }
    }
    Stop-QaWindow $bd
    Stop-QaStartedProcesses
    Write-QaSummary
}
