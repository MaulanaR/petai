<#
.SYNOPSIS
  Harness self-test: dry-runs the window-less QA scripts against qa\selftest\fakepet (a contract
  double of the app's debug API + AI wire format) - first a correct fake (expect PASS), then fakes
  with injected defects (expect the matching FAILs). No mouse/keyboard input, no foreground changes.
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File qa\selftest\Run-SelfTest.ps1
#>
[CmdletBinding()]
param(
    [string]$MockAddr = '127.0.0.1:47790',
    [string]$DebugAddr = '127.0.0.1:47691',
    [switch]$SkipBugRuns
)
$ErrorActionPreference = 'Stop'
$scripts = Join-Path (Split-Path -Parent $PSScriptRoot) 'scripts'
. (Join-Path $scripts 'QaCommon.ps1')
$root = Join-Path $script:QaArtifactsRoot ('selftest-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
$tmp = Join-Path (Get-QaTempRoot) (Split-Path -Leaf $root)   # fake data dirs off C:
New-Item -ItemType Directory -Force -Path $root | Out-Null
Initialize-QaContext -ScriptName 'Run-SelfTest' -ArtifactsDir $root
$ak = 'sk-test-qa-anthropic-FAKE-7d1e'; $ok = 'sk-test-qa-openai-FAKE-9c2b'

function Invoke-Child {
    param([string]$Name, [string]$ResultsFile, [hashtable]$Params)
    $argList = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', (Join-Path $scripts "$Name.ps1"), '-ResultsFile', $ResultsFile, '-ArtifactsDir', (Join-Path $root $Name))
    foreach ($k in $Params.Keys) { $v = $Params[$k]; if ($v -is [bool]) { if ($v) { $argList += "-$k" } } else { $argList += "-$k"; $argList += "$v" } }
    & powershell.exe @argList | Out-Host
}

function Get-Results { param([string]$File) if (-not (Test-Path $File)) { return @() }; return @(Get-Content $File | Where-Object { $_ } | ForEach-Object { ConvertFrom-Json -InputObject $_ }) }

function Invoke-FakeRun {
    param([string]$Label, [string]$Bugs = '', [switch]$WithPersistence)
    $data = Join-Path $tmp "data-$Label"
    $res = Join-Path $root "results-$Label.jsonl"
    $p = Start-PetApp -Exe $fake -DataDir $data -DebugAddr $DebugAddr -MockAddr $MockAddr -AnthropicKey $ak -OpenAIKey $ok -ExtraEnv @{ PETAI_FAKE_BUGS = $Bugs } -LogPrefix (Join-Path $root "fake-$Label-$([guid]::NewGuid().ToString().Substring(0,4))")
    if (-not (Wait-QaUntil -TimeoutMs 10000 -Condition { $null -ne (Get-PetState -DebugAddr $DebugAddr -TimeoutSec 1) })) { throw "fake ($Label) did not start" }
    $c = @{ DebugAddr = $DebugAddr; MockAddr = $MockAddr }
    Invoke-Child 'Test-Privacy-Defaults' $res ($c + @{ DataDir = $data; SkipWindows = $true })
    Invoke-Child 'Test-AI' $res ($c + @{ DataDir = $data; SkipWindows = $true; StateFile = (Join-Path $root "state-$Label.json"); AnthropicKey = $ak; OpenAIKey = $ok })
    if ($WithPersistence) {
        $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ movement = @{ mode = 'free' }; pet = @{ character = 'cat'; name = 'QaMochi' } }
        $null = Stop-PetApp -Process $p
        Start-Sleep -Seconds 1
        $p = Start-PetApp -Exe $fake -DataDir $data -DebugAddr $DebugAddr -MockAddr $MockAddr -AnthropicKey $ak -OpenAIKey $ok -ExtraEnv @{ PETAI_FAKE_BUGS = $Bugs } -LogPrefix (Join-Path $root "fake-$Label-$([guid]::NewGuid().ToString().Substring(0,4))")
        $null = Wait-QaUntil -TimeoutMs 10000 -Condition { $null -ne (Get-PetState -DebugAddr $DebugAddr -TimeoutSec 1) }
        Invoke-Child 'Test-Persistence' $res ($c + @{ DataDir = $data; StateFile = (Join-Path $root "state-$Label.json"); AnthropicKey = $ak; OpenAIKey = $ok })
    }
    $null = Stop-PetApp -Process $p
    return Get-Results $res
}

$mock = $null
try {
    $bin = Join-Path $tmp 'bin'; New-Item -ItemType Directory -Force -Path $bin | Out-Null
    Set-QaGoEnv
    Push-Location (Join-Path $script:QaRoot 'mockai'); & go build -buildvcs=false -o (Join-Path $bin 'mockai.exe') .; Pop-Location
    Push-Location (Join-Path $PSScriptRoot 'fakepet'); & go build -buildvcs=false -ldflags '-H=windowsgui' -o (Join-Path $bin 'petai-fake.exe') .; Pop-Location
    $fake = Join-Path $bin 'petai-fake.exe'
    $mock = Start-Process -FilePath (Join-Path $bin 'mockai.exe') -ArgumentList @('-addr', $MockAddr, '-log', (Join-Path $root 'mockai.jsonl'), '-anthropic-key-fp', (Get-QaFingerprint $ak), '-openai-key-fp', (Get-QaFingerprint $ok)) -PassThru -WindowStyle Hidden
    Register-QaPid -Process $mock -Role 'mockai'
    $null = Wait-QaUntil -TimeoutMs 5000 -Condition { $null -ne (Get-MockHealth -MockAddr $MockAddr) }

    $summary = @()
    Write-Host '=== correct fake: expect no FAIL' -ForegroundColor Cyan
    $r = Invoke-FakeRun -Label 'good' -WithPersistence
    $fails = @($r | Where-Object { $_.status -eq 'FAIL' })
    $summary += [pscustomobject]@{ Run = 'good'; Expect = 'no FAIL'; Got = ("{0} results, {1} FAIL: {2}" -f $r.Count, $fails.Count, (($fails | ForEach-Object { "$($_.id) $($_.message)" }) -join ' || ')); Ok = ($fails.Count -eq 0 -and $r.Count -gt 20) }

    if (-not $SkipBugRuns) {
        $cases = @(
            @{ Bugs = 'regen'; Expect = @('AC-81') },
            @{ Bugs = 'save_invalid'; Expect = @('AC-82') },
            @{ Bugs = 'no_memory'; Expect = @('AC-70') },
            @{ Bugs = 'image_always'; Expect = @('AC-51') },
            @{ Bugs = 'key_in_log'; Expect = @('AC-43') }
        )
        foreach ($c in $cases) {
            Write-Host "=== fake with bug '$($c.Bugs)': expect FAIL in $($c.Expect -join ',')" -ForegroundColor Cyan
            $r = Invoke-FakeRun -Label $c.Bugs -Bugs $c.Bugs
            $failIds = @($r | Where-Object { $_.status -eq 'FAIL' } | ForEach-Object { $_.id } | Select-Object -Unique)
            $missing = @($c.Expect | Where-Object { $failIds -notcontains $_ })
            $summary += [pscustomobject]@{ Run = $c.Bugs; Expect = ($c.Expect -join ','); Got = ('FAIL ids: ' + ($failIds -join ',')); Ok = ($missing.Count -eq 0) }
        }
    }
    Write-Host ''
    $summary | Format-Table -AutoSize -Wrap | Out-Host
    $summary | ConvertTo-Json -Depth 4 | Set-Content -Path (Join-Path $root 'selftest-summary.json') -Encoding UTF8
    if (@($summary | Where-Object { -not $_.Ok }).Count -eq 0) { Write-Host 'SELF-TEST OK' -ForegroundColor Green } else { Write-Host 'SELF-TEST FOUND HARNESS PROBLEMS' -ForegroundColor Red }
} finally {
    Stop-QaStartedProcesses
    if ($null -ne $mock -and -not $mock.HasExited) { $mock.Kill() }
    Remove-QaTempDir -Path $tmp
}
