<#
.SYNOPSIS
  AI / privacy / memory / animation-cache tests for BOTH providers against the mock AI server.
.DESCRIPTION
  Preconditions: mockai running at -MockAddr; petai running with PETAI_DEBUG_ADDR, PETAI_DATA_DIR=-DataDir,
  PETAI_ANTHROPIC_BASE_URL=http://<mock>/anthropic, PETAI_OPENAI_BASE_URL=http://<mock>/openai,
  PETAI_API_KEY_ANTHROPIC/-OPENAI = the fake keys passed here, PETAI_FAST=1.
  Everything is asserted from the mock's request log (black box) + the debug API + files in <data>.
  Results: AC-40..AC-47, AC-49, AC-51..AC-57, AC-61, AC-62, AC-70, AC-72..AC-74, AC-80..AC-87.
  Opens small QA windows (own process) with chosen titles as the "foreground app" (unless -SkipWindows).
  Writes -StateFile (JSON) with what Test-Persistence must find after a restart.
#>
[CmdletBinding()]
param(
    [string]$DebugAddr = '127.0.0.1:47611',
    [string]$MockAddr = '127.0.0.1:47700',
    [Parameter(Mandatory = $true)][string]$DataDir,
    [string]$AnthropicKey = 'sk-test-qa-anthropic-FAKE-7d1e',
    [string]$OpenAIKey = 'sk-test-qa-openai-FAKE-9c2b',
    [ValidateSet('anthropic', 'openai')][string[]]$Providers = @('anthropic', 'openai'),
    [string]$StateFile = '',
    [switch]$SkipWindows,
    [switch]$SkipErrors,
    [int]$ProcessId = 0,
    [string]$ResultsFile = '',
    [string]$ArtifactsDir = '',
    [string]$PidRegistry = ''
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
Initialize-QaContext -ScriptName 'Test-AI' -ResultsFile $ResultsFile -ArtifactsDir $ArtifactsDir -PidRegistry $PidRegistry

$fp = @{ anthropic = (Get-QaFingerprint $AnthropicKey); openai = (Get-QaFingerprint $OpenAIKey) }
$expectPath = @{ anthropic = '/anthropic/v1/messages'; openai = '/openai/v1/chat/completions' }
$wireId = @{ anthropic = 'AC-40'; openai = 'AC-41' }
$occasions = @('greet', 'long_focus', 'app_switch', 'late_night', 'random_chatter', 'user_click')
$invalidScenarios = @('invalid_anim', 'invalid_anim_slot', 'invalid_anim_prop', 'invalid_anim_times', 'invalid_anim_duration',
    'invalid_anim_len', 'invalid_anim_scale', 'invalid_anim_tracks', 'invalid_anim_keys', 'invalid_anim_code', 'invalid_anim_name')
$state = [ordered]@{ animations = @(); memories = @(); createdAt = (Get-Date).ToString('s') }
$blockTokens = @()

# ------------------------------------------------------------------ helpers
function New-Check { return ,(New-Object System.Collections.ArrayList) }   # unary comma: do not unroll the empty list to `$null
function Complete-Check {
    param([string]$Id, $Fails, [string]$PassMsg, $Evidence = $null, [string]$Prefix = '')
    if ($Fails.Count -eq 0) { Add-QaResult -Id $Id -Status PASS -Message ($Prefix + $PassMsg) -Evidence $Evidence }
    else { Add-QaResult -Id $Id -Status FAIL -Message ($Prefix + (($Fails | Select-Object -First 8) -join '; ')) -Evidence $Evidence }
}

function Wait-MockQuiet {
    # Waits until no new AI request arrived for $QuietMs; returns the AI requests since $Since.
    param([long]$Since, [int]$QuietMs = 2000, [int]$MaxMs = 30000)
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $lastCount = -1; $lastChange = 0
    while ($sw.ElapsedMilliseconds -lt $MaxMs) {
        $cnt = @(Get-MockAiRequests -MockAddr $MockAddr -Since $Since).Count
        if ($cnt -ne $lastCount) { $lastCount = $cnt; $lastChange = $sw.ElapsedMilliseconds }
        elseif (($sw.ElapsedMilliseconds - $lastChange) -ge $QuietMs) { break }
        Start-Sleep -Milliseconds 300
    }
    return @(Get-MockAiRequests -MockAddr $MockAddr -Since $Since)
}

function Use-Scenario {
    # Switches the mock scenario WITHOUT clearing its request log (all steps use 'since' markers,
    # and the final key-leak / API-validity checks need every request of the run).
    param([string]$Name, [hashtable]$Options = @{})
    $null = Set-MockScenario -MockAddr $MockAddr -Scenario $Name -Options $Options
}

function Invoke-Observed {
    param([scriptblock]$Action, [int]$QuietMs = 2000)
    $since = Get-MockLastSeq -MockAddr $MockAddr
    $resp = & $Action
    $reqs = Wait-MockQuiet -Since $since -QuietMs $QuietMs
    return [pscustomobject]@{ Resp = $resp; Reqs = $reqs; Since = $since }
}

function Invoke-Trigger { param([string]$Occasion) return (Invoke-PetTrigger -DebugAddr $DebugAddr -Occasion $Occasion -TimeoutSec 120) }

function Get-Action { param($Resp) if ($null -eq $Resp -or $null -eq $Resp.Json) { return $null }; return (Get-QaProp $Resp.Json 'action') }

function Get-Ok { param($Resp) return ($null -ne $Resp -and $Resp.Ok -and $null -ne $Resp.Json -and (Get-QaProp $Resp.Json 'ok' $false) -eq $true) }

function Show-Req {
    param($R)
    if ($null -eq $R) { return $null }
    return [ordered]@{ seq = $R.seq; provider = $R.provider; path = $R.path; status = $R.responseStatus; task = $R.taskTag; occasion = $R.occasion
        hasActivity = $R.hasActivity; activity = $R.activity; images = $R.imageCount; model = $R.model; auth = $R.authScheme; fp = $R.keyFingerprint
        structured = $R.structuredOutput; validationErrors = $R.validationErrors; warnings = $R.warnings }
}

function Find-AnimFiles {
    param([string]$Name)
    $dir = Join-Path $DataDir 'animations'
    if (-not (Test-Path $dir)) { return @() }
    return @(Get-ChildItem -LiteralPath $dir -Recurse -File -Filter ("{0}.json" -f $Name) -ErrorAction SilentlyContinue)
}

function Test-IndexHas {
    param([string]$Name)
    $idx = Join-Path $DataDir 'animations\index.json'
    if (-not (Test-Path $idx)) { return $false }
    return ([IO.File]::ReadAllText($idx).Contains('"' + $Name + '"'))
}

function Test-ListedAnim {
    param([string]$Name)
    $list = @(Get-PetAnimations -DebugAddr $DebugAddr)
    return @($list | Where-Object { $_.name -eq $Name })
}

function Set-Foreground-Window {
    param($Win)
    if ($null -eq $Win -or $Win.Hwnd -eq 0) { return $false }
    $ok = Set-QaForeground -Hwnd $Win.Hwnd
    Start-Sleep -Milliseconds 1600   # watcher polls the foreground window every ~1 s
    $null = Set-QaForeground -Hwnd $Win.Hwnd
    return $ok
}

function Get-PetMemoryMatching { param([string]$Needle) return @(@(Get-PetMemories -DebugAddr $DebugAddr) | Where-Object { "$($_.content)" -like "*$Needle*" }) }

# ------------------------------------------------------------------ main
$orig = $null
$windows = @()
try {
    $st0 = Get-PetState -DebugAddr $DebugAddr
    if ($null -eq $st0) { Add-QaResult -Id 'AC-40' -Status FAIL -Message '/debug/state not available'; return }
    if ($null -eq (Get-MockHealth -MockAddr $MockAddr)) { Add-QaResult -Id 'AC-40' -Status FAIL -Message "mock AI server not reachable at $MockAddr"; return }
    $orig = $st0.config
    $origBlocklist = @(Get-QaProp $orig 'privacy.blocklist' $script:QaDefaultBlocklist)
    $appProc = Get-QaPetProcess -ProcessId $ProcessId

    # Quiet the automatic triggers so the counts below are deterministic (debug calls bypass the limit).
    $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ ai = @{ enabled = $true; maxCallsPerHour = 0 }; privacy = @{ watchActivity = $false; screenshots = $false } } -SettleMs 500
    $null = Reset-Mock -MockAddr $MockAddr -Scenario 'normal'
    Start-Sleep -Seconds 5
    $noise = @(Get-MockAiRequests -MockAddr $MockAddr)
    if ($noise.Count -gt 0) {
        Add-QaResult -Id 'AC-63' -Status FAIL -Message ("{0} automatic AI call(s) in 5 s although ai.maxCallsPerHour=0 (contract: 0 = no automatic comments)" -f $noise.Count) -Evidence @{ occasions = @($noise | ForEach-Object { $_.occasion }) }
    }

    if (-not $SkipWindows) {
        $tokA = New-QaToken 'QAPROBE'
        $probeTitle = "$tokA qa.user@example.com 98765432101 C:\Users\QaPerson\notes.txt https://ex.com/p?t=QAQUERYSECRET sk-QAFAKEKEY0123456789abcdefXYZ"
        $probeWin = Start-QaWindow -Title $probeTitle -X 260 -Y 220 -W 640 -H 360 -Color 'C8E6FF' -Foreground
        $windows += $probeWin
    }

    foreach ($prov in $Providers) {
        $P = "[$prov] "
        Write-Host ("---------------- provider {0}" -f $prov) -ForegroundColor Cyan
        $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ ai = @{ provider = $prov; enabled = $true }; privacy = @{ watchActivity = $false; screenshots = $false } } -SettleMs 800
        $provStart = Get-MockLastSeq -MockAddr $MockAddr
        $stp = Get-PetState -DebugAddr $DebugAddr
        Use-Scenario 'normal'

        # ---------------------------------------------------------- wire format (greet)
        $f = New-Check
        if ((Get-QaProp $stp 'ai.provider') -ne $prov) { [void]$f.Add("/debug/state ai.provider=$(Get-QaProp $stp 'ai.provider') after switching to $prov") }
        if ((Get-QaProp $stp 'ai.hasKey') -ne $true) { [void]$f.Add('/debug/state ai.hasKey is not true with PETAI_API_KEY_* set') }
        $o = Invoke-Observed { Invoke-Trigger 'greet' }
        $mine = @($o.Reqs | Where-Object { $_.occasion -eq 'greet' })
        $act = Get-Action $o.Resp
        if (-not (Get-Ok $o.Resp)) { [void]$f.Add("trigger greet: status=$($o.Resp.Status) body=$($o.Resp.Text) err=$($o.Resp.Error)") }
        if ($null -eq $act -or -not "$($act.speech)") { [void]$f.Add('trigger greet returned no parsed PetAction/speech') }
        if ($mine.Count -eq 0) { [void]$f.Add("no AI request with occasion=greet reached the mock (requests: $(@($o.Reqs).Count))") }
        $other = if ($prov -eq 'anthropic') { 'openai' } else { 'anthropic' }
        if (@($o.Reqs | Where-Object { $_.provider -eq $other }).Count -gt 0) { [void]$f.Add("request(s) went to the $other endpoint while provider=$prov") }
        foreach ($r in $mine) {
            if ($r.provider -ne $prov) { [void]$f.Add("request hit provider prefix '$($r.provider)' (path $($r.path))") }
            if ($r.path -ne $expectPath[$prov]) { [void]$f.Add("path $($r.path) != contract $($expectPath[$prov])") }
            if ($prov -eq 'anthropic' -and $r.authScheme -ne 'x-api-key') { [void]$f.Add("auth scheme $($r.authScheme), contract: x-api-key") }
            if ($prov -eq 'openai' -and $r.authScheme -ne 'bearer') { [void]$f.Add("auth scheme $($r.authScheme), contract: Authorization: Bearer") }
            if ($r.keyFingerprint -ne $fp[$prov]) { [void]$f.Add("key fingerprint $($r.keyFingerprint) != fingerprint of PETAI_API_KEY_$($prov.ToUpper()) ($($fp[$prov]))") }
            if ($prov -eq 'anthropic' -and -not (Get-QaProp $r 'headers.anthropic-version')) { [void]$f.Add('missing anthropic-version header') }
            if (-not $r.model) { [void]$f.Add('empty model') }
            if ($prov -eq 'anthropic' -and $r.model -ne (Get-QaProp $stp 'config.ai.anthropic.model')) { [void]$f.Add("model $($r.model) != config $(Get-QaProp $stp 'config.ai.anthropic.model')") }
            if ($r.taskTag -ne 'pet_action' -or -not $r.taskTagAtStart) { [void]$f.Add("task tag '$($r.taskTag)' atStart=$($r.taskTagAtStart)") }
            if (-not $r.hasLocalTime) { [void]$f.Add('context has no localTime') }
            if ($r.contextError) { [void]$f.Add("context: $($r.contextError)") }
            if ($r.responseStatus -ne 200) { [void]$f.Add("mock answered $($r.responseStatus) ($($r.responseKind)): $(@($r.validationErrors) -join ' | ')") }
        }
        Complete-Check -Id $wireId[$prov] -Fails $f -Prefix $P -PassMsg ("{0} {1} with {2} (fingerprint {3}), model {4}, [task:pet_action] + context, reply parsed (speech '{5}')" -f $mine[0].method, $mine[0].path, $mine[0].authScheme, $mine[0].keyFingerprint, $mine[0].model, "$($act.speech)") -Evidence @{ request = (Show-Req $mine[0]); action = $act }
        if ((Get-QaProp $stp 'ai.provider') -eq $prov -and $mine.Count -gt 0 -and @($mine | Where-Object { $_.provider -ne $prov }).Count -eq 0) {
            Add-QaResult -Id 'AC-42' -Status PASS -Message "${P}provider switched live via config (no restart); requests go to the $prov endpoint"
        } else {
            Add-QaResult -Id 'AC-42' -Status FAIL -Message "${P}switching ai.provider live did not route requests to $prov"
        }

        # ---------------------------------------------------------- all occasions
        $f = New-Check; $occEv = @()
        foreach ($occ in $occasions) {
            $o = Invoke-Observed { Invoke-Trigger $occ } -QuietMs 1200
            $mine = @($o.Reqs | Where-Object { $_.occasion -eq $occ -and $_.taskTag -eq 'pet_action' })
            $act = Get-Action $o.Resp
            $occEv += [ordered]@{ occasion = $occ; ok = (Get-Ok $o.Resp); requests = $mine.Count; speech = "$($act.speech)"; suggestion = "$($act.suggestion)" }
            if (-not (Get-Ok $o.Resp)) { [void]$f.Add("$occ -> trigger not ok ($($o.Resp.Status) $($o.Resp.Error) $($o.Resp.Text))") }
            elseif ($mine.Count -eq 0) { [void]$f.Add("$occ -> no [task:pet_action] request with occasion=$occ") }
            if ($occ -eq 'long_focus' -and (Get-Ok $o.Resp) -and -not "$($act.suggestion)") { [void]$f.Add('long_focus: mock suggestion not present in returned action') }
        }
        Complete-Check -Id 'AC-62' -Fails $f -Prefix $P -PassMsg ("all {0} occasions produced a [task:pet_action] request with the right occasion" -f $occasions.Count) -Evidence @{ occasions = $occEv }

        # ---------------------------------------------------------- chat
        $f = New-Check
        $tokChat = New-QaToken 'QACHAT'
        $o = Invoke-Observed { Invoke-PetChat -DebugAddr $DebugAddr -Text ("halo pet, apa kabar? $tokChat") -TimeoutSec 120 }
        $act = Get-Action $o.Resp
        $hits = (Search-Mock -MockAddr $MockAddr -Needles @($tokChat) -Since $o.Since)[$tokChat]
        if (-not (Get-Ok $o.Resp)) { [void]$f.Add("POST /debug/chat not ok: $($o.Resp.Status) $($o.Resp.Error) $($o.Resp.Text)") }
        if ($null -eq $act -or -not "$($act.speech)") { [void]$f.Add('chat returned no speech') }
        if ($hits.Count -eq 0) { [void]$f.Add('the chat text never reached the AI request') }
        Complete-Check -Id 'AC-44' -Fails $f -Prefix $P -PassMsg ("chat text reached the AI and the reply was parsed: '{0}'" -f "$($act.speech)") -Evidence @{ requestSeqs = $hits; action = $act }

        # ---------------------------------------------------------- privacy: activity
        if (-not $SkipWindows) {
            # watch OFF
            $f = New-Check
            $null = Set-Foreground-Window $probeWin
            $o = Invoke-Observed { Invoke-Trigger 'random_chatter' }
            $withAct = @($o.Reqs | Where-Object { $_.hasActivity })
            $leak = (Search-Mock -MockAddr $MockAddr -Needles @($tokA) -Since $o.Since)[$tokA]
            if ($withAct.Count -gt 0) { [void]$f.Add("$($withAct.Count) request(s) contain 'activity' although watchActivity=false") }
            if ($leak.Count -gt 0) { [void]$f.Add("foreground window title reached the AI although watchActivity=false (seq $($leak -join ','))") }
            $diskLeak = @(Find-QaStringInFiles -Root $DataDir -Needle $tokA)
            if ($diskLeak.Count -gt 0) { [void]$f.Add("foreground title stored on disk although watchActivity=false: $($diskLeak -join ', ')") }
            Complete-Check -Id 'AC-51' -Fails $f -Prefix $P -PassMsg 'watchActivity=false: no activity key, no window title in requests or on disk' -Evidence @{ requests = @($o.Reqs | ForEach-Object { Show-Req $_ }) }

            # watch ON
            $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ privacy = @{ watchActivity = $true } } -SettleMs 300
            $null = Set-Foreground-Window $probeWin
            $f = New-Check; $f53 = New-Check
            $o = Invoke-Observed { Invoke-Trigger 'random_chatter' }
            $mine = @($o.Reqs | Where-Object { $_.occasion -eq 'random_chatter' })
            $r = $mine | Select-Object -First 1
            if ($null -eq $r) { [void]$f.Add('no random_chatter request') }
            elseif (-not $r.hasActivity) { [void]$f.Add('watchActivity=true but the request has no activity key') }
            else {
                $app = "$(Get-QaProp $r.activity 'app')"; $title = "$(Get-QaProp $r.activity 'title')"
                if ($app -notlike '*QaWindow.exe') { [void]$f.Add("activity.app='$app', expected the foreground exe qawindow.exe") }
                elseif ($app -match '[\\/:]') { [void]$f.Add("activity.app is a path ('$app'), contract: bare exe name (paths leak the user name)") }
                elseif ($app -cne $app.ToLower()) { Add-QaResult -Id 'AC-52' -Status WARN -Message "${P}activity.app '$app' is not lower-case (contract: bare lower-case exe name)" }
                if ($title -notlike "*$tokA*") { [void]$f.Add("activity.title '$title' is not the foreground window title") }
                if ($title -notlike '*`[email`]*') { [void]$f53.Add("email not redacted to [email] in '$title'") }
                if ($title -notlike '*`[num`]*') { [void]$f53.Add("11-digit number not redacted to [num] in '$title'") }
                if ($title -notlike '*`[user`]*') { [void]$f53.Add("C:\Users\<name> not redacted to [user] in '$title'") }
                if ($title.Length -gt 160) { [void]$f53.Add("title longer than 160 chars ($($title.Length))") }
            }
            $rawNeedles = @('qa.user@example.com', '98765432101', 'QaPerson', 'QAQUERYSECRET', 'sk-QAFAKEKEY0123456789abcdefXYZ')
            $raw = Search-Mock -MockAddr $MockAddr -Needles $rawNeedles -Since $o.Since
            foreach ($rn in $rawNeedles) { if ($raw[$rn].Count -gt 0) { [void]$f53.Add("raw '$rn' present in a request body (contract: redact email/[num]/[user]/URL query/[secret])") } }
            Complete-Check -Id 'AC-52' -Fails $f -Prefix $P -PassMsg ("watchActivity=true: activity {0}" -f (Format-QaJson $r.activity)) -Evidence @{ request = (Show-Req $r) }
            Complete-Check -Id 'AC-53' -Fails $f53 -Prefix $P -PassMsg ("title redacted: {0}" -f "$(Get-QaProp $r.activity 'title')") -Evidence @{ activity = $r.activity }

            # long_focus with activity -> suggestion context
            $o = Invoke-Observed { Invoke-Trigger 'long_focus' }
            $lf = @($o.Reqs | Where-Object { $_.occasion -eq 'long_focus' }) | Select-Object -First 1
            $act = Get-Action $o.Resp
            if ($null -ne $lf -and $lf.hasActivity -and "$($act.suggestion)") {
                Add-QaResult -Id 'AC-61' -Status PASS -Message ("${P}long_focus request carries the current activity and the returned suggestion is parsed ('{0}'); on-screen display: manual M-11" -f "$($act.suggestion)") -Evidence @{ request = (Show-Req $lf) }
            } else {
                Add-QaResult -Id 'AC-61' -Status FAIL -Message ("${P}long_focus: activity in request={0}, suggestion returned='{1}'" -f $(if ($null -ne $lf) { $lf.hasActivity } else { 'no request' }), "$($act.suggestion)") -Evidence @{ request = (Show-Req $lf) }
            }

            # blocklist
            $cases = @(
                @{ App = 'QaWindow'; Title = 'Internet Banking BCA - {0}'; Why = "title keyword 'bank'/'bca' (case-insensitive)" },
                @{ App = 'QaWindow'; Title = 'Livin by Mandiri {0}'; Why = "title keyword 'mandiri'" },
                @{ App = 'QaWindow'; Title = 'InPrivate - Microsoft Edge {0}'; Why = "title keyword 'inprivate'" },
                @{ App = 'KeePass'; Title = 'Database vault {0}'; Why = "app KeePass.exe ('keepass')" },
                @{ App = 'QaWindow'; Title = 'Project qasecretword notes {0}'; Why = "user-added blocklist entry 'qasecretword'"; Extra = 'qasecretword' }
            )
            $f = New-Check; $caseEv = @()
            foreach ($c in $cases) {
                $tok = New-QaToken 'QABLOCK'
                $blockTokens += $tok
                $title = ($c.Title -f $tok)
                if ($c.Extra) { $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ privacy = @{ blocklist = @($origBlocklist + $c.Extra) } } -SettleMs 300 }
                $w = Start-QaWindow -App $c.App -Title $title -X 320 -Y 260 -W 600 -H 300 -Color 'FFE0E0' -Foreground
                $windows += $w
                $null = Set-Foreground-Window $w
                $fg = Get-QaForeground
                $o = Invoke-Observed { Invoke-Trigger 'random_chatter' }
                $withAct = @($o.Reqs | Where-Object { $_.hasActivity })
                $leak = (Search-Mock -MockAddr $MockAddr -Needles @($tok) -Since $o.Since)[$tok]
                $caseEv += [ordered]@{ case = $c.Why; title = $title; foregroundOk = ($fg.Hwnd -eq $w.Hwnd); requests = @($o.Reqs).Count; withActivity = $withAct.Count; tokenSeqs = $leak; activities = @($withAct | ForEach-Object { $_.activity }) }
                if ($fg.Hwnd -ne $w.Hwnd) { [void]$f.Add("$($c.Why): could not make the test window foreground (inconclusive)") }
                if ($leak.Count -gt 0) { [void]$f.Add("$($c.Why): blocklisted window title reached the AI") }
                if ($withAct.Count -gt 0) { [void]$f.Add("$($c.Why): request still contains an activity key ($(Format-QaJson $withAct[0].activity))") }
                Stop-QaWindow $w
                if ($c.Extra) { $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ privacy = @{ blocklist = $origBlocklist } } -SettleMs 300 }
            }
            Complete-Check -Id 'AC-54' -Fails $f -Prefix $P -PassMsg ("{0} blocklist cases: no activity key and no title text sent" -f $cases.Count) -Evidence @{ cases = $caseEv }

            # screenshots
            $f56 = New-Check; $shotEv = @()
            $null = Set-Foreground-Window $probeWin
            $o = Invoke-Observed { Invoke-Trigger 'screenshot_insight' } -QuietMs 2500
            $imgs = @($o.Reqs | Where-Object { $_.imageCount -gt 0 })
            $shotEv += [ordered]@{ step = 'screenshots=false, screenshot_insight'; requests = @($o.Reqs).Count; withImage = $imgs.Count }
            if ($imgs.Count -gt 0) { [void]$f56.Add('image sent although privacy.screenshots=false') }

            $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ privacy = @{ screenshots = $true } } -SettleMs 300
            $null = Set-Foreground-Window $probeWin
            $o = Invoke-Observed { Invoke-Trigger 'screenshot_insight' } -QuietMs 3000
            $si = @($o.Reqs | Where-Object { $_.occasion -eq 'screenshot_insight' })
            $imgReqs = @($si | Where-Object { $_.imageCount -gt 0 })
            $imgInfo = if ($imgReqs.Count -gt 0) { @($imgReqs[0].images)[0] } else { $null }
            $shotEv += [ordered]@{ step = 'screenshots=true, screenshot_insight'; ok = (Get-Ok $o.Resp); requests = $si.Count; image = $imgInfo }
            if ($imgReqs.Count -eq 0) { [void]$f56.Add("screenshots=true + screenshot_insight: no image attached (requests: $($si.Count), trigger: $($o.Resp.Text))") }
            elseif ($null -ne $imgInfo) {
                if ($imgInfo.mediaType -ne 'image/jpeg' -or -not $imgInfo.jpegMagic) { [void]$f56.Add("image is not JPEG (mediaType=$($imgInfo.mediaType), jpegMagic=$($imgInfo.jpegMagic))") }
                if ([Math]::Max([int]$imgInfo.width, [int]$imgInfo.height) -gt 1280) { [void]$f56.Add("image $($imgInfo.width)x$($imgInfo.height) exceeds 1280 px") }
                if (@($imgReqs[0].images).Count -gt 1) { [void]$f56.Add("$(@($imgReqs[0].images).Count) images in one request") }
            }
            $o = Invoke-Observed { Invoke-Trigger 'random_chatter' }
            $imgs = @($o.Reqs | Where-Object { $_.imageCount -gt 0 })
            $shotEv += [ordered]@{ step = 'screenshots=true, random_chatter'; withImage = $imgs.Count }
            if ($imgs.Count -gt 0) { [void]$f56.Add('image attached to a non-screenshot_insight occasion') }

            $tokS = New-QaToken 'QABLOCK'; $blockTokens += $tokS
            $bw = Start-QaWindow -Title ("Internet Banking BCA {0}" -f $tokS) -X 320 -Y 260 -W 600 -H 300 -Color 'FFE0E0' -Foreground
            $windows += $bw
            $null = Set-Foreground-Window $bw
            Start-Sleep -Seconds 11   # PETAI_FAST screenshot interval (10 min / 60)
            $o = Invoke-Observed { Invoke-Trigger 'screenshot_insight' } -QuietMs 3000
            $imgs = @($o.Reqs | Where-Object { $_.imageCount -gt 0 })
            $shotEv += [ordered]@{ step = 'screenshots=true, blocklisted foreground, screenshot_insight'; withImage = $imgs.Count }
            if ($imgs.Count -gt 0) { [void]$f56.Add('screenshot sent while a blocklisted (banking) window was in the foreground') }
            Stop-QaWindow $bw

            # screenshots=true but watchActivity=false: contract - screenshots are a sub-toggle of the master watch permission
            $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ privacy = @{ watchActivity = $false } } -SettleMs 300
            $null = Set-Foreground-Window $probeWin
            Start-Sleep -Seconds 11
            $o = Invoke-Observed { Invoke-Trigger 'screenshot_insight' } -QuietMs 3000
            $imgs = @($o.Reqs | Where-Object { $_.imageCount -gt 0 })
            $shotEv += [ordered]@{ step = 'screenshots=true, watchActivity=false, screenshot_insight'; withImage = $imgs.Count }
            if ($imgs.Count -gt 0) { [void]$f56.Add("screenshot sent while the master 'watch activity' permission is OFF (screenshots=true only)") }
            $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ privacy = @{ screenshots = $false; watchActivity = $false } } -SettleMs 300
            Complete-Check -Id 'AC-56' -Fails $f56 -Prefix $P -PassMsg ("image only for screenshot_insight with watchActivity+screenshots on and a non-blocklisted app ({0}x{1} JPEG, {2} bytes)" -f $imgInfo.width, $imgInfo.height, $imgInfo.bytes) -Evidence @{ steps = $shotEv }

            $pics = @(Get-ChildItem -LiteralPath $DataDir -Recurse -File -ErrorAction SilentlyContinue | Where-Object { @('.jpg', '.jpeg', '.png', '.bmp', '.webp') -contains $_.Extension.ToLower() -and $_.FullName -notmatch '\\(EBWebView|webview|WebView2)\\' })
            if ($pics.Count -eq 0) { Add-QaResult -Id 'AC-57' -Status PASS -Message "${P}no image files written under the data dir after screenshot_insight" }
            else { Add-QaResult -Id 'AC-57' -Status FAIL -Message ("${P}image files found under the data dir: " + (($pics | Select-Object -First 5 | ForEach-Object { $_.FullName }) -join ', ')) }
        }

        # ---------------------------------------------------------- animations
        $anim = if ($prov -eq 'anthropic') { 'qa_spin' } else { 'qa_spin_oai' }
        $f80 = New-Check
        Use-Scenario 'new_anim' -Options @{ animName = $anim }
        # Sample pet.animation WHILE the generation may still be running (it can be asynchronous):
        # stop once the mock was quiet for 3 s and the animation was seen (or 8 s passed).
        $since = Get-MockLastSeq -MockAddr $MockAddr
        $resp = Invoke-Trigger 'user_click'
        $seen = $false; $sw = [Diagnostics.Stopwatch]::StartNew(); $lastCnt = -1; $lastChange = 0
        while ($sw.ElapsedMilliseconds -lt 25000) {
            if ((Get-QaProp (Get-PetState -DebugAddr $DebugAddr -TimeoutSec 2) 'pet.animation') -eq $anim) { $seen = $true }
            $cnt = @(Get-MockAiRequests -MockAddr $MockAddr -Since $since).Count
            if ($cnt -ne $lastCnt) { $lastCnt = $cnt; $lastChange = $sw.ElapsedMilliseconds }
            elseif (($sw.ElapsedMilliseconds - $lastChange) -ge 3000 -and ($seen -or $sw.ElapsedMilliseconds -gt 8000)) { break }
            Start-Sleep -Milliseconds 80
        }
        $o = [pscustomobject]@{ Resp = $resp; Reqs = @(Get-MockAiRequests -MockAddr $MockAddr -Since $since); Since = $since }
        $spec = @($o.Reqs | Where-Object { $_.taskTag -eq 'animation_spec' })
        $act = Get-Action $o.Resp
        $files = @(Find-AnimFiles $anim)
        $listed = @(Test-ListedAnim $anim)
        if ("$(Get-QaProp $act 'new_animation_request.name')" -ne $anim) { [void]$f80.Add("trigger response action.new_animation_request.name='$(Get-QaProp $act 'new_animation_request.name')'") }
        if ($spec.Count -ne 1) { [void]$f80.Add("expected exactly 1 [task:animation_spec] request, got $($spec.Count)") }
        foreach ($sr in $spec) {
            if (-not $sr.structuredOutput) { [void]$f80.Add('animation_spec request without structured output') }
            if ($sr.responseStatus -ne 200) { [void]$f80.Add("animation_spec answered $($sr.responseStatus): $(@($sr.validationErrors) -join ' | ')") }
        }
        if ($files.Count -ne 1) { [void]$f80.Add("expected 1 file <data>\animations\<target>\$anim.json, found $($files.Count)") }
        else {
            $targetDir = Split-Path -Leaf (Split-Path -Parent $files[0].FullName)
            if (@('generic', 'blob', 'cat', 'chick') -notcontains $targetDir) { [void]$f80.Add("saved under '$targetDir', expected animations\<generic|blob|cat|chick>\") }
            $vr = Test-MockSpec -MockAddr $MockAddr -JsonText ([IO.File]::ReadAllText($files[0].FullName))
            if (-not $vr.valid) { [void]$f80.Add("saved spec fails contract validation: $(@($vr.errors) -join ' | ')") }
        }
        if (-not (Test-IndexHas $anim)) { [void]$f80.Add('animations\index.json missing or does not list it') }
        if ($listed.Count -eq 0) { [void]$f80.Add('/debug/animations does not list it') }
        elseif ($listed[0].builtin -ne $false) { [void]$f80.Add("/debug/animations builtin=$($listed[0].builtin), expected false") }
        $ev80 = @{ specRequests = @($spec | ForEach-Object { Show-Req $_ }); file = $(if ($files.Count) { $files[0].FullName } else { $null }); listed = $listed; playedObserved = $seen }
        Complete-Check -Id 'AC-80' -Fails $f80 -Prefix $P -PassMsg ("new animation '{0}' generated by exactly one animation_spec call, saved to {1}, listed (builtin:false){2}" -f $anim, $(if ($files.Count) { $files[0].FullName.Substring($DataDir.Length) } else { '' }), $(if ($seen) { ', played' } else { '' })) -Evidence $ev80
        if (-not $seen -and $f80.Count -eq 0) { Add-QaResult -Id 'AC-80' -Status WARN -Message "${P}pet.animation never showed '$anim' within 2.5 s after generation (may have finished before sampling)" }
        $state.animations += $anim

        # reuse, same session (trigger + chat)
        Use-Scenario 'new_anim' -Options @{ animName = $anim }
        $o = Invoke-Observed { Invoke-Trigger 'user_click' } -QuietMs 3000
        $o2 = Invoke-Observed { Invoke-PetChat -DebugAddr $DebugAddr -Text 'tunjukkan gerakan spin kamu lagi' -TimeoutSec 120 } -QuietMs 3000
        $spec2 = @(@($o.Reqs) + @($o2.Reqs) | Where-Object { $_.taskTag -eq 'animation_spec' })
        $pa2 = @(@($o.Reqs) + @($o2.Reqs) | Where-Object { $_.taskTag -eq 'pet_action' })
        if ($spec2.Count -eq 0 -and $pa2.Count -ge 2) {
            Add-QaResult -Id 'AC-81' -Status PASS -Message ("${P}'{0}' requested again twice (trigger + chat): 0 animation_spec calls, cached file reused" -f $anim) -Evidence @{ petActionRequests = $pa2.Count }
        } else {
            Add-QaResult -Id 'AC-81' -Status FAIL -Message ("${P}re-requesting '{0}': {1} animation_spec call(s) (expected 0), {2} pet_action call(s)" -f $anim, $spec2.Count, $pa2.Count) -Evidence @{ spec = @($spec2 | ForEach-Object { Show-Req $_ }) }
        }

        # existing built-in name requested as "new"
        Use-Scenario 'new_anim_builtin'
        $o = Invoke-Observed { Invoke-Trigger 'user_click' } -QuietMs 2500
        $spec3 = @($o.Reqs | Where-Object { $_.taskTag -eq 'animation_spec' })
        if ($spec3.Count -eq 0) { Add-QaResult -Id 'AC-87' -Status PASS -Message "${P}new_animation_request for built-in 'wave' -> no generation call" }
        else { Add-QaResult -Id 'AC-87' -Status FAIL -Message "${P}new_animation_request for built-in 'wave' caused $($spec3.Count) animation_spec call(s)" }

        # invalid specs
        $f82 = New-Check; $f84 = New-Check; $invEv = @()
        foreach ($sc in $invalidScenarios) {
            $bad = if ($sc -eq 'invalid_anim') { 'qa_bad_combo' } elseif ($sc -eq 'invalid_anim_name') { 'qa_bad_name' } else { 'qa_bad_' + $sc.Substring('invalid_anim_'.Length) }
            Use-Scenario $sc
            $o = Invoke-Observed { Invoke-Trigger 'user_click' } -QuietMs 2500
            $specN = @($o.Reqs | Where-Object { $_.taskTag -eq 'animation_spec' }).Count
            $alive = Test-PetAlive -DebugAddr $DebugAddr
            $saved = @(Find-AnimFiles $bad)
            $listedBad = @(Test-ListedAnim $bad)
            $idx = Test-IndexHas $bad
            $cur = Get-QaProp (Get-PetState -DebugAddr $DebugAddr) 'pet.animation'
            $evil = @()
            if ($sc -eq 'invalid_anim_name') {
                $evil = @(Get-ChildItem -LiteralPath $DataDir -Recurse -File -Filter 'qa_evil*' -ErrorAction SilentlyContinue | ForEach-Object { $_.FullName })
                $evil += @(Get-ChildItem -LiteralPath (Split-Path -Parent $DataDir) -File -Filter 'qa_evil*' -ErrorAction SilentlyContinue | ForEach-Object { $_.FullName })
            }
            $invEv += [ordered]@{ scenario = $sc; name = $bad; specCalls = $specN; savedFiles = @($saved | ForEach-Object { $_.FullName }); listed = $listedBad.Count; inIndex = $idx; alive = $alive.Alive; petAnimation = $cur; traversalFiles = $evil }
            if (-not $alive.Alive) { [void]$f82.Add("$sc killed/hung the app"); break }
            if ($specN -ne 1) { [void]$f82.Add("$sc -> $specN animation_spec calls (contract: exactly one)") }
            if ($sc -ne 'invalid_anim_name') {
                # (invalid_anim_name: contract clarification - the saved name is always the requested name, so saving it as qa_bad_name is correct)
                if ($saved.Count -gt 0 -or $listedBad.Count -gt 0 -or $idx) { [void]$f82.Add("$sc -> invalid spec '$bad' was saved/listed") }
                if ($cur -eq $bad) { [void]$f82.Add("$sc -> pet is playing the invalid animation") }
            } elseif ($saved.Count -gt 0) {
                $vr = Test-MockSpec -MockAddr $MockAddr -JsonText ([IO.File]::ReadAllText($saved[0].FullName))
                if ($vr.name -ne $bad) { [void]$f84.Add("spec with name '../../qa_evil' was saved with name '$($vr.name)' instead of the requested '$bad'") }
            }
            if ($sc -in @('invalid_anim_code', 'invalid_anim_prop', 'invalid_anim_slot') -and ($saved.Count -gt 0 -or $listedBad.Count -gt 0)) { [void]$f84.Add("$sc (code-like/prototype values) accepted") }
            if ($evil.Count -gt 0) { [void]$f84.Add("path-traversal name wrote a file outside animations\: $($evil -join ', ')") }
        }
        Complete-Check -Id 'AC-82' -Fails $f82 -Prefix $P -PassMsg ("{0} invalid specs (each breaking one rule) rejected: not saved, not listed, not played, app alive" -f $invalidScenarios.Count) -Evidence @{ cases = $invEv }
        Complete-Check -Id 'AC-84' -Fails $f84 -Prefix $P -PassMsg 'code strings / __proto__ / constructor / ../ names in AI specs never accepted or written outside the library' -Evidence @{ cases = @($invEv | Where-Object { $_.scenario -in @('invalid_anim_code', 'invalid_anim_prop', 'invalid_anim_slot', 'invalid_anim_name') }) }

        # clamping
        $clampName = "qa_clamp_$prov"
        Use-Scenario 'clamp_anim' -Options @{ animName = $clampName }
        $o = Invoke-Observed { Invoke-Trigger 'user_click' } -QuietMs 2500
        $cf = @(Find-AnimFiles $clampName)
        if ($cf.Count -eq 0) {
            Add-QaResult -Id 'AC-83' -Status FAIL -Message "${P}out-of-range spec '$clampName' was rejected instead of clamped (contract: clamp rotation +-6.2832, position +-2, scale 0.3-2)"
        } else {
            $vr = Test-MockSpec -MockAddr $MockAddr -JsonText ([IO.File]::ReadAllText($cf[0].FullName))
            if ($vr.valid -and @($vr.clampViolations).Count -eq 0) { Add-QaResult -Id 'AC-83' -Status PASS -Message "${P}out-of-range values clamped before saving ($($cf[0].Name))" }
            else { Add-QaResult -Id 'AC-83' -Status FAIL -Message ("${P}saved spec still out of range: " + ((@($vr.clampViolations) + @($vr.errors)) -join ' | ')) -Evidence @{ file = $cf[0].FullName } }
        }

        # play from library without AI
        Use-Scenario 'normal'
        $since = Get-MockLastSeq -MockAddr $MockAddr
        $pr = Invoke-PetPlay -DebugAddr $DebugAddr -Name 'wave'
        $played = Wait-QaUntil -TimeoutMs 1500 -IntervalMs 80 -Condition { (Get-QaProp (Get-PetState -DebugAddr $DebugAddr -TimeoutSec 2) 'pet.animation') -eq 'wave' }
        Start-Sleep -Milliseconds 800
        $after = @(Get-MockAiRequests -MockAddr $MockAddr -Since $since | Where-Object { $_.taskTag -eq 'animation_spec' -or $_.occasion -eq 'user_click' })
        if ($pr.Ok -and $played -and $after.Count -eq 0) { Add-QaResult -Id 'AC-86' -Status PASS -Message "${P}/debug/play wave -> pet.animation=wave, no AI call" }
        else { Add-QaResult -Id 'AC-86' -Status FAIL -Message ("${P}/debug/play wave: http={0} observed={1} aiCalls={2}" -f $pr.Status, $played, $after.Count) }

        # ---------------------------------------------------------- memory
        $tokM = New-QaToken 'QAHABIT'
        $memText = "QA-HABIT $tokM user sering ngoding sampai larut malam ($prov)"
        Use-Scenario 'memory' -Options @{ memoryContent = $memText }
        $o = Invoke-Observed { Invoke-Trigger 'random_chatter' }
        $got = Wait-QaUntil -TimeoutMs 4000 -Condition { @(Get-PetMemoryMatching $tokM).Count -gt 0 }
        $db = Join-Path $DataDir 'petai.db'
        $m = @(Get-PetMemoryMatching $tokM)
        if ($got -and (Test-Path $db)) {
            Add-QaResult -Id 'AC-70' -Status PASS -Message ("${P}memory_ops add -> /debug/memories has it (id={0}, kind={1}, source={2}); petai.db exists" -f $m[0].id, $m[0].kind, $m[0].source) -Evidence @{ memory = $m[0] }
        } else {
            Add-QaResult -Id 'AC-70' -Status FAIL -Message ("${P}memory_ops add not persisted: in /debug/memories={0}, petai.db exists={1}" -f $got, (Test-Path $db))
        }
        if ($got -and "$($m[0].kind)" -ne 'habit') { Add-QaResult -Id 'AC-70' -Status WARN -Message "${P}memory kind '$($m[0].kind)' (AI sent 'habit')" }

        Use-Scenario 'normal'
        $o = Invoke-Observed { Invoke-Trigger 'random_chatter' }
        $used = (Search-Mock -MockAddr $MockAddr -Needles @($tokM) -Since $o.Since)[$tokM]
        if ($used.Count -gt 0) { Add-QaResult -Id 'AC-72' -Status PASS -Message "${P}stored habit is injected into the next prompt" }
        else { Add-QaResult -Id 'AC-72' -Status FAIL -Message "${P}stored habit never appears in the next pet_action prompt (memory not used)" }

        $tokC = New-QaToken 'QACONS'
        Use-Scenario 'memory' -Options @{ memoryContent = "QA-HABIT $tokC" }
        $o = Invoke-Observed { Invoke-Trigger 'consolidate' } -QuietMs 2500
        $mo = @($o.Reqs | Where-Object { $_.taskTag -eq 'memory_ops' })
        $cons = Wait-QaUntil -TimeoutMs 4000 -Condition { @(Get-PetMemoryMatching "QA-CONSOLIDATED QA-HABIT $tokC").Count -gt 0 }
        if ($mo.Count -ge 1 -and $cons) { Add-QaResult -Id 'AC-74' -Status PASS -Message "${P}consolidate -> [task:memory_ops] request, returned op applied" -Evidence @{ request = (Show-Req $mo[0]) } }
        else { Add-QaResult -Id 'AC-74' -Status FAIL -Message ("${P}consolidate: memory_ops requests={0}, applied={1}, trigger={2}" -f $mo.Count, $cons, $o.Resp.Text) }
        if ($cons) { $state.memories += "QA-CONSOLIDATED QA-HABIT $tokC" }

        if ($m.Count -gt 0) {
            Use-Scenario 'memory_forget' -Options @{ forgetId = [long]$m[0].id }
            $o = Invoke-Observed { Invoke-Trigger 'random_chatter' }
            $gone = Wait-QaUntil -TimeoutMs 4000 -Condition { @(Get-PetMemoryMatching $tokM).Count -eq 0 }
            if ($gone) { Add-QaResult -Id 'AC-73' -Status PASS -Message ("${P}memory_ops forget id={0} removed the memory (UI delete / 'lupakan semua': manual M-16)" -f $m[0].id) }
            else { Add-QaResult -Id 'AC-73' -Status FAIL -Message ("${P}memory_ops forget id={0} did not remove the memory" -f $m[0].id) }
        }
        Use-Scenario 'normal'

        # ---------------------------------------------------------- AI errors
        if (-not $SkipErrors) {
            $errEv = @(); $f45 = New-Check
            foreach ($sc in @('refusal', 'http401', 'http429', 'http500', 'http529', 'malformed', 'wrong_shape', 'drop')) {
                Use-Scenario $sc
                $since = Get-MockLastSeq -MockAddr $MockAddr
                $sw = [Diagnostics.Stopwatch]::StartNew()
                $r = Invoke-Trigger 'random_chatter'
                $ms = $sw.ElapsedMilliseconds
                $alive = Test-PetAlive -DebugAddr $DebugAddr -TimeoutSec 5
                $procAlive = ($null -eq $appProc) -or (-not $appProc.HasExited)
                $n = @(Get-MockAiRequests -MockAddr $MockAddr -Since $since | Where-Object { $_.occasion -eq 'random_chatter' }).Count
                $errEv += [ordered]@{ scenario = $sc; httpStatus = $r.Status; ok = (Get-QaProp $r.Json 'ok'); error = (Get-QaProp $r.Json 'error'); action = (Get-QaProp $r.Json 'action'); ms = $ms; aiRequests = $n; alive = $alive.Alive; stateMs = $alive.Ms }
                if (-not $alive.Alive -or -not $procAlive) { [void]$f45.Add("$sc -> app died or /debug/state stopped responding"); break }
                if ($r.Status -eq 0) { [void]$f45.Add("$sc -> /debug/trigger hung or dropped ($($r.Error))") }
                if ($n -gt 6) { [void]$f45.Add("$sc -> $n AI requests for one trigger (retry storm)") }
                if ($sc -in @('malformed', 'wrong_shape') -and (Get-QaProp $r.Json 'ok') -eq $true -and "$(Get-QaProp $r.Json 'action.speech')" -like '*{*') { [void]$f45.Add("$sc -> raw model text shown as speech") }
            }
            Use-Scenario 'normal'
            $rec = Invoke-Trigger 'greet'
            if (-not (Get-Ok $rec) -or $null -eq (Get-Action $rec)) { [void]$f45.Add("no recovery: greet after the error scenarios -> $($rec.Status) $($rec.Text)") }
            Complete-Check -Id 'AC-45' -Fails $f45 -Prefix $P -PassMsg 'refusal/401/429/500/529/malformed/wrong-shape/dropped connection: app stayed responsive and recovered' -Evidence @{ cases = $errEv }

            # slow AI must not freeze the pet
            Add-Type -AssemblyName System.Net.Http
            Use-Scenario 'slow' -Options @{ delayMs = 8000 }
            $handler = New-Object System.Net.Http.HttpClientHandler
            $handler.UseProxy = $false
            $client = New-Object System.Net.Http.HttpClient($handler)
            $client.Timeout = [TimeSpan]::FromSeconds(90)
            $content = New-Object System.Net.Http.StringContent('{"occasion":"random_chatter"}', [Text.Encoding]::UTF8, 'application/json')
            $task = $client.PostAsync(("http://{0}/debug/trigger" -f $DebugAddr), $content)
            $lat = @()
            $sw = [Diagnostics.Stopwatch]::StartNew()
            while (-not $task.IsCompleted -and $sw.Elapsed.TotalSeconds -lt 60) {
                $a = Test-PetAlive -DebugAddr $DebugAddr -TimeoutSec 5
                $lat += $a.Ms
                Start-Sleep -Milliseconds 300
            }
            $done = $task.IsCompleted
            $client.Dispose()
            $maxLat = ($lat | Measure-Object -Maximum).Maximum
            if ($done -and $lat.Count -ge 5 -and $maxLat -le 1500) { Add-QaResult -Id 'AC-46' -Status PASS -Message ("${P}during an 8 s AI call /debug/state answered in <= {0} ms ({1} probes)" -f $maxLat, $lat.Count) }
            else { Add-QaResult -Id 'AC-46' -Status FAIL -Message ("${P}slow AI: completed={0}, probes={1}, max /debug/state latency {2} ms" -f $done, $lat.Count, $maxLat) }
            Use-Scenario 'normal'
        }

        # ---------------------------------------------------------- AI disabled
        $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ ai = @{ enabled = $false } } -SettleMs 500
        Use-Scenario 'normal'
        $since = Get-MockLastSeq -MockAddr $MockAddr
        $r1 = Invoke-Trigger 'greet'
        $r2 = Invoke-PetChat -DebugAddr $DebugAddr -Text 'halo?' -TimeoutSec 30
        Start-Sleep -Seconds 2
        $n = @(Get-MockAiRequests -MockAddr $MockAddr -Since $since).Count
        $alive = Test-PetAlive -DebugAddr $DebugAddr
        $pp = Invoke-PetPlay -DebugAddr $DebugAddr -Name 'jump'
        if ($n -eq 0 -and $alive.Alive -and $pp.Ok) { Add-QaResult -Id 'AC-47' -Status PASS -Message "${P}ai.enabled=false: trigger + chat made 0 network calls; local animations still play" -Evidence @{ trigger = $r1.Text; chat = $r2.Text } }
        else { Add-QaResult -Id 'AC-47' -Status FAIL -Message ("${P}ai.enabled=false: {0} AI request(s), alive={1}, play={2}" -f $n, $alive.Alive, $pp.Status) }
        $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ ai = @{ enabled = $true } } -SettleMs 300

        # ---------------------------------------------------------- structured output / API validity summary
        $all = @(Get-MockAiRequests -MockAddr $MockAddr -Provider $prov -Since $provStart)
        $bad = @($all | Where-Object { $_.responseKind -eq 'validation_error' -or @($_.validationErrors | Where-Object { $_ }).Count -gt 0 -or -not $_.structuredOutput })
        $warns = @($all | ForEach-Object { @($_.warnings) + @($_.schemaIssues) } | Where-Object { $_ } | Select-Object -Unique)
        if ($bad.Count -eq 0) { Add-QaResult -Id 'AC-49' -Status PASS -Message ("${P}all {0} requests of this provider used structured output and passed the real-API validation rules" -f $all.Count) -Evidence @{ warnings = $warns } }
        else { Add-QaResult -Id 'AC-49' -Status FAIL -Message ("${P}{0} request(s) invalid for the real API or without structured output: {1}" -f $bad.Count, ((@($bad | ForEach-Object { "$($_.taskTag): $(@($_.validationErrors) -join ' / ') structured='$($_.structuredOutput)'" }) | Select-Object -First 3) -join ' || ')) -Evidence @{ warnings = $warns } }
        if ($warns.Count -gt 0) { Write-Host ("  mock warnings: " + ($warns -join ' | ')) -ForegroundColor Yellow }
    }

    # ---------------------------------------------------------- built-ins via the debug API
    $list = @(Get-PetAnimations -DebugAddr $DebugAddr)
    $missing = @($script:QaBuiltinAnimations | Where-Object { $n = $_; @($list | Where-Object { $_.name -eq $n -and $_.builtin -eq $true }).Count -eq 0 })
    if ($missing.Count -eq 0) { Add-QaResult -Id 'AC-85' -Status PASS -Message ("/debug/animations lists all {0} built-ins with builtin:true" -f $script:QaBuiltinAnimations.Count) }
    else { Add-QaResult -Id 'AC-85' -Status FAIL -Message ('/debug/animations is missing built-in(s): ' + ($missing -join ', ')) }

    # ---------------------------------------------------------- key secrecy
    $f43 = New-Check
    $needles = @($AnthropicKey, $OpenAIKey, 'qa-anthropic-FAKE', 'qa-openai-FAKE')
    $all = @(Get-MockRequests -MockAddr $MockAddr)
    if (@($all | Where-Object { $_.bodyContainsAuthKey }).Count -gt 0) { [void]$f43.Add('an API key appears inside a request body') }
    $hits = Search-Mock -MockAddr $MockAddr -Needles $needles
    foreach ($nd in $needles) { if ($hits[$nd].Count -gt 0) { [void]$f43.Add("'$nd' found in request bodies") } }
    $cross = @($all | Where-Object { ($_.provider -eq 'openai' -and $_.keyFingerprint -eq $fp.anthropic) -or ($_.provider -eq 'anthropic' -and $_.keyFingerprint -eq $fp.openai) })
    if ($cross.Count -gt 0) { [void]$f43.Add('a key was sent to the other provider') }
    foreach ($ep in @('/debug/state', '/debug/animations', '/debug/memories')) {
        $t = (Invoke-QaHttp -Url ("http://$DebugAddr$ep")).Text
        foreach ($nd in $needles) { if ($t -and $t.Contains($nd)) { [void]$f43.Add("'$nd' visible in $ep") } }
    }
    $t = (Invoke-QaHttp -Method POST -Url "http://$DebugAddr/debug/config" -Body '{}').Text
    foreach ($nd in $needles) { if ($t -and $t.Contains($nd)) { [void]$f43.Add("'$nd' visible in POST /debug/config response") } }
    foreach ($nd in $needles) {
        $files = @(Find-QaStringInFiles -Root $DataDir -Needle $nd)
        if ($files.Count -gt 0) { [void]$f43.Add("'$nd' written to disk: $($files -join ', ')") }
    }
    Complete-Check -Id 'AC-43' -Fails $f43 -PassMsg 'fake keys never in request bodies, debug API output or any file under the data dir; each key only sent to its own provider' -Evidence @{ needles = @('<anthropic key>', '<openai key>', 'qa-anthropic-FAKE', 'qa-openai-FAKE'); dataDir = $DataDir }

    # blocklisted titles must never be stored either
    $stored = @()
    foreach ($tk in $blockTokens) { $stored += @(Find-QaStringInFiles -Root $DataDir -Needle $tk) }
    if ($blockTokens.Count -gt 0) {
        if ($stored.Count -eq 0) { Add-QaResult -Id 'AC-54' -Status PASS -Message 'blocklisted window titles were never written to the data dir (DB/logs)' }
        else { Add-QaResult -Id 'AC-54' -Status FAIL -Message ('blocklisted window titles stored on disk: ' + (($stored | Select-Object -Unique) -join ', ')) }
    }
} catch {
    Add-QaResult -Id 'AC-40' -Status FAIL -Message ('Test-AI aborted: ' + $_.Exception.Message + ' @ ' + $_.InvocationInfo.ScriptLineNumber)
} finally {
    foreach ($w in $windows) { Stop-QaWindow $w }
    Stop-QaStartedProcesses
    if ($null -ne $orig) {
        try {
            $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{
                ai      = @{ provider = (Get-QaProp $orig 'ai.provider' 'anthropic'); enabled = (Get-QaProp $orig 'ai.enabled' $true); maxCallsPerHour = (Get-QaProp $orig 'ai.maxCallsPerHour' 12) }
                privacy = @{ watchActivity = (Get-QaProp $orig 'privacy.watchActivity' $false); screenshots = (Get-QaProp $orig 'privacy.screenshots' $false); blocklist = $origBlocklist }
            } -SettleMs 0
        } catch { }
    }
    try { $null = Set-MockScenario -MockAddr $MockAddr -Scenario 'normal' } catch { }
    if ($StateFile) { [IO.File]::WriteAllText($StateFile, (ConvertTo-Json -InputObject $state -Depth 5), (New-Object Text.UTF8Encoding($false))) }
    Write-QaSummary
}
