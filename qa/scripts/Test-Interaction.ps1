<#
.SYNOPSIS
  Drives real mouse/keyboard input against the pet: right-click menu, click reaction,
  focus-steal check, petting, double-click chat (typed text must reach the AI), drag & drop
  in every movement mode, and dragging past the screen edge.
.DESCRIPTION
  A magenta QA backdrop window (our own process) is put under the overlay so every click that
  passes through lands on our window, never on the user's applications. Keys are only sent when
  the foreground window is the overlay or our backdrop. Cursor and config are restored at the end.
  Results: AC-12 AC-13 AC-14 AC-15 AC-16 AC-17 AC-19 AC-23 AC-35.
#>
[CmdletBinding()]
param(
    [string]$DebugAddr = '127.0.0.1:47611',
    [string]$MockAddr = '127.0.0.1:47700',
    [int]$ProcessId = 0,
    [switch]$SkipChat,
    [string]$ResultsFile = '',
    [string]$ArtifactsDir = '',
    [string]$PidRegistry = ''
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
Initialize-QaContext -ScriptName 'Test-Interaction' -ResultsFile $ResultsFile -ArtifactsDir $ArtifactsDir -PidRegistry $PidRegistry

$VK_ESCAPE = 0x1B
$VK_RETURN = 0x0D

function Watch-Pet {
    param([int]$Ms, [int]$IntervalMs = 100)
    $out = @()
    $sw = [Diagnostics.Stopwatch]::StartNew()
    while ($sw.ElapsedMilliseconds -lt $Ms) {
        $b = Get-PetBox (Get-PetState -DebugAddr $DebugAddr -TimeoutSec 2)
        if ($null -ne $b) { $out += [pscustomobject]@{ T = $sw.ElapsedMilliseconds; State = $b.State; Animation = $b.Animation; CX = [int]$b.CX; CY = [int]$b.CY; Bottom = [int]$b.Bottom } }
        Start-Sleep -Milliseconds $IntervalMs
    }
    return $out
}

function Test-KeySafe {
    # Only send keys when the foreground is the overlay or our backdrop.
    $fg = Get-QaForeground
    return ($null -ne $fg -and ($fg.Hwnd -eq $script:hwnd -or ($null -ne $script:bd -and $fg.Hwnd -eq $script:bd.Hwnd)))
}

function Wait-Interactive {
    param([int]$TimeoutMs = 1500)
    return Wait-QaUntil -TimeoutMs $TimeoutMs -IntervalMs 50 -Condition {
        $s = Get-PetState -DebugAddr $DebugAddr -TimeoutSec 2
        $r = Get-QaWindowReport -Hwnd $script:hwnd
        ((Get-QaProp $s 'window.interactive' $false) -eq $true) -or (-not $r.TRANSPARENT)
    }
}

function Get-Dist { param($Ax, $Ay, $Bx, $By) return [Math]::Sqrt(($Ax - $Bx) * ($Ax - $Bx) + ($Ay - $By) * ($Ay - $By)) }

function Set-Mode {
    param([string]$Mode, [int]$SettleMs = 2000)
    $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ movement = @{ mode = $Mode } } -SettleMs $SettleMs
}

function Get-DragTarget {
    # Target point for a drag of (dx,dy) from the pet centre, kept inside the work area.
    param($Box, [double]$Dx, [double]$Dy)
    $minX = $script:work.X + $Box.W / 2 + 30; $maxX = $script:work.X + $script:work.W - $Box.W / 2 - 30
    $minY = $script:work.Y + $Box.H / 2 + 30; $maxY = $script:work.Y + $script:work.H - $Box.H / 2 - 30
    $tx = $Box.CX + $Dx; $ty = $Box.CY + $Dy
    if ($tx -lt $minX -or $tx -gt $maxX) { $tx = $Box.CX - $Dx }
    if ($ty -lt $minY -or $ty -gt $maxY) { $ty = $Box.CY - $Dy }
    $tx = [Math]::Min($maxX, [Math]::Max($minX, $tx)); $ty = [Math]::Min($maxY, [Math]::Max($minY, $ty))
    return [pscustomobject]@{ X = $tx; Y = $ty }
}

function Invoke-PetDrag {
    # Presses on the pet centre, drags to (tx,ty) and releases. Returns mid/end pet boxes.
    param($Box, [double]$Tx, [double]$Ty)
    Move-QaCursor $Box.CX $Box.CY
    $null = Wait-Interactive -TimeoutMs 1500
    $probe = { Get-PetBox (Get-PetState -DebugAddr $DebugAddr -TimeoutSec 2) }
    return Invoke-QaDrag -X1 $Box.CX -Y1 $Box.CY -X2 $Tx -Y2 $Ty -Steps 24 -StepMs 30 -Probe $probe -HoverMs 80
}

function Measure-NewUi {
    # Screenshot around the pet before/after $Action; counts 48x48 grid cells (outside the pet box) that
    # changed from backdrop to non-backdrop = new UI (menu, bubble, panel) that appeared.
    param([string]$Name, [scriptblock]$Action, [int]$WaitMs = 900)
    $box = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
    $reg = Get-QaClampedRect -X ($box.X - 320) -Y ($box.Y - 320) -W ($box.W + 640) -H ($box.H + 640) -Monitor $script:mon
    $n = 48
    $petCells = New-Object System.Drawing.Rectangle(([int]($box.X - 20 - $reg.X)), ([int]($box.Y - 20 - $reg.Y)), ([int]$box.W + 40), ([int]$box.H + 40))
    $before = Save-QaScreenshot -X $reg.X -Y $reg.Y -W $reg.W -H $reg.H -Name "$Name-before.png" -KeepBitmap
    & $Action
    Start-Sleep -Milliseconds $WaitMs
    $after = Save-QaScreenshot -X $reg.X -Y $reg.Y -W $reg.W -H $reg.H -Name "$Name-after.png" -KeepBitmap
    $full = New-Object System.Drawing.Rectangle(0, 0, $reg.W, $reg.H)
    $m1 = [PetQa.Img]::Mask($before.Bitmap, $full, 255, 0, 255, 24, $n)
    $m2 = [PetQa.Img]::Mask($after.Bitmap, $full, 255, 0, 255, 24, $n)
    $before.Bitmap.Dispose(); $after.Bitmap.Dispose()
    $newCells = 0
    for ($k = 0; $k -lt $m2.Length; $k++) {
        $cx = (($k % $n) + 0.5) * $reg.W / $n; $cy = ([Math]::Floor($k / $n) + 0.5) * $reg.H / $n
        if ($petCells.Contains([int]$cx, [int]$cy)) { continue }
        if ($m2[$k] -eq '1' -and $m1[$k] -eq '0') { $newCells++ }
    }
    return [pscustomobject]@{ NewCells = $newCells; Of = ($n * $n); Before = $before.Path; After = $after.Path; Foreground = (Get-QaForeground).Title }
}

function Close-Ui {
    # Escape (only into the overlay or our backdrop), then a click on an empty spot of our backdrop.
    if (Test-KeySafe) { Send-QaKey $VK_ESCAPE }
    Start-Sleep -Milliseconds 400
    Invoke-QaClick -X $script:park.X -Y $script:park.Y -HoverMs 300
    Start-Sleep -Milliseconds 600
}

$script:bd = $null
$script:hwnd = 0
$origCfg = $null
try {
    $proc = Get-QaPetProcess -ProcessId $ProcessId
    $ov = if ($null -ne $proc) { @(Find-PetOverlay -ProcessId $proc.Id) } else { @() }
    $state0 = Get-PetState -DebugAddr $DebugAddr
    if ($ov.Count -eq 0 -or $null -eq $state0) {
        foreach ($i in 'AC-12', 'AC-14', 'AC-15', 'AC-16') { Add-QaResult -Id $i -Status FAIL -Message 'overlay window or /debug/state not available' }
        return
    }
    $script:hwnd = $ov[0].Hwnd
    Set-QaClickAllow -Pids @($proc.Id)
    $origCfg = $state0.config
    $wa = Get-QaOverlayWork -Hwnd $script:hwnd
    $script:work = $wa.Work
    $mon = $wa.Monitor
    $script:mon = $mon
    $null = Save-QaCursor
    $clickLog = Get-QaArtifactPath 'interaction-backdrop-clicks.txt'
    $script:bd = Start-QaBackdrop -Work $wa.Work -ClickLog $clickLog
    $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ ai = @{ enabled = $false }; movement = @{ mode = 'stay' } } -SettleMs 1500
    $far = @(Get-QaFarPoints -Area $script:bd.Rect -Box (Get-PetBox (Get-PetState -DebugAddr $DebugAddr)) -MinDist 350 -Backdrop $script:bd)
    $park = $far[0]
    $script:park = $park
    Move-QaCursor $park.X $park.Y
    $null = Set-QaForeground -Hwnd $script:bd.Hwnd
    Start-Sleep -Milliseconds 800

    # ------------------------------------------------------------ AC-16 right-click menu
    try {
        $rc = Measure-NewUi -Name 'rightclick' -Action { $b = Get-PetBox (Get-PetState -DebugAddr $DebugAddr); Invoke-QaClick -X $b.CX -Y $b.CY -Button right -HoverMs 400 }
        if ($rc.NewCells -ge 25) {
            Add-QaResult -Id 'AC-16' -Status PASS -Message ("right-click opened new UI next to the pet ({0} grid cells changed); verify the menu items manually: {1}" -f $rc.NewCells, $rc.After) -Evidence $rc
        } else {
            # distinguish "right-click not detected" from "menu broken" with the contract's /debug/ui
            Close-Ui
            $um = Measure-NewUi -Name 'debugui-menu' -Action { $null = Invoke-PetUi -DebugAddr $DebugAddr -Open 'menu' }
            $hint = if ($um.NewCells -ge 25) { 'but POST /debug/ui {open:menu} does show a menu -> the right-click is not detected' } else { 'and POST /debug/ui {open:menu} shows nothing either' }
            Add-QaResult -Id 'AC-16' -Status FAIL -Message ("no menu-like UI appeared within 0.9 s of a real right-click on the pet ({0} changed cells), {1}" -f $rc.NewCells, $hint) -Evidence @{ rightClick = $rc; debugUiMenu = $um }
        }
        Close-Ui
    } catch { Add-QaResult -Id 'AC-16' -Status FAIL -Message ('right-click test error: ' + $_.Exception.Message) }

    # ------------------------------------------------------------ AC-12 / AC-13 / AC-23 click
    try {
        $null = Set-QaForeground -Hwnd $script:bd.Hwnd
        Move-QaCursor $park.X $park.Y
        Start-Sleep -Milliseconds 500
        $baseline = @(Watch-Pet -Ms 1500)
        $baseStates = @($baseline | ForEach-Object { '{0}/{1}' -f $_.State, $_.Animation } | Select-Object -Unique)
        $box = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
        $logBefore = @(Read-QaClickLog $clickLog).Count
        $fgBefore = Get-QaForeground
        Invoke-QaClick -X $box.CX -Y $box.CY -HoverMs 400
        $afterClick = @(Watch-Pet -Ms 2500)
        $fgAfter = Get-QaForeground
        $logAfter = @(Read-QaClickLog $clickLog).Count
        Move-QaCursor $park.X $park.Y
        $novel = @($afterClick | Where-Object { $baseStates -notcontains ('{0}/{1}' -f $_.State, $_.Animation) })
        $firstNovel = if ($novel.Count -gt 0) { $novel[0].T } else { -1 }
        $ev = @{ baseline = $baseStates; afterClick = @($afterClick | ForEach-Object { '{0}ms {1}/{2}' -f $_.T, $_.State, $_.Animation } | Select-Object -Unique -First 30) }
        if ($novel.Count -gt 0 -and $firstNovel -le 1200) {
            Add-QaResult -Id 'AC-12' -Status PASS -Message ("click -> reaction {0}/{1} after {2} ms" -f $novel[0].State, $novel[0].Animation, $firstNovel) -Evidence $ev
        } else {
            Add-QaResult -Id 'AC-12' -Status FAIL -Message 'no state/animation change within 1.2 s after clicking the pet' -Evidence $ev
        }
        if ($fgAfter.Hwnd -eq $fgBefore.Hwnd) {
            Add-QaResult -Id 'AC-13' -Status PASS -Message ("foreground stayed on '{0}' after clicking the pet" -f $fgBefore.Title)
        } else {
            Add-QaResult -Id 'AC-13' -Status FAIL -Message ("clicking the pet stole focus: foreground '{0}' -> '{1}'" -f $fgBefore.Title, $fgAfter.Title)
        }
        if ($logAfter -eq $logBefore) {
            Add-QaResult -Id 'AC-23' -Status PASS -Message 'a real click on the pet was captured by the overlay (did not fall through to the window below)'
        } else {
            Add-QaResult -Id 'AC-23' -Status FAIL -Message 'a real click on the pet fell THROUGH to the window below' -Evidence @{ clickLog = (Read-QaClickLog $clickLog) }
        }
    } catch { Add-QaResult -Id 'AC-12' -Status FAIL -Message ('click test error: ' + $_.Exception.Message) }

    # ------------------------------------------------------------ AC-17 petting (observational)
    try {
        $box = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
        $base = @(Watch-Pet -Ms 800)
        $baseSet = @($base | ForEach-Object { '{0}/{1}' -f $_.State, $_.Animation } | Select-Object -Unique)
        for ($k = 0; $k -lt 14; $k++) {
            $dx = if ($k % 2 -eq 0) { -0.3 * $box.W } else { 0.3 * $box.W }
            Move-QaCursor ($box.CX + $dx) ($box.CY)
            Start-Sleep -Milliseconds 70
        }
        $pet = @(Watch-Pet -Ms 1500)
        Move-QaCursor $park.X $park.Y
        $novel = @($pet | Where-Object { $baseSet -notcontains ('{0}/{1}' -f $_.State, $_.Animation) } | ForEach-Object { '{0}/{1}' -f $_.State, $_.Animation } | Select-Object -Unique)
        if ($novel.Count -gt 0) { Add-QaResult -Id 'AC-17' -Status PASS -Message ('petting (fast wiggle) -> ' + ($novel -join ', ')) }
        else { Add-QaResult -Id 'AC-17' -Status WARN -Message 'no state/animation change observed while petting; check hearts/happy manually (M-07)' }
    } catch { Add-QaResult -Id 'AC-17' -Status WARN -Message ('petting test error: ' + $_.Exception.Message) }

    # ------------------------------------------------------------ AC-14 double-click chat
    if (-not $SkipChat) {
        try {
            $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ ai = @{ enabled = $true } } -SettleMs 500
            if ($MockAddr) { $null = Reset-Mock -MockAddr $MockAddr -Scenario 'normal' }
            $since = if ($MockAddr) { Get-MockLastSeq -MockAddr $MockAddr } else { 0 }
            $null = Set-QaForeground -Hwnd $script:bd.Hwnd
            $box = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
            Invoke-QaDoubleClick -X $box.CX -Y $box.CY -HoverMs 400
            $focused = Wait-QaUntil -TimeoutMs 2500 -IntervalMs 100 -Condition { $f = Get-QaForeground; ($null -ne $f -and $f.Hwnd -eq $script:hwnd) }
            $rep = Get-QaWindowReport -Hwnd $script:hwnd
            $shotRegion = Get-QaClampedRect -X ($box.X - 400) -Y ($box.Y - 400) -W ($box.W + 800) -H ($box.H + 800) -Monitor $mon
            $null = Save-QaScreenshot -X $shotRegion.X -Y $shotRegion.Y -W $shotRegion.W -H $shotRegion.H -Name 'chat-input-open.png'
            if (-not $focused) {
                Close-Ui
                $null = Set-QaForeground -Hwnd $script:bd.Hwnd
                $uc = Invoke-PetUi -DebugAddr $DebugAddr -Open 'chat'
                $viaUi = Wait-QaUntil -TimeoutMs 2500 -IntervalMs 100 -Condition { $fw = Get-QaForeground; ($null -ne $fw -and $fw.Hwnd -eq $script:hwnd) }
                $hint = if ($viaUi) { 'but POST /debug/ui {open:chat} does focus a chat input -> the double-click is not detected' } else { 'and POST /debug/ui {open:chat} does not focus a chat input either' }
                Add-QaResult -Id 'AC-14' -Status FAIL -Message ("double-click did not give the chat input keyboard focus (foreground='{0}', NOACTIVATE={1}), {2}" -f (Get-QaForeground).Title, $rep.NOACTIVATE, $hint) -Evidence @{ screenshot = (Get-QaArtifactPath 'chat-input-open.png'); debugUiChat = $uc.Status }
                Close-Ui
            } else {
                $token = New-QaToken 'QAUICHAT'
                $null = Send-QaText ("halo pet, ini tes QA " + $token)
                Start-Sleep -Milliseconds 300
                if (Test-KeySafe) { Send-QaKey $VK_RETURN }
                $found = $false
                if ($MockAddr) {
                    $found = Wait-QaUntil -TimeoutMs 30000 -IntervalMs 500 -Condition { (Search-Mock -MockAddr $MockAddr -Needles @($token) -Since $since)[$token].Count -gt 0 }
                }
                Start-Sleep -Milliseconds 3500
                $null = Save-QaScreenshot -X $shotRegion.X -Y $shotRegion.Y -W $shotRegion.W -H $shotRegion.H -Name 'chat-reply-bubble.png'
                $ev = @{ noActivateWhileTyping = $rep.NOACTIVATE; token = $token; replyScreenshot = (Get-QaArtifactPath 'chat-reply-bubble.png') }
                if ($found) {
                    Add-QaResult -Id 'AC-14' -Status PASS -Message 'double-click opened a focused chat input; typed text + Enter reached the AI. Check the reply bubble screenshot (M-10).' -Evidence $ev
                } elseif (-not $MockAddr) {
                    Add-QaResult -Id 'AC-14' -Status SKIP -Message 'chat input focused; no -MockAddr to verify the message reached the AI' -Evidence $ev
                } else {
                    Add-QaResult -Id 'AC-14' -Status FAIL -Message 'typed chat text + Enter never reached the AI within 30 s' -Evidence $ev
                }
                # close chat, focus must go back to normal (NOACTIVATE restored)
                if (Test-KeySafe) { Send-QaKey $VK_ESCAPE }
                $restored = Wait-QaUntil -TimeoutMs 2000 -Condition { (Get-QaWindowReport -Hwnd $script:hwnd).NOACTIVATE }
                if (-not $restored) {
                    Invoke-QaClick -X $park.X -Y $park.Y -HoverMs 300
                    $restored = Wait-QaUntil -TimeoutMs 2000 -Condition { (Get-QaWindowReport -Hwnd $script:hwnd).NOACTIVATE }
                    if ($restored) { Add-QaResult -Id 'AC-14' -Status WARN -Message 'Escape did not close the chat input; clicking elsewhere did' }
                }
                if (-not $restored) { Add-QaResult -Id 'AC-14' -Status FAIL -Message 'after closing the chat the overlay never got WS_EX_NOACTIVATE back (pet keeps stealing focus)' }
            }
            Move-QaCursor $park.X $park.Y
        } catch { Add-QaResult -Id 'AC-14' -Status FAIL -Message ('chat test error: ' + $_.Exception.Message) }
        try { $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ ai = @{ enabled = $false } } -SettleMs 300 } catch { }
    }

    # ------------------------------------------------------------ AC-19 settings panel: focus in, Esc -> focus back
    try {
        $null = Set-QaForeground -Hwnd $script:bd.Hwnd
        Move-QaCursor $park.X $park.Y
        Start-Sleep -Milliseconds 400
        $ur = Invoke-PetUi -DebugAddr $DebugAddr -Open 'settings'
        $focused = Wait-QaUntil -TimeoutMs 2500 -IntervalMs 100 -Condition { $fw = Get-QaForeground; ($null -ne $fw -and $fw.Hwnd -eq $script:hwnd) }
        $repS = Get-QaWindowReport -Hwnd $script:hwnd
        Start-Sleep -Milliseconds 700
        $shotS = Save-QaScreenshot -X $script:work.X -Y $script:work.Y -W $script:work.W -H $script:work.H -Name 'settings-panel.png'
        if ($focused -and (Test-KeySafe)) { Send-QaKey $VK_ESCAPE }
        $back = Wait-QaUntil -TimeoutMs 2500 -IntervalMs 100 -Condition {
            $fw = Get-QaForeground
            ($null -ne $fw -and $fw.Hwnd -eq $script:bd.Hwnd -and (Get-QaWindowReport -Hwnd $script:hwnd).NOACTIVATE)
        }
        $ev = @{ debugUi = $ur.Status; focused = $focused; noActivateWhileOpen = $repS.NOACTIVATE; returnedToPrevious = $back; screenshot = $shotS.Path; foregroundAfter = (Get-QaForeground).Title }
        if (-not $ur.Ok) { Add-QaResult -Id 'AC-19' -Status FAIL -Message ("POST /debug/ui {{open:settings}} -> HTTP {0} {1}" -f $ur.Status, $ur.Text) -Evidence $ev }
        elseif ($focused -and $back) { Add-QaResult -Id 'AC-19' -Status PASS -Message ("settings panel took keyboard focus; Esc closed it and focus returned to the previous window; screenshot {0} (layout: M-14)" -f $shotS.Path) -Evidence $ev }
        else { Add-QaResult -Id 'AC-19' -Status FAIL -Message ("settings: took focus={0}; Esc returned focus to the previous window with NOACTIVATE restored={1}" -f $focused, $back) -Evidence $ev }
        if (-not $back) { Close-Ui }
    } catch { Add-QaResult -Id 'AC-19' -Status FAIL -Message ('settings test error: ' + $_.Exception.Message) }

    # ------------------------------------------------------------ AC-15 drag & drop per mode
    $null = Set-QaForeground -Hwnd $script:bd.Hwnd
    # stay: pet follows, stays at drop point, anchor updated
    try {
        Set-Mode 'stay' 1500
        $box = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
        $anchor0 = @((Get-QaProp (Get-PetState -DebugAddr $DebugAddr) 'config.movement.anchorX'), (Get-QaProp (Get-PetState -DebugAddr $DebugAddr) 'config.movement.anchorY'))
        $t = Get-DragTarget -Box $box -Dx 260 -Dy -160
        $d = Invoke-PetDrag -Box $box -Tx $t.X -Ty $t.Y
        Move-QaCursor $park.X $park.Y
        Start-Sleep -Milliseconds 1500
        $s1 = Get-PetState -DebugAddr $DebugAddr
        $b1 = Get-PetBox $s1
        Start-Sleep -Milliseconds 4000
        $b2 = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
        $followEnd = if ($null -ne $d.End) { Get-Dist $d.End.CX $d.End.CY $t.X $t.Y } else { -1 }
        $dropDist = Get-Dist $b1.CX $b1.CY $t.X $t.Y
        $drift = Get-Dist $b1.CX $b1.CY $b2.CX $b2.CY
        $ax = Get-QaProp $s1 'config.movement.anchorX' -1; $ay = Get-QaProp $s1 'config.movement.anchorY' -1
        $anchorNear = ($ax -ge ($b1.X - 80) -and $ax -le ($b1.Right + 80) -and $ay -ge ($b1.Y - 80) -and $ay -le ($b1.Bottom + 80))
        $ev = @{ from = @([int]$box.CX, [int]$box.CY); target = @([int]$t.X, [int]$t.Y); followDistAtEnd = [int]$followEnd; dropDist = [int]$dropDist; driftAfter4s = [int]$drift; anchorBefore = $anchor0; anchorAfter = @($ax, $ay); midState = (Get-QaProp $d.Mid 'State') }
        $tolFollow = [Math]::Max(80, $box.W * 0.75)
        if ($followEnd -ge 0 -and $followEnd -le $tolFollow -and $dropDist -le $tolFollow -and $drift -le 30 -and $anchorNear) {
            Add-QaResult -Id 'AC-15' -Status PASS -Message ("[stay] pet followed the cursor, stayed at the drop point (drift {0}px/4s) and anchor moved to ({1},{2})" -f [int]$drift, $ax, $ay) -Evidence $ev
        } else {
            Add-QaResult -Id 'AC-15' -Status FAIL -Message ("[stay] follow={0}px drop={1}px drift={2}px anchorUpdated={3} (tolerance {4}px)" -f [int]$followEnd, [int]$dropDist, [int]$drift, $anchorNear, [int]$tolFollow) -Evidence $ev
        }
    } catch { Add-QaResult -Id 'AC-15' -Status FAIL -Message ('[stay] drag error: ' + $_.Exception.Message) }

    # free: stays where dropped, no gravity
    try {
        Set-Mode 'free' 1500
        $box = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
        $midY = $script:work.Y + $script:work.H * 0.4
        $t = Get-DragTarget -Box $box -Dx (-200) -Dy ($midY - $box.CY)
        $d = Invoke-PetDrag -Box $box -Tx $t.X -Ty $t.Y
        Move-QaCursor $park.X $park.Y
        Start-Sleep -Milliseconds 500
        $b1 = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
        Start-Sleep -Milliseconds 1500
        $b2 = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
        $followEnd = if ($null -ne $d.End) { Get-Dist $d.End.CX $d.End.CY $t.X $t.Y } else { -1 }
        $dropDist = Get-Dist $b1.CX $b1.CY $t.X $t.Y
        $fellToGround = ([Math]::Abs($b2.Bottom - $script:work.Bottom) -le 16)
        $ev = @{ target = @([int]$t.X, [int]$t.Y); followDistAtEnd = [int]$followEnd; dropDist = [int]$dropDist; bottomAfter2s = [int]$b2.Bottom; workBottom = $script:work.Bottom }
        $tolFollow = [Math]::Max(100, $box.W * 0.75)
        if ($followEnd -ge 0 -and $followEnd -le $tolFollow -and $dropDist -le $tolFollow -and -not $fellToGround) {
            Add-QaResult -Id 'AC-15' -Status PASS -Message ("[free] pet followed the cursor and floated where dropped (no gravity)") -Evidence $ev
        } else {
            Add-QaResult -Id 'AC-15' -Status FAIL -Message ("[free] follow={0}px drop={1}px fellToGround={2}" -f [int]$followEnd, [int]$dropDist, $fellToGround) -Evidence $ev
        }

        # AC-35: drag beyond the right screen edge -> pet must end up fully on the monitor
        $box = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
        $edgeX = $mon.X + $mon.W - 3
        $null = Invoke-PetDrag -Box $box -Tx $edgeX -Ty $box.CY
        Move-QaCursor $park.X $park.Y
        Start-Sleep -Milliseconds 1500
        $b = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
        $inside = ($b.X -ge $mon.X - 2) -and ($b.Right -le $mon.X + $mon.W + 2) -and ($b.Y -ge $mon.Y - 2) -and ($b.Bottom -le $mon.Y + $mon.H + 2)
        $ev = @{ box = $b; monitor = $mon }
        if ($inside) { Add-QaResult -Id 'AC-35' -Status PASS -Message 'pet dragged past the right screen edge ends up fully inside the monitor' -Evidence $ev }
        else { Add-QaResult -Id 'AC-35' -Status FAIL -Message ("pet dragged past the edge stays partly off-screen: x={0} right={1} (monitor {2}..{3})" -f [int]$b.X, [int]$b.Right, $mon.X, ($mon.X + $mon.W)) -Evidence $ev }
    } catch { Add-QaResult -Id 'AC-15' -Status FAIL -Message ('[free] drag error: ' + $_.Exception.Message) }

    # ground: lifted pet falls back to the floor (work-area bottom / taskbar top)
    try {
        Set-Mode 'ground' 3000
        $box = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
        $t = Get-DragTarget -Box $box -Dx 120 -Dy (-[Math]::Min(350, $script:work.H * 0.4))
        $d = Invoke-PetDrag -Box $box -Tx $t.X -Ty $t.Y
        $releasedAt = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
        Move-QaCursor $park.X $park.Y
        $landed = Wait-QaUntil -TimeoutMs 5000 -IntervalMs 150 -Condition {
            $bb = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
            [Math]::Abs($bb.Bottom - $script:work.Bottom) -le 16
        }
        $b = Get-PetBox (Get-PetState -DebugAddr $DebugAddr)
        $followEnd = if ($null -ne $d.End) { Get-Dist $d.End.CX $d.End.CY $t.X $t.Y } else { -1 }
        $ev = @{ target = @([int]$t.X, [int]$t.Y); followDistAtEnd = [int]$followEnd; midState = (Get-QaProp $d.Mid 'State'); releasedBottom = [int]$releasedAt.Bottom; finalBottom = [int]$b.Bottom; workBottom = $script:work.Bottom; finalState = $b.State }
        $tolFollow = [Math]::Max(100, $box.W * 0.75)
        if ($followEnd -ge 0 -and $followEnd -le $tolFollow -and $landed) {
            Add-QaResult -Id 'AC-15' -Status PASS -Message ("[ground] pet dangled at the cursor and fell back to the taskbar line (bottom {0} vs work-area bottom {1})" -f [int]$b.Bottom, $script:work.Bottom) -Evidence $ev
        } else {
            Add-QaResult -Id 'AC-15' -Status FAIL -Message ("[ground] follow={0}px landedOnFloor={1} (bottom {2} vs {3})" -f [int]$followEnd, $landed, [int]$b.Bottom, $script:work.Bottom) -Evidence $ev
        }
    } catch { Add-QaResult -Id 'AC-15' -Status FAIL -Message ('[ground] drag error: ' + $_.Exception.Message) }
} catch {
    Add-QaResult -Id 'AC-12' -Status FAIL -Message ('Test-Interaction error: ' + $_.Exception.Message)
} finally {
    Restore-QaCursor
    if ($null -ne $origCfg) {
        try {
            $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{
                ai       = @{ enabled = (Get-QaProp $origCfg 'ai.enabled' $true) }
                movement = @{ mode = (Get-QaProp $origCfg 'movement.mode' 'ground'); anchorX = (Get-QaProp $origCfg 'movement.anchorX' -1); anchorY = (Get-QaProp $origCfg 'movement.anchorY' -1) }
            } -SettleMs 0
        } catch { }
    }
    Stop-QaWindow $script:bd
    Stop-QaStartedProcesses
    Write-QaSummary
}
