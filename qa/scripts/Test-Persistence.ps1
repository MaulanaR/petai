<#
.SYNOPSIS
  After an app RESTART on the same data dir: settings, AI-made animations and memories survive,
  cached animations are reused without generation, and no secret ever reached the disk.
.DESCRIPTION
  AC-36 / AC-95  mode, character and name set before the restart are still active + in config.json
  AC-81          AI-made animations (from -StateFile) still listed; re-requesting them -> 0 animation_spec calls
  AC-71          memories (from -StateFile) still in /debug/memories
  AC-43 / AC-93  logs/petai.log exists; fake keys absent from the data dir, logs, extra paths and the
                 WebView2 profile folders that belong to petai
#>
[CmdletBinding()]
param(
    [string]$DebugAddr = '127.0.0.1:47611',
    [string]$MockAddr = '127.0.0.1:47700',
    [Parameter(Mandatory = $true)][string]$DataDir,
    [string]$StateFile = '',
    [string]$ExpectMode = 'free',
    [string]$ExpectCharacter = 'cat',
    [string]$ExpectName = 'QaMochi',
    [string]$AnthropicKey = 'sk-test-qa-anthropic-FAKE-7d1e',
    [string]$OpenAIKey = 'sk-test-qa-openai-FAKE-9c2b',
    [string]$ExtraScanPaths = '',
    [int]$ProcessId = 0,
    [string]$ResultsFile = '',
    [string]$ArtifactsDir = '',
    [string]$PidRegistry = ''
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
Initialize-QaContext -ScriptName 'Test-Persistence' -ResultsFile $ResultsFile -ArtifactsDir $ArtifactsDir -PidRegistry $PidRegistry

try {
    $st = Get-PetState -DebugAddr $DebugAddr
    if ($null -eq $st) { Add-QaResult -Id 'AC-95' -Status FAIL -Message '/debug/state not available after restart'; return }
    $saved = $null
    if ($StateFile -and (Test-Path $StateFile)) { $saved = ConvertFrom-Json -InputObject ([IO.File]::ReadAllText($StateFile)) }

    # ---- settings
    $cfgPath = Join-Path $DataDir 'config.json'
    $file = $null
    if (Test-Path $cfgPath) { try { $file = ConvertFrom-Json -InputObject ([IO.File]::ReadAllText($cfgPath)) } catch { } }
    $f = @()
    if ((Get-QaProp $st 'pet.mode') -ne $ExpectMode) { $f += "pet.mode=$(Get-QaProp $st 'pet.mode') (expected $ExpectMode)" }
    if ((Get-QaProp $st 'pet.character') -ne $ExpectCharacter) { $f += "pet.character=$(Get-QaProp $st 'pet.character') (expected $ExpectCharacter)" }
    if ((Get-QaProp $st 'config.pet.name') -ne $ExpectName) { $f += "config.pet.name=$(Get-QaProp $st 'config.pet.name') (expected $ExpectName)" }
    if ($null -eq $file) { $f += 'config.json missing or invalid' }
    else {
        if ((Get-QaProp $file 'movement.mode') -ne $ExpectMode) { $f += "config.json movement.mode=$(Get-QaProp $file 'movement.mode')" }
        if ((Get-QaProp $file 'pet.character') -ne $ExpectCharacter) { $f += "config.json pet.character=$(Get-QaProp $file 'pet.character')" }
    }
    if ($f.Count -eq 0) {
        Add-QaResult -Id 'AC-36' -Status PASS -Message "mode=$ExpectMode, character=$ExpectCharacter, name=$ExpectName survived the restart"
        Add-QaResult -Id 'AC-95' -Status PASS -Message 'settings persisted in config.json and re-applied on start'
    } else {
        Add-QaResult -Id 'AC-36' -Status FAIL -Message ('after restart: ' + ($f -join '; '))
        Add-QaResult -Id 'AC-95' -Status FAIL -Message ('after restart: ' + ($f -join '; '))
    }

    # ---- animations
    $anims = @(); if ($null -ne $saved) { $anims = @($saved.animations | Where-Object { $_ }) }
    if ($anims.Count -eq 0) {
        Add-QaResult -Id 'AC-81' -Status SKIP -Message 'no AI-made animation recorded by Test-AI (state file missing/empty)'
    } else {
        $list = @(Get-PetAnimations -DebugAddr $DebugAddr)
        foreach ($a in $anims) {
            $listed = @($list | Where-Object { $_.name -eq $a })
            $files = @(Get-ChildItem -LiteralPath (Join-Path $DataDir 'animations') -Recurse -File -Filter "$a.json" -ErrorAction SilentlyContinue)
            $null = Set-MockScenario -MockAddr $MockAddr -Scenario 'new_anim' -Options @{ animName = $a }
            $since = Get-MockLastSeq -MockAddr $MockAddr
            $r = Invoke-PetTrigger -DebugAddr $DebugAddr -Occasion 'user_click' -TimeoutSec 90
            Start-Sleep -Seconds 3
            $reqs = @(Get-MockAiRequests -MockAddr $MockAddr -Since $since)
            $spec = @($reqs | Where-Object { $_.taskTag -eq 'animation_spec' })
            $pa = @($reqs | Where-Object { $_.taskTag -eq 'pet_action' })
            $ev = @{ listed = $listed.Count; files = @($files | ForEach-Object { $_.FullName }); petActionCalls = $pa.Count; specCalls = $spec.Count; trigger = $r.Text }
            if ($listed.Count -gt 0 -and $files.Count -gt 0 -and $pa.Count -ge 1 -and $spec.Count -eq 0) {
                Add-QaResult -Id 'AC-81' -Status PASS -Message "after restart '$a' is still in the library and re-requesting it made 0 animation_spec calls" -Evidence $ev
            } else {
                Add-QaResult -Id 'AC-81' -Status FAIL -Message ("after restart '{0}': listed={1}, file={2}, pet_action calls={3}, animation_spec calls={4} (expected 0)" -f $a, $listed.Count, $files.Count, $pa.Count, $spec.Count) -Evidence $ev
            }
        }
        $null = Set-MockScenario -MockAddr $MockAddr -Scenario 'normal'
    }

    # ---- memories
    $mems = @(); if ($null -ne $saved) { $mems = @($saved.memories | Where-Object { $_ }) }
    if ($mems.Count -eq 0) {
        Add-QaResult -Id 'AC-71' -Status SKIP -Message 'no memory recorded by Test-AI (state file missing/empty)'
    } else {
        $cur = @(Get-PetMemories -DebugAddr $DebugAddr)
        $missing = @($mems | Where-Object { $m = $_; @($cur | Where-Object { "$($_.content)" -like "*$m*" }).Count -eq 0 })
        if ($missing.Count -eq 0) { Add-QaResult -Id 'AC-71' -Status PASS -Message ("{0} memory item(s) survived the restart" -f $mems.Count) }
        else { Add-QaResult -Id 'AC-71' -Status FAIL -Message ('memories lost after restart: ' + ($missing -join ' | ')) -Evidence @{ current = $cur } }
    }

    # ---- logs + secrets on disk
    $log = Join-Path $DataDir 'logs\petai.log'
    if ((Test-Path $log) -and (Get-Item $log).Length -gt 0) { Add-QaResult -Id 'AC-93' -Status PASS -Message ("log file present: {0} ({1} bytes)" -f $log, (Get-Item $log).Length) }
    else { Add-QaResult -Id 'AC-93' -Status FAIL -Message "contract log file missing or empty: $log" }

    $needles = @($AnthropicKey, $OpenAIKey, 'qa-anthropic-FAKE', 'qa-openai-FAKE')
    $roots = @($DataDir) + @($ExtraScanPaths.Split(';') | Where-Object { $_ -and (Test-Path $_) })
    foreach ($cand in @((Join-Path $env:APPDATA 'petai.exe'), (Join-Path $env:LOCALAPPDATA 'petai.exe'), (Join-Path $env:LOCALAPPDATA 'petai'))) {
        if (Test-Path $cand) { $roots += $cand }
    }
    $hits = @()
    foreach ($root in $roots) {
        foreach ($nd in $needles) {
            foreach ($h in @(Find-QaStringInFiles -Root $root -Needle $nd)) { $hits += "$h ('$($nd.Substring(0, [Math]::Min(12, $nd.Length)))...')" }
        }
    }
    if ($hits.Count -eq 0) { Add-QaResult -Id 'AC-43' -Status PASS -Message ("no API key (or fragment) in: " + ($roots -join '; ')) }
    else { Add-QaResult -Id 'AC-43' -Status FAIL -Message ('API key material found on disk: ' + (($hits | Select-Object -Unique) -join ', ')) }
    $logHits = @(); foreach ($nd in $needles) { if (Test-Path $log) { $logHits += @(Find-QaStringInFiles -Root (Split-Path $log) -Needle $nd) } }
    if ($logHits.Count -gt 0) { Add-QaResult -Id 'AC-93' -Status FAIL -Message 'API key material in logs' }
} catch {
    Add-QaResult -Id 'AC-95' -Status FAIL -Message ('Test-Persistence error: ' + $_.Exception.Message)
} finally {
    Write-QaSummary
}
