<#
.SYNOPSIS
  Fresh-install privacy defaults: run right after the FIRST start with an empty PETAI_DATA_DIR.
.DESCRIPTION
  AC-50 watchActivity defaults to false        AC-55 screenshots defaults to false
  AC-51 no activity / no image / no window title in any AI request or on disk by default
  AC-92 autostart defaults to false (config) - the registry Run key is diffed by Run-Acceptance
  Also checks other contract defaults (blocklist, retentionDays, excludeFromCapture, maxCallsPerHour)
  and that config.json is written to the data dir.
  With -CorruptConfig: the data dir was seeded with an invalid config.json -> the app must still
  start with defaults (AC-94).
#>
[CmdletBinding()]
param(
    [string]$DebugAddr = '127.0.0.1:47611',
    [string]$MockAddr = '127.0.0.1:47700',
    [Parameter(Mandatory = $true)][string]$DataDir,
    [switch]$CorruptConfig,
    [switch]$SkipWindows,
    [int]$ProcessId = 0,
    [string]$ResultsFile = '',
    [string]$ArtifactsDir = '',
    [string]$PidRegistry = ''
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
Initialize-QaContext -ScriptName 'Test-Privacy-Defaults' -ResultsFile $ResultsFile -ArtifactsDir $ArtifactsDir -PidRegistry $PidRegistry

$win = $null
try {
    $st = Get-PetState -DebugAddr $DebugAddr
    if ($null -eq $st) {
        $id = if ($CorruptConfig) { 'AC-94' } else { 'AC-50' }
        Add-QaResult -Id $id -Status FAIL -Message '/debug/state not available (app not running / crashed on start)'
        return
    }
    $cfgPath = Join-Path $DataDir 'config.json'
    $fileCfg = $null
    $fileOk = $false
    if (Test-Path $cfgPath) {
        try { $fileCfg = ConvertFrom-Json -InputObject ([IO.File]::ReadAllText($cfgPath)); $fileOk = $true } catch { $fileOk = $false }
    }
    $dbg = $st.config

    if ($CorruptConfig) {
        $f = @()
        if ((Get-QaProp $dbg 'privacy.watchActivity') -ne $false) { $f += "watchActivity=$(Get-QaProp $dbg 'privacy.watchActivity')" }
        if ((Get-QaProp $dbg 'privacy.screenshots') -ne $false) { $f += "screenshots=$(Get-QaProp $dbg 'privacy.screenshots')" }
        if ($script:QaCharacters -notcontains (Get-QaProp $dbg 'pet.character')) { $f += "pet.character=$(Get-QaProp $dbg 'pet.character')" }
        if ($script:QaModes -notcontains (Get-QaProp $dbg 'movement.mode')) { $f += "movement.mode=$(Get-QaProp $dbg 'movement.mode')" }
        $ov = @(Find-PetOverlay -ProcessId $ProcessId)
        if ($ov.Count -eq 0) { $f += 'no overlay window' }
        if ($f.Count -eq 0) { Add-QaResult -Id 'AC-94' -Status PASS -Message ("started with a corrupt config.json and fell back to defaults; config.json now valid JSON: {0}" -f $fileOk) }
        else { Add-QaResult -Id 'AC-94' -Status FAIL -Message ('corrupt config.json: ' + ($f -join ', ')) }
        return
    }

    # ---- defaults
    $src = if ($fileOk) { $fileCfg } else { $dbg }
    if (-not (Test-Path $cfgPath)) { Add-QaResult -Id 'AC-95' -Status WARN -Message "config.json not written to the data dir on first start ($cfgPath); checked /debug/state config instead" }
    elseif (-not $fileOk) { Add-QaResult -Id 'AC-95' -Status FAIL -Message "config.json is not valid JSON on first start" }

    $wa = Get-QaProp $src 'privacy.watchActivity'; $wa2 = Get-QaProp $dbg 'privacy.watchActivity'
    if ($wa -eq $false -and $wa2 -eq $false) { Add-QaResult -Id 'AC-50' -Status PASS -Message 'fresh install: privacy.watchActivity=false (config.json and /debug/state)' }
    else { Add-QaResult -Id 'AC-50' -Status FAIL -Message "fresh install: watchActivity file=$wa debug=$wa2 (must default to false)" }

    $ss = Get-QaProp $src 'privacy.screenshots'; $ss2 = Get-QaProp $dbg 'privacy.screenshots'
    if ($ss -eq $false -and $ss2 -eq $false) { Add-QaResult -Id 'AC-55' -Status PASS -Message 'fresh install: privacy.screenshots=false, separate key from watchActivity' }
    else { Add-QaResult -Id 'AC-55' -Status FAIL -Message "fresh install: screenshots file=$ss debug=$ss2 (must default to false)" }

    $as = Get-QaProp $src 'general.autostart'
    if ($as -eq $false) { Add-QaResult -Id 'AC-92' -Status PASS -Message 'fresh install: general.autostart=false' }
    else { Add-QaResult -Id 'AC-92' -Status FAIL -Message "fresh install: general.autostart=$as (must be opt-in)" }

    $other = @()
    $bl = @(Get-QaProp $src 'privacy.blocklist' @())
    $missingBl = @($script:QaDefaultBlocklist | Where-Object { $bl -notcontains $_ })
    if ($missingBl.Count -gt 0) { $other += ('default blocklist missing: ' + ($missingBl -join ',')) }
    if ((Get-QaProp $src 'privacy.retentionDays') -ne 14) { $other += "retentionDays=$(Get-QaProp $src 'privacy.retentionDays')" }
    if ((Get-QaProp $src 'privacy.excludeFromCapture') -ne $true) { $other += "excludeFromCapture=$(Get-QaProp $src 'privacy.excludeFromCapture')" }
    if ((Get-QaProp $src 'ai.maxCallsPerHour') -ne 12) { $other += "maxCallsPerHour=$(Get-QaProp $src 'ai.maxCallsPerHour')" }
    if ((Get-QaProp $src 'version') -ne 1) { $other += "version=$(Get-QaProp $src 'version')" }
    if ($script:QaCharacters -notcontains (Get-QaProp $src 'pet.character')) { $other += "pet.character=$(Get-QaProp $src 'pet.character')" }
    if ($script:QaModes -notcontains (Get-QaProp $src 'movement.mode')) { $other += "movement.mode=$(Get-QaProp $src 'movement.mode')" }
    if ((Get-QaProp $src 'ai.anthropic.model') -ne 'claude-opus-5-5') { $other += "ai.anthropic.model=$(Get-QaProp $src 'ai.anthropic.model')" }
    $cfgText = if (Test-Path $cfgPath) { [IO.File]::ReadAllText($cfgPath) } else { '' }
    if ($cfgText -match '(?i)"(api_?key|apikey|secret|token)"') { $other += 'config.json contains a key/secret/token field' }
    if ($other.Count -eq 0) { Add-QaResult -Id 'AC-95' -Status PASS -Message 'contract defaults present (blocklist, retentionDays=14, excludeFromCapture, maxCallsPerHour=12, version=1, default model)' -Evidence @{ config = $src } }
    else { Add-QaResult -Id 'AC-95' -Status FAIL -Message ('defaults differ from the contract: ' + ($other -join '; ')) -Evidence @{ config = $src } }

    # ---- nothing about the user's screen goes out by default
    $tok = New-QaToken 'QADEFAULT'
    if (-not $SkipWindows) {
        $win = Start-QaWindow -Title "QA default privacy $tok user@example.com" -X 300 -Y 250 -W 600 -H 300 -Color 'E0FFE0' -Foreground
        Start-Sleep -Milliseconds 1800
    }
    $since = Get-MockLastSeq -MockAddr $MockAddr
    $null = Set-MockScenario -MockAddr $MockAddr -Scenario 'normal'
    $r1 = Invoke-PetTrigger -DebugAddr $DebugAddr -Occasion 'greet' -TimeoutSec 90
    $r2 = Invoke-PetTrigger -DebugAddr $DebugAddr -Occasion 'screenshot_insight' -TimeoutSec 90
    $r3 = Invoke-PetTrigger -DebugAddr $DebugAddr -Occasion 'app_switch' -TimeoutSec 90
    Start-Sleep -Seconds 8   # also catch automatic calls (PETAI_FAST)
    $reqs = @(Get-MockAiRequests -MockAddr $MockAddr -Since $since)
    $withAct = @($reqs | Where-Object { $_.hasActivity })
    $withImg = @($reqs | Where-Object { $_.imageCount -gt 0 })
    $leak = (Search-Mock -MockAddr $MockAddr -Needles @($tok) -Since $since)[$tok]
    $disk = @(Find-QaStringInFiles -Root $DataDir -Needle $tok)
    $ev = @{ requests = $reqs.Count; occasions = @($reqs | ForEach-Object { $_.occasion }); withActivity = $withAct.Count; withImage = $withImg.Count; titleInRequests = $leak.Count; titleOnDisk = $disk; greet = $r1.Text }
    if ($reqs.Count -eq 0) {
        Add-QaResult -Id 'AC-51' -Status FAIL -Message ('no AI request at all with default config (greet trigger: ' + $r1.Status + ' ' + $r1.Text + ') - cannot verify; AI should work out of the box with a key') -Evidence $ev
    } elseif ($withAct.Count -eq 0 -and $withImg.Count -eq 0 -and $leak.Count -eq 0 -and $disk.Count -eq 0) {
        Add-QaResult -Id 'AC-51' -Status PASS -Message ("default config: {0} AI request(s) (incl. screenshot_insight/app_switch), none with activity, image or the foreground title; title not stored on disk" -f $reqs.Count) -Evidence $ev
    } else {
        Add-QaResult -Id 'AC-51' -Status FAIL -Message ("default config leaks: activity in {0}, image in {1}, title in {2} request(s), title on disk: {3}" -f $withAct.Count, $withImg.Count, $leak.Count, ($disk -join ', ')) -Evidence $ev
    }
    if ($withImg.Count -eq 0) { Add-QaResult -Id 'AC-55' -Status PASS -Message 'default config: screenshot_insight sends no image' }
    else { Add-QaResult -Id 'AC-55' -Status FAIL -Message 'default config: an image was sent' }
} catch {
    Add-QaResult -Id 'AC-50' -Status FAIL -Message ('Test-Privacy-Defaults error: ' + $_.Exception.Message)
} finally {
    Stop-QaWindow $win
    Stop-QaStartedProcesses
    Write-QaSummary
}
