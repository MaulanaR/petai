<#
.SYNOPSIS
  Screenshots of the pet region (and optionally the whole monitor) into the artifacts dir.
.DESCRIPTION
  Uses System.Drawing CopyFromScreen (physical pixels). Requires the app to run with
  PETAI_EXCLUDE_CAPTURE=0, otherwise the pet is excluded from captures.

  With -Backdrop a magenta QA window is placed under the overlay first, which turns the
  screenshots into measurements:
    AC-10  the pet is really drawn where /debug/state says (>= MinPetFraction non-magenta pixels in its box)
    AC-21  the overlay is transparent: regions far from the pet show the magenta backdrop (<= 2% other pixels)
  Without -Backdrop it only saves images for manual review.
#>
[CmdletBinding()]
param(
    [string]$DebugAddr = '127.0.0.1:47611',
    [int]$ProcessId = 0,
    [string]$Label = 'pet',
    [int]$Margin = 60,
    [switch]$Backdrop,
    [switch]$Full,
    [double]$MinPetFraction = 0.03,
    [string]$ResultsFile = '',
    [string]$ArtifactsDir = '',
    [string]$PidRegistry = '',
    [string]$MockAddr = ''
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
Initialize-QaContext -ScriptName 'Capture-Screen' -ResultsFile $ResultsFile -ArtifactsDir $ArtifactsDir -PidRegistry $PidRegistry

$bd = $null
$origMode = $null
$fullShot = $null
try {
    $proc = Get-QaPetProcess -ProcessId $ProcessId
    $ov = if ($null -ne $proc) { @(Find-PetOverlay -ProcessId $proc.Id) } else { @() }
    $state = Get-PetState -DebugAddr $DebugAddr
    if ($ov.Count -eq 0 -or $null -eq $state) {
        if ($Backdrop) {
            Add-QaResult -Id 'AC-10' -Status FAIL -Message 'overlay window or /debug/state not available'
            Add-QaResult -Id 'AC-21' -Status FAIL -Message 'overlay window or /debug/state not available'
        }
        return
    }
    $rep = Get-QaWindowReport -Hwnd $ov[0].Hwnd
    $mon = $rep.Monitor
    if ($rep.DisplayAffinityValue -ne 0) {
        $msg = ("overlay display affinity is {0}: the pet is excluded from screen capture (run the app with PETAI_EXCLUDE_CAPTURE=0)" -f $rep.DisplayAffinity)
        if ($Backdrop) { Add-QaResult -Id 'AC-10' -Status SKIP -Message $msg; Add-QaResult -Id 'AC-21' -Status SKIP -Message $msg }
        else { Write-Warning $msg }
        return
    }

    if ($Backdrop) {
        $origMode = Get-QaProp $state 'config.movement.mode' $null
        $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ movement = @{ mode = 'stay' } } -SettleMs 1200
        $bd = Start-QaBackdrop -Work $rep.Work -NoActivate
        Start-Sleep -Milliseconds 1200
    }

    # Pet box before/after the capture must agree (pet not moving during the shot).
    $box = $null; $shot = $null
    for ($attempt = 1; $attempt -le 4; $attempt++) {
        $b1 = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
        $shot = Save-QaScreenshot -X $mon.X -Y $mon.Y -W $mon.W -H $mon.H -Name ("screen-{0}.png" -f $Label) -KeepBitmap
        $b2 = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
        if ($null -ne $b1 -and $null -ne $b2 -and [Math]::Abs($b1.CX - $b2.CX) -lt 12 -and [Math]::Abs($b1.CY - $b2.CY) -lt 12) { $box = $b2; break }
        $shot.Bitmap.Dispose(); $shot = $null
        Start-Sleep -Milliseconds 400
    }
    if ($null -eq $shot) {
        $shot = Save-QaScreenshot -X $mon.X -Y $mon.Y -W $mon.W -H $mon.H -Name ("screen-{0}.png" -f $Label) -KeepBitmap
        $box = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
    }
    $fullShot = $shot.Bitmap

    # Crop of the pet region (+margin) for review.
    $crop = Get-QaClampedRect -X ($box.X - $Margin) -Y ($box.Y - $Margin) -W ($box.W + 2 * $Margin) -H ($box.H + 2 * $Margin) -Monitor $mon
    $cropRect = New-Object System.Drawing.Rectangle(($crop.X - $mon.X), ($crop.Y - $mon.Y), $crop.W, $crop.H)
    $cropBmp = $fullShot.Clone($cropRect, $fullShot.PixelFormat)
    $petPng = Get-QaArtifactPath ("pet-{0}.png" -f $Label)
    $cropBmp.Save($petPng, [System.Drawing.Imaging.ImageFormat]::Png)
    $cropBmp.Dispose()
    Write-Host "saved $petPng"
    if (-not $Full -and -not $Backdrop) { Remove-Item -LiteralPath $shot.Path -Force -ErrorAction SilentlyContinue }

    if ($Backdrop) {
        $petRect = New-Object System.Drawing.Rectangle(([int]$box.X - $mon.X), ([int]$box.Y - $mon.Y), [Math]::Max(1, [int]$box.W), [Math]::Max(1, [int]$box.H))
        $frac = [PetQa.Img]::ForegroundFraction($fullShot, $petRect, 255, 0, 255, 24)
        $color = [PetQa.Img]::MeanForegroundColor($fullShot, $petRect, 255, 0, 255, 24)
        $ev10 = @{ petBox = $box; nonBackdropFraction = [Math]::Round($frac, 4); meanPetColor = $color; screenshot = $shot.Path; petCrop = $petPng }
        if ($box.W -lt 16 -or $box.H -lt 16) {
            Add-QaResult -Id 'AC-10' -Status FAIL -Message ("pet bbox is tiny/empty ({0}x{1})" -f $box.W, $box.H) -Evidence $ev10
        } elseif ($frac -ge $MinPetFraction) {
            Add-QaResult -Id 'AC-10' -Status PASS -Message ("pet pixels drawn inside its reported box: {0:P1} non-backdrop (mean colour {1})" -f $frac, $color) -Evidence $ev10
        } else {
            Add-QaResult -Id 'AC-10' -Status FAIL -Message ("only {0:P2} non-backdrop pixels inside the reported pet box -> pet not drawn there (invisible or bbox/DPI mismatch)" -f $frac) -Evidence $ev10
        }

        # Transparency: sample 120x120 regions far from the pet inside the backdrop.
        $regions = @()
        foreach ($p in @(Get-QaFarPoints -Area $bd.Rect -Box $box -MinDist 300 -Grid 5 -Backdrop $bd)) {
            $rx = $p.X - 60; $ry = $p.Y - 60
            if (Test-QaInLabelZone $bd ($rx) ($ry)) { continue }
            $r = New-Object System.Drawing.Rectangle(($rx - $mon.X), ($ry - $mon.Y), 120, 120)
            $f = [PetQa.Img]::ForegroundFraction($fullShot, $r, 255, 0, 255, 24)
            $regions += [pscustomobject]@{ X = $rx; Y = $ry; NonBackdrop = [Math]::Round($f, 4); Mean = [PetQa.Img]::MeanForegroundColor($fullShot, $r, 255, 0, 255, 24) }
        }
        $bad = @($regions | Where-Object { $_.NonBackdrop -gt 0.02 })
        $ev21 = @{ regions = $regions; screenshot = $shot.Path }
        if ($regions.Count -lt 3) {
            Add-QaResult -Id 'AC-21' -Status SKIP -Message 'not enough sample regions far from the pet' -Evidence $ev21
        } elseif ($bad.Count -eq 0) {
            Add-QaResult -Id 'AC-21' -Status PASS -Message ("{0} regions away from the pet show the backdrop unchanged -> overlay background is transparent" -f $regions.Count) -Evidence $ev21
        } else {
            Add-QaResult -Id 'AC-21' -Status FAIL -Message ("{0}/{1} regions away from the pet are covered by overlay pixels (e.g. mean colour {2}) -> background not transparent" -f $bad.Count, $regions.Count, $bad[0].Mean) -Evidence $ev21
        }
    }
} catch {
    if ($Backdrop) { Add-QaResult -Id 'AC-10' -Status FAIL -Message ('Capture-Screen error: ' + $_.Exception.Message) }
    else { Write-Warning $_.Exception.Message }
} finally {
    if ($null -ne $fullShot) { $fullShot.Dispose() }
    if ($null -ne $origMode) { try { $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ movement = @{ mode = $origMode } } -SettleMs 0 } catch { } }
    Stop-QaWindow $bd
    Stop-QaStartedProcesses
    Write-QaSummary
}
