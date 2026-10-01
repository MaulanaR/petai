<#
.SYNOPSIS
  Runs the whole PetAI acceptance suite and writes qa\REPORT.md.
.DESCRIPTION
  0. syntax-checks every qa\scripts\*.ps1, builds + unit-tests qa\mockai (offline, stdlib only)
  1. Test-Static (repo checks, go vet / go test)
  2. starts mockai (127.0.0.1 only) and launches build\bin\petai.exe several times, each with a
     temporary data dir under the run folder and test-only env vars (fake keys, mock base URLs,
     PETAI_FAST=1, PETAI_DEBUG_ADDR):
       A  fresh data dir, default capture exclusion  -> Test-Privacy-Defaults, Probe-Window, Test-SingleInstance
          (every launch first puts a QA window in front: the app must not steal focus, AC-29)
       B  fresh data dir, PETAI_EXCLUDE_CAPTURE=0     -> Probe-Window, Capture-Screen, Test-Autonomy, Test-Fullscreen,
                                                       Test-ClickThrough, Test-Interaction, Test-Characters,
                                                       Test-Wander, Test-Performance, Test-AI
       C  restart on B's data dir                     -> Test-Persistence
       D  data dir with a corrupt config.json         -> Test-Privacy-Defaults -CorruptConfig
  3. diffs HKCU Run / Startup folder / `cmdkey /list` (read-only) before vs after
  4. ALWAYS cleans up: stops only processes it started (PID + start time recorded), restores the cursor,
     copies config/db/animations/logs of each temp data dir into the run folder and deletes the temp dirs
     (temp data lives under D:\petai-build\qa-tmp when D: exists - C: is nearly full; override PETAI_QA_TMP)
  5. writes qa\REPORT.md (+ a copy in the run folder) with PASS/FAIL/WARN/SKIP per acceptance ID.
  The tests move the mouse, click and type (only into the pet overlay and QA windows of this
  harness). Do not use the PC while it runs (~20 min). Use -NoInput to skip input-driven tests.
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File qa\scripts\Run-Acceptance.ps1
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File qa\scripts\Run-Acceptance.ps1 -Only Test-Static,Test-AI
#>
[CmdletBinding()]
param(
    [string]$AppExe = '',
    [string]$MockAddr = '127.0.0.1:47700',
    [string]$DebugAddr = '127.0.0.1:47611',
    [string]$RunDir = '',
    [string]$Only = '',
    [string]$Skip = '',
    [switch]$NoInput,
    [switch]$NoGoTests,
    [int]$StartTimeoutSec = 60,
    [int]$CountdownSec = 5,
    [string]$AnthropicKey = 'sk-test-qa-anthropic-FAKE-7d1e',
    [string]$OpenAIKey = 'sk-test-qa-openai-FAKE-9c2b',
    [string]$ReportPath = ''
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
$ErrorActionPreference = 'Stop'

$qaRoot = $script:QaRoot
$repo = $script:QaRepoRoot
if (-not $AppExe) { $AppExe = Join-Path $repo 'build\bin\petai.exe' }
if (-not $RunDir) { $RunDir = Join-Path $script:QaArtifactsRoot ('run-' + (Get-Date -Format 'yyyyMMdd-HHmmss')) }
New-Item -ItemType Directory -Force -Path $RunDir | Out-Null
$RunDir = (Resolve-Path $RunDir).Path
$resultsFile = Join-Path $RunDir 'results.jsonl'
$pidReg = Join-Path $RunDir 'pids.txt'
New-Item -ItemType File -Force -Path $resultsFile | Out-Null
New-Item -ItemType File -Force -Path $pidReg | Out-Null
$tmpRun = Join-Path (Get-QaTempRoot) (Split-Path -Leaf $RunDir)   # big temp data (WebView2 caches) off C:
New-Item -ItemType Directory -Force -Path $tmpRun | Out-Null
$onlyList = @($Only.Split(',') | ForEach-Object { $_.Trim() } | Where-Object { $_ })
$skipList = @($Skip.Split(',') | ForEach-Object { $_.Trim() } | Where-Object { $_ })
$inputTests = @('Test-ClickThrough', 'Test-Interaction')
$startedAt = Get-Date
$timeline = New-Object System.Collections.ArrayList
$meta = [ordered]@{}

try { Start-Transcript -Path (Join-Path $RunDir 'orchestrator.log') -Force | Out-Null } catch { }
Initialize-QaContext -ScriptName 'Run-Acceptance' -ResultsFile $resultsFile -ArtifactsDir $RunDir -PidRegistry $pidReg
$null = Save-QaCursor

function Write-Phase { param([string]$Text) Write-Host ''; Write-Host ('#### ' + $Text) -ForegroundColor Magenta; [void]$timeline.Add(('{0}  {1}' -f (Get-Date -Format 'HH:mm:ss'), $Text)) }

function Test-Wanted {
    param([string]$Name)
    if ($onlyList.Count -gt 0 -and $onlyList -notcontains $Name) { return $false }
    if ($skipList -contains $Name) { return $false }
    if ($NoInput -and $inputTests -contains $Name) { return $false }
    return $true
}

function Invoke-QaScript {
    # Runs a test script in a child powershell.exe (isolation + timeout), streaming its output.
    param([string]$Name, [hashtable]$Params = @{}, [int]$TimeoutSec = 900, [string]$Tag = '')
    if (-not (Test-Wanted $Name)) { Write-Host "skip $Name"; return }
    $label = if ($Tag) { "$Name-$Tag" } else { $Name }
    $art = Join-Path $RunDir $label
    New-Item -ItemType Directory -Force -Path $art | Out-Null
    $argList = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', ('"{0}"' -f (Join-Path $PSScriptRoot "$Name.ps1")),
        '-ResultsFile', ('"{0}"' -f $resultsFile), '-ArtifactsDir', ('"{0}"' -f $art), '-PidRegistry', ('"{0}"' -f $pidReg))
    foreach ($k in $Params.Keys) {
        $v = $Params[$k]
        if ($v -is [bool] -or $v -is [System.Management.Automation.SwitchParameter]) { if ($v) { $argList += "-$k" } }
        else { $argList += "-$k"; $argList += ('"{0}"' -f $v) }
    }
    $out = Join-Path $RunDir "$label.out.txt"; $err = Join-Path $RunDir "$label.err.txt"
    Write-Phase "$label"
    $p = Start-Process -FilePath 'powershell.exe' -ArgumentList $argList -NoNewWindow -PassThru -RedirectStandardOutput $out -RedirectStandardError $err
    $null = $p.Handle
    $pos = 0L
    $sw = [Diagnostics.Stopwatch]::StartNew()
    while (-not $p.HasExited) {
        $pos = Show-NewOutput -Path $out -Pos $pos
        if ($sw.Elapsed.TotalSeconds -gt $TimeoutSec) {
            try { $p.Kill() } catch { }
            Add-QaResult -Id 'QA-00' -Status FAIL -Message "$label timed out after $TimeoutSec s (killed)"
            break
        }
        Start-Sleep -Milliseconds 500
    }
    $null = $p.WaitForExit(5000)
    $null = Show-NewOutput -Path $out -Pos $pos
    if ((Test-Path $err) -and (Get-Item $err).Length -gt 0) {
        $e = [IO.File]::ReadAllText($err).Trim()
        if ($e) { Write-Host $e -ForegroundColor Red; Add-QaResult -Id 'QA-00' -Status WARN -Message ("{0} wrote to stderr: {1}" -f $label, ($e.Substring(0, [Math]::Min(300, $e.Length)) -replace "`r?`n", ' ')) }
    }
    Restore-QaCursor
}

function Show-NewOutput {
    param([string]$Path, [long]$Pos)
    if (-not (Test-Path $Path)) { return $Pos }
    try {
        $fs = New-Object IO.FileStream($Path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite)
        try {
            if ($fs.Length -gt $Pos) {
                $null = $fs.Seek($Pos, [IO.SeekOrigin]::Begin)
                $buf = New-Object byte[] ($fs.Length - $Pos)
                $n = $fs.Read($buf, 0, $buf.Length)
                $Pos += $n
                $text = [Text.Encoding]::UTF8.GetString($buf, 0, $n)
                if ($text) { Write-Host -NoNewline $text }
            }
        } finally { $fs.Close() }
    } catch { }
    return $Pos
}

function Test-PortFree {
    param([string]$Addr)
    $hp = $Addr.Split(':')
    try {
        $l = New-Object System.Net.Sockets.TcpListener([System.Net.IPAddress]::Parse($hp[0]), [int]$hp[1])
        $l.Start(); $l.Stop(); return $true
    } catch { return $false }
}

function Get-AutostartSnapshot {
    $items = @()
    try {
        $run = Get-ItemProperty -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -ErrorAction Stop
        foreach ($pp in $run.PSObject.Properties) { if ($pp.Name -notlike 'PS*') { $items += ('Run:{0}={1}' -f $pp.Name, $pp.Value) } }
    } catch { }
    $startup = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\Startup'
    if (Test-Path $startup) { $items += @(Get-ChildItem -LiteralPath $startup -File | ForEach-Object { 'Startup:' + $_.Name }) }
    return $items
}

function Get-CredSnapshot {
    # Read-only listing of Credential Manager target names that mention PetAI.
    try {
        $lines = & cmdkey.exe /list 2>$null
        return @($lines | Where-Object { $_ -match '(?i)petai' } | ForEach-Object { $_.Trim() })
    } catch { return @() }
}

function Start-App {
    param([string]$Label, [string]$DataDir, [switch]$ExcludeCaptureOff)
    Write-Phase "launch $Label (data: $DataDir)"
    # Contract: launching must not steal keyboard focus. Put our own window in front first.
    $sentinel = $null
    if (-not $NoInput) {
        $sentinel = Start-QaWindow -Title "PetAI QA focus sentinel $Label" -X 120 -Y 120 -W 520 -H 220 -Color 'E8E8FF' -Foreground -Label 'PetAI QA: this window must keep the keyboard focus while the pet starts'
        Start-Sleep -Milliseconds 500
    }
    $fgBefore = Get-QaForeground
    $p = Start-PetApp -Exe $AppExe -DataDir $DataDir -DebugAddr $DebugAddr -MockAddr $MockAddr -AnthropicKey $AnthropicKey -OpenAIKey $OpenAIKey -ExcludeCaptureOff:$ExcludeCaptureOff -LogPrefix (Join-Path $RunDir "petai-$Label")
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $ready = Wait-PetReady -DebugAddr $DebugAddr -Process $p -TimeoutSec $StartTimeoutSec
    $meta["start$Label"] = @{ pid = $p.Id; ready = $ready; ms = $sw.ElapsedMilliseconds }
    if ($null -ne $sentinel) {
        Start-Sleep -Seconds 2
        $fgAfter = Get-QaForeground
        $stolen = ($null -eq $fgAfter -or $fgAfter.Hwnd -ne $sentinel.Hwnd)
        if ($ready -and $null -ne $fgBefore -and $fgBefore.Hwnd -eq $sentinel.Hwnd) {
            if ($stolen) { Add-QaResult -Id 'AC-29' -Status FAIL -Message ("launch {0}: focus moved from the window in front ('{1}') to '{2}' (pid {3})" -f $Label, $fgBefore.Title, $fgAfter.Title, $fgAfter.Pid) }
            else { Add-QaResult -Id 'AC-29' -Status PASS -Message ("launch {0}: the window in front kept the keyboard focus" -f $Label) }
        } elseif ($ready) {
            Add-QaResult -Id 'AC-29' -Status SKIP -Message "launch ${Label}: could not put the sentinel window in front before launching"
        }
        Stop-QaWindow $sentinel
    }
    if (-not $ready) {
        $why = if ($p.HasExited) { "process exited with code $($p.ExitCode)" } else { "no /debug/state + overlay within $StartTimeoutSec s" }
        Add-QaResult -Id 'QA-00' -Status FAIL -Message "launch $Label failed: $why (env PETAI_DEBUG_ADDR=$DebugAddr). See petai-$Label.*.txt"
        Stop-PetApp -Process $p | Out-Null
        return $null
    }
    Write-Host ("app $Label ready in {0} ms (pid {1})" -f $sw.ElapsedMilliseconds, $p.Id)
    Start-Sleep -Seconds 2
    return $p
}

function Stop-App {
    param($Process, [string]$Label)
    if ($null -eq $Process) { return }
    $how = Stop-PetApp -Process $Process
    Write-Host "stopped app $Label ($how)"
    $meta["stop$Label"] = $how
    Start-Sleep -Seconds 1
}

function Stop-Leftovers {
    # Kills processes recorded in the PID registry ONLY if PID + start time still match (never foreign processes).
    if (-not (Test-Path $pidReg)) { return }
    foreach ($line in Get-Content -LiteralPath $pidReg) {
        $parts = $line.Split('|')
        if ($parts.Count -lt 4) { continue }
        $procId = [int]$parts[0]; $start = [long]$parts[1]
        $p = Get-Process -Id $procId -ErrorAction SilentlyContinue
        if ($null -eq $p) { continue }
        try {
            if ($p.StartTime.ToFileTimeUtc() -eq $start) {
                if ($parts[3] -eq 'petai') { $null = Stop-PetApp -Process $p -GraceMs 3000 } else { $p.Kill() }
                Write-Host "cleanup: stopped leftover $($parts[2]) pid $procId ($($parts[3]))"
            }
        } catch { }
    }
}

$mockProc = $null
$app = $null
$autoBefore = @(); $credBefore = @()
try {
    Write-Host ("PetAI acceptance run -> {0}" -f $RunDir) -ForegroundColor Cyan

    # ---------------------------------------------------------------- 0. harness self-checks
    Write-Phase 'syntax check of qa\scripts'
    $synErr = @()
    foreach ($f in Get-ChildItem -LiteralPath $PSScriptRoot -Filter '*.ps1') {
        $tokens = $null; $errs = $null
        $null = [System.Management.Automation.Language.Parser]::ParseFile($f.FullName, [ref]$tokens, [ref]$errs)
        foreach ($e in @($errs)) { $synErr += ('{0}:{1}: {2}' -f $f.Name, $e.Extent.StartLineNumber, $e.Message) }
    }
    if ($synErr.Count -eq 0) { Add-QaResult -Id 'QA-00' -Status PASS -Message 'all harness scripts parse without errors' }
    else { Add-QaResult -Id 'QA-00' -Status FAIL -Message ('harness syntax errors: ' + ($synErr -join ' | ')) }

    Write-Phase 'build + unit-test qa\mockai (offline)'
    $binDir = Join-Path $tmpRun 'bin'   # per run (a running mock locks its exe), on the temp drive, deleted at cleanup
    New-Item -ItemType Directory -Force -Path $binDir | Out-Null
    $mockExe = Join-Path $binDir 'mockai.exe'
    $savedEnv = @{}
    foreach ($k in 'GOPROXY', 'GOTOOLCHAIN', 'GOFLAGS', 'GOCACHE', 'GOTMPDIR') { $savedEnv[$k] = [Environment]::GetEnvironmentVariable($k, 'Process') }
    try {
        Set-QaGoEnv
        $mockDir = Join-Path $qaRoot 'mockai'
        $b = Start-Process -FilePath 'go' -ArgumentList @('build', '-buildvcs=false', '-o', ('"{0}"' -f $mockExe), '.') -WorkingDirectory $mockDir -NoNewWindow -PassThru -RedirectStandardOutput (Join-Path $RunDir 'mockai-build.txt') -RedirectStandardError (Join-Path $RunDir 'mockai-build.err.txt')
        $null = $b.Handle; $null = $b.WaitForExit(300000)
        $t = Start-Process -FilePath 'go' -ArgumentList @('test', '-count=1', './...') -WorkingDirectory $mockDir -NoNewWindow -PassThru -RedirectStandardOutput (Join-Path $RunDir 'mockai-test.txt') -RedirectStandardError (Join-Path $RunDir 'mockai-test.err.txt')
        $null = $t.Handle; $null = $t.WaitForExit(300000)
    } finally {
        foreach ($k in $savedEnv.Keys) { [Environment]::SetEnvironmentVariable($k, $savedEnv[$k], 'Process') }
    }
    if (-not (Test-Path $mockExe)) {
        Add-QaResult -Id 'QA-00' -Status FAIL -Message ('mockai build failed: ' + [IO.File]::ReadAllText((Join-Path $RunDir 'mockai-build.err.txt')))
    } else {
        Add-QaResult -Id 'QA-00' -Status $(if ($t.ExitCode -eq 0) { 'PASS' } else { 'FAIL' }) -Message ("mockai built; go test exit {0}" -f $t.ExitCode)
    }

    # ---------------------------------------------------------------- 1. static
    Invoke-QaScript -Name 'Test-Static' -Params @{ RepoRoot = $repo; MockaiExe = $mockExe; NoGoTests = [bool]$NoGoTests } -TimeoutSec 1800

    # ---------------------------------------------------------------- 2. runtime
    $runtimeNames = @('Test-Privacy-Defaults', 'Probe-Window', 'Test-SingleInstance', 'Capture-Screen', 'Test-Autonomy', 'Test-Fullscreen',
        'Test-ClickThrough', 'Test-Interaction', 'Test-Characters', 'Test-Wander', 'Test-Performance', 'Test-AI', 'Test-Persistence')
    $anyRuntime = @($runtimeNames | Where-Object { Test-Wanted $_ }).Count -gt 0
    if (-not $anyRuntime) { Write-Host 'no runtime tests selected' }
    elseif (-not (Test-Path $AppExe)) {
        Add-QaResult -Id 'QA-00' -Status FAIL -Message "app executable not found: $AppExe (run 'wails build' first) - all runtime acceptance checks NOT RUN"
    } elseif (-not (Test-Path $mockExe)) {
        Add-QaResult -Id 'QA-00' -Status FAIL -Message 'mock AI server not built - runtime checks NOT RUN'
    } else {
        $foreign = @(Get-Process -Name ([IO.Path]::GetFileNameWithoutExtension($AppExe)) -ErrorAction SilentlyContinue)
        $busy = @($MockAddr, $DebugAddr | Where-Object { -not (Test-PortFree $_) })
        if ($foreign.Count -gt 0) {
            Add-QaResult -Id 'QA-00' -Status FAIL -Message ("a petai instance not started by this harness is running (pid {0}); its single-instance lock would block the test instance. Quit it from the tray and re-run. Runtime checks NOT RUN." -f (($foreign | ForEach-Object { $_.Id }) -join ','))
        } elseif ($busy.Count -gt 0) {
            Add-QaResult -Id 'QA-00' -Status FAIL -Message ('port(s) already in use: ' + ($busy -join ', ') + ' - pass -MockAddr/-DebugAddr. Runtime checks NOT RUN.')
        } else {
            $autoBefore = Get-AutostartSnapshot
            $credBefore = Get-CredSnapshot
            $meta['app'] = @{ path = $AppExe; size = (Get-Item $AppExe).Length; modified = (Get-Item $AppExe).LastWriteTime.ToString('s'); sha256 = (Get-FileHash -LiteralPath $AppExe -Algorithm SHA256).Hash.Substring(0, 16) }

            Write-Phase "start mockai on $MockAddr"
            $mockArgs = @('-addr', $MockAddr, '-log', ('"{0}"' -f (Join-Path $RunDir 'mockai.jsonl')),
                '-anthropic-key-fp', (Get-QaFingerprint $AnthropicKey), '-openai-key-fp', (Get-QaFingerprint $OpenAIKey))
            $mockProc = Start-Process -FilePath $mockExe -ArgumentList $mockArgs -PassThru -WindowStyle Hidden -RedirectStandardError (Join-Path $RunDir 'mockai.stderr.txt') -RedirectStandardOutput (Join-Path $RunDir 'mockai.stdout.txt')
            Register-QaPid -Process $mockProc -Role 'mockai'
            if (-not (Wait-QaUntil -TimeoutMs 10000 -Condition { $null -ne (Get-MockHealth -MockAddr $MockAddr) })) { throw "mockai did not come up on $MockAddr" }

            if (-not $NoInput -and $CountdownSec -gt 0) {
                Write-Host ''
                Write-Host 'The next ~20 minutes the harness moves the mouse, clicks and types (only into the pet and its own QA windows).' -ForegroundColor Yellow
                Write-Host 'Please do not touch mouse/keyboard. Ctrl+C aborts (cleanup still runs).' -ForegroundColor Yellow
                for ($i = $CountdownSec; $i -gt 0; $i--) { Write-Host -NoNewline "$i.. "; Start-Sleep -Seconds 1 }
                Write-Host ''
            }
            $common = @{ DebugAddr = $DebugAddr; MockAddr = $MockAddr }

            # ---- A: fresh install, default capture exclusion
            if (@('Test-Privacy-Defaults', 'Probe-Window', 'Test-SingleInstance') | Where-Object { Test-Wanted $_ }) {
                $dataA = Join-Path $tmpRun 'data-A'
                $app = Start-App -Label 'A' -DataDir $dataA
                if ($null -ne $app) {
                    Invoke-QaScript -Name 'Test-Privacy-Defaults' -Params ($common + @{ DataDir = $dataA; ProcessId = $app.Id }) -Tag 'A'
                    Invoke-QaScript -Name 'Probe-Window' -Params ($common + @{ ProcessId = $app.Id; ExpectExcludeCapture = $true; NoInput = [bool]$NoInput }) -Tag 'A'
                    Invoke-QaScript -Name 'Test-SingleInstance' -Params ($common + @{ AppExe = $AppExe; DataDir = $dataA; ProcessId = $app.Id; AnthropicKey = $AnthropicKey; OpenAIKey = $OpenAIKey })
                    Stop-App -Process $app -Label 'A'; $app = $null
                }
            }

            # ---- B: main session
            $dataB = Join-Path $tmpRun 'data-B'
            $stateFile = Join-Path $RunDir 'ai-state.json'
            $bTests = @('Probe-Window', 'Capture-Screen', 'Test-Autonomy', 'Test-Fullscreen', 'Test-ClickThrough', 'Test-Interaction', 'Test-Characters', 'Test-Wander', 'Test-Performance', 'Test-AI', 'Test-Persistence')
            if (@($bTests | Where-Object { Test-Wanted $_ }).Count -gt 0) {
                $app = Start-App -Label 'B' -DataDir $dataB -ExcludeCaptureOff
                if ($null -ne $app) {
                    $pp = $common + @{ ProcessId = $app.Id }
                    Invoke-QaScript -Name 'Probe-Window' -Params ($pp + @{ NoInput = [bool]$NoInput }) -Tag 'B'
                    Invoke-QaScript -Name 'Capture-Screen' -Params ($pp + @{ Backdrop = $true; Full = $true; Label = 'initial' })
                    Invoke-QaScript -Name 'Test-Autonomy' -Params $pp -TimeoutSec 300
                    Invoke-QaScript -Name 'Test-Fullscreen' -Params $pp -TimeoutSec 400
                    Invoke-QaScript -Name 'Test-ClickThrough' -Params $pp
                    Invoke-QaScript -Name 'Test-Interaction' -Params $pp
                    Invoke-QaScript -Name 'Test-Characters' -Params $pp
                    Invoke-QaScript -Name 'Test-Wander' -Params ($pp + @{ PerchSeconds = 30; NoInput = [bool]$NoInput }) -TimeoutSec 400
                    Invoke-QaScript -Name 'Test-Performance' -Params ($pp + @{ NoInput = [bool]$NoInput }) -TimeoutSec 300
                    Invoke-QaScript -Name 'Test-AI' -Params ($pp + @{ DataDir = $dataB; StateFile = $stateFile; AnthropicKey = $AnthropicKey; OpenAIKey = $OpenAIKey }) -TimeoutSec 2400
                    if (Test-Wanted 'Test-Persistence') {
                        try {
                            $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ movement = @{ mode = 'free' }; pet = @{ character = 'cat'; name = 'QaMochi' } } -SettleMs 2000
                        } catch { Add-QaResult -Id 'AC-95' -Status FAIL -Message ('could not set config before restart: ' + $_.Exception.Message) }
                    }
                    Stop-App -Process $app -Label 'B'; $app = $null

                    # ---- C: restart on the same data dir
                    if (Test-Wanted 'Test-Persistence') {
                        $app = Start-App -Label 'C' -DataDir $dataB -ExcludeCaptureOff
                        if ($null -ne $app) {
                            $extra = @((Join-Path $RunDir 'petai-B.stdout.txt'), (Join-Path $RunDir 'petai-B.stderr.txt'), (Join-Path $RunDir 'petai-A.stdout.txt'), (Join-Path $RunDir 'petai-A.stderr.txt')) -join ';'
                            Invoke-QaScript -Name 'Test-Persistence' -Params ($common + @{ ProcessId = $app.Id; DataDir = $dataB; StateFile = $stateFile; AnthropicKey = $AnthropicKey; OpenAIKey = $OpenAIKey; ExtraScanPaths = $extra })
                            Stop-App -Process $app -Label 'C'; $app = $null
                        }
                    }
                }
            }

            # ---- D: corrupt config
            if (Test-Wanted 'Test-Privacy-Defaults') {
                $dataD = Join-Path $tmpRun 'data-D'
                New-Item -ItemType Directory -Force -Path $dataD | Out-Null
                [IO.File]::WriteAllText((Join-Path $dataD 'config.json'), '{ "version": 1, "pet": { "character": "dragon", "name": ', (New-Object Text.UTF8Encoding($false)))
                $app = Start-App -Label 'D' -DataDir $dataD
                if ($null -ne $app) {
                    Invoke-QaScript -Name 'Test-Privacy-Defaults' -Params ($common + @{ DataDir = $dataD; ProcessId = $app.Id; CorruptConfig = $true }) -Tag 'D'
                    Stop-App -Process $app -Label 'D'; $app = $null
                } else {
                    Add-QaResult -Id 'AC-94' -Status FAIL -Message 'app did not start with a corrupt config.json'
                }
            }

            # ---- system-state diffs (read-only)
            $autoAfter = Get-AutostartSnapshot
            $newAuto = @($autoAfter | Where-Object { $autoBefore -notcontains $_ })
            if ($newAuto.Count -eq 0) { Add-QaResult -Id 'AC-92' -Status PASS -Message 'no autostart entry (HKCU Run / Startup folder) was created by running the app with default settings' }
            else { Add-QaResult -Id 'AC-92' -Status FAIL -Message ('autostart entries appeared: ' + ($newAuto -join ', ')) }
            $credAfter = Get-CredSnapshot
            $newCred = @($credAfter | Where-Object { $credBefore -notcontains $_ })
            if ($newCred.Count -eq 0) { Add-QaResult -Id 'AC-43' -Status PASS -Message 'no Credential Manager entry was written while keys came from PETAI_API_KEY_* (env override honoured)' }
            else { Add-QaResult -Id 'AC-43' -Status FAIL -Message ('Credential Manager entries appeared during the run (env test keys written to the keyring?): ' + ($newCred -join ', ')) }
        }
    }
} catch {
    Add-QaResult -Id 'QA-00' -Status FAIL -Message ('orchestrator error: ' + $_.Exception.Message + ' @ line ' + $_.InvocationInfo.ScriptLineNumber)
} finally {
    Write-Phase 'cleanup'
    if ($null -ne $app) { $null = Stop-PetApp -Process $app }
    if ($null -ne $mockProc) {
        try {
            $stats = Invoke-QaHttp -Url "http://$MockAddr/mock/stats" -TimeoutSec 3
            if ($stats.Ok) { $meta['mockStats'] = $stats.Json }
        } catch { }
        try { if (-not $mockProc.HasExited) { $mockProc.Kill() } } catch { }
    }
    Stop-Leftovers
    Stop-QaStartedProcesses
    Restore-QaCursor
    foreach ($d in @(Get-ChildItem -LiteralPath $tmpRun -Directory -ErrorAction SilentlyContinue)) {
        Copy-QaDataEvidence -DataDir $d.FullName -Dest (Join-Path $RunDir $d.Name)
    }
    Remove-QaTempDir -Path $tmpRun
    try { Stop-Transcript | Out-Null } catch { }
}

# ==================================================================== REPORT
function Get-Acceptance {
    $path = Join-Path $qaRoot 'ACCEPTANCE.md'
    $rows = @()
    if (-not (Test-Path $path)) { return $rows }
    foreach ($line in Get-Content -LiteralPath $path -Encoding UTF8) {
        if ($line -notmatch '^\|\s*(AC-\d+)\s*\|') { continue }
        $cells = @($line.Trim().Trim('|').Split('|') | ForEach-Object { $_.Trim() })
        if ($cells.Count -lt 5) { continue }
        $rows += [pscustomobject]@{ Id = $cells[0]; Expectation = $cells[1]; Pass = $cells[2]; Method = $cells[3]; Severity = $cells[$cells.Count - 1].ToLower() }
    }
    return $rows
}

function Get-Aggregate {
    param($Results, $Row)
    $st = @($Results | ForEach-Object { $_.status })
    if ($st -contains 'FAIL') { return 'FAIL' }
    if ($st -contains 'PASS') { if ($st -contains 'WARN') { return 'PASS*' } else { return 'PASS' } }
    if ($st -contains 'WARN') { return 'WARN' }
    if ($st -contains 'SKIP') { return 'SKIP' }
    if ($null -ne $Row -and $Row.Method -match '^\s*MANUAL') { return 'MANUAL' }
    return 'NOT RUN'
}

function Get-Rel {
    param([string]$Path)
    $prefix = $repo.TrimEnd('\') + '\'
    if ($Path -and $Path.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) { return $Path.Substring($prefix.Length) }
    return $Path
}

function ConvertTo-Cell { param([string]$s, [int]$Max = 220) $s = ($s -replace '\|', '/' -replace "`r?`n", ' '); if ($s.Length -gt $Max) { $s = $s.Substring(0, $Max) + '...' }; return $s }

$all = @()
if (Test-Path $resultsFile) {
    foreach ($l in Get-Content -LiteralPath $resultsFile -Encoding UTF8) { if ($l.Trim()) { try { $all += (ConvertFrom-Json -InputObject $l) } catch { } } }
}
$acc = @(Get-Acceptance)
$ids = @($acc | ForEach-Object { $_.Id })
foreach ($r in $all) { if ($r.id -like 'AC-*' -and $ids -notcontains $r.id) { $ids += $r.id } }
$sb = New-Object System.Text.StringBuilder
$null = $sb.AppendLine('# PetAI - Acceptance Report')
$null = $sb.AppendLine('')
$null = $sb.AppendLine(('Run: `{0}` | started {1} | duration {2:N0} min | host {3} | {4}' -f (Get-Rel $RunDir), $startedAt.ToString('yyyy-MM-dd HH:mm'), ((Get-Date) - $startedAt).TotalMinutes, $env:COMPUTERNAME, [Environment]::OSVersion.VersionString))
if ($meta.Contains('app')) { $null = $sb.AppendLine(('App: `{0}` ({1:N1} MB, built {2}, sha256 {3}...)' -f $meta.app.path, ($meta.app.size / 1MB), $meta.app.modified, $meta.app.sha256)) }
$mon = Get-QaPrimaryMonitor
$null = $sb.AppendLine(('Primary monitor: {0}x{1}, work area bottom {2}. Inputs injected: {3}.' -f $mon.W, $mon.H, $mon.WorkBottom, (-not $NoInput)))
$null = $sb.AppendLine('')

$rowsOut = @()
foreach ($id in $ids) {
    $row = $acc | Where-Object { $_.Id -eq $id } | Select-Object -First 1
    $res = @($all | Where-Object { $_.id -eq $id })
    $agg = Get-Aggregate -Results $res -Row $row
    $rowsOut += [pscustomobject]@{ Id = $id; Row = $row; Results = $res; Status = $agg; Severity = $(if ($row) { $row.Severity } else { '?' }) }
}
$counts = $rowsOut | Group-Object Status | ForEach-Object { '{0}: {1}' -f $_.Name, $_.Count }
$blockFail = @($rowsOut | Where-Object { $_.Severity -eq 'blocker' -and @('FAIL', 'NOT RUN') -contains $_.Status })
$majorFail = @($rowsOut | Where-Object { $_.Severity -eq 'major' -and @('FAIL', 'NOT RUN') -contains $_.Status })
$verdict = if ($blockFail.Count -gt 0) { 'NOT ACCEPTED - blocker expectations failed or were not verified' }
elseif ($majorFail.Count -gt 0) { 'NOT ACCEPTED - major expectations failed or were not verified' }
else { 'AUTOMATED CHECKS OK - complete MANUAL-CHECKLIST.md before accepting' }
$null = $sb.AppendLine("## Verdict: $verdict")
$null = $sb.AppendLine('')
$null = $sb.AppendLine('Totals: ' + ($counts -join ', ') + '. (PASS* = passed with warnings; MANUAL = see qa/MANUAL-CHECKLIST.md)')
$null = $sb.AppendLine('')
if ($blockFail.Count + $majorFail.Count -gt 0) {
    $null = $sb.AppendLine('### Failing / unverified blocker and major items')
    foreach ($x in @($blockFail) + @($majorFail)) {
        $msg = (@($x.Results | Where-Object { $_.status -eq 'FAIL' } | ForEach-Object { $_.message }) | Select-Object -First 2) -join ' // '
        if (-not $msg) { $msg = 'not run / no automated result' }
        $null = $sb.AppendLine(('- **{0}** ({1}) {2}: {3}' -f $x.Id, $x.Severity, (ConvertTo-Cell $x.Row.Expectation 90), (ConvertTo-Cell $msg 300)))
    }
    $null = $sb.AppendLine('')
}
$null = $sb.AppendLine('## Results per acceptance criterion')
$null = $sb.AppendLine('')
$null = $sb.AppendLine('| ID | Sev | Status | Expectation | Evidence (first messages) |')
$null = $sb.AppendLine('|---|---|---|---|---|')
foreach ($x in $rowsOut) {
    $msgs = @($x.Results | Sort-Object @{ Expression = { @{ FAIL = 0; WARN = 1; PASS = 2; SKIP = 3 }[$_.status] } } | ForEach-Object { '[{0}] {1}' -f $_.status, $_.message }) | Select-Object -First 3
    $exp = if ($x.Row) { $x.Row.Expectation } else { '' }
    $null = $sb.AppendLine(('| {0} | {1} | **{2}** | {3} | {4} |' -f $x.Id, $x.Severity, $x.Status, (ConvertTo-Cell $exp 110), (ConvertTo-Cell ($msgs -join ' / ') 420)))
}
$null = $sb.AppendLine('')
$harness = @($all | Where-Object { $_.id -eq 'QA-00' })
if ($harness.Count) {
    $null = $sb.AppendLine('## Harness / environment notes (QA-00)')
    foreach ($h in $harness) { $null = $sb.AppendLine(('- [{0}] {1}' -f $h.status, (ConvertTo-Cell $h.message 500))) }
    $null = $sb.AppendLine('')
}
$null = $sb.AppendLine('## Details')
foreach ($x in $rowsOut | Where-Object { $_.Results.Count -gt 0 }) {
    $null = $sb.AppendLine(('<details><summary>{0} - {1} ({2})</summary>' -f $x.Id, $x.Status, (ConvertTo-Cell $(if ($x.Row) { $x.Row.Expectation } else { '' }) 120)))
    $null = $sb.AppendLine('')
    foreach ($r in $x.Results) {
        $null = $sb.AppendLine(('- **{0}** `{1}` {2}' -f $r.status, $r.script, (ConvertTo-Cell $r.message 1000)))
        if ($null -ne $r.evidence) {
            $evj = ConvertTo-Json -InputObject $r.evidence -Depth 6 -Compress
            if ($evj.Length -gt 1500) { $evj = $evj.Substring(0, 1500) + '...' }
            $null = $sb.AppendLine('  ```json')
            $null = $sb.AppendLine('  ' + $evj)
            $null = $sb.AppendLine('  ```')
        }
    }
    $null = $sb.AppendLine('</details>')
    $null = $sb.AppendLine('')
}
$pngs = @(Get-ChildItem -LiteralPath $RunDir -Recurse -File -Filter '*.png' -ErrorAction SilentlyContinue)
if ($pngs.Count) {
    $null = $sb.AppendLine('## Screenshots for manual review')
    foreach ($p in $pngs) { $null = $sb.AppendLine(('- `{0}`' -f (Get-Rel $p.FullName))) }
    $null = $sb.AppendLine('')
}
$null = $sb.AppendLine('## Timeline')
foreach ($t in $timeline) { $null = $sb.AppendLine("- $t") }
if ($meta.Contains('mockStats')) { $null = $sb.AppendLine(''); $null = $sb.AppendLine('Mock AI stats (since last reset): `' + (ConvertTo-Json -InputObject $meta.mockStats -Depth 4 -Compress) + '`') }
$null = $sb.AppendLine('')
$null = $sb.AppendLine('Raw results: `' + (Get-Rel $resultsFile) + '`, mock request log: `mockai.jsonl` in the run folder (key fingerprints only).')

$report = $sb.ToString()
$enc = New-Object Text.UTF8Encoding($false)
if (-not $ReportPath) { $ReportPath = Join-Path $qaRoot 'REPORT.md' }
[IO.File]::WriteAllText($ReportPath, $report, $enc)
[IO.File]::WriteAllText((Join-Path $RunDir 'REPORT.md'), $report, $enc)
Write-Host ''
Write-Host ("Verdict: {0}" -f $verdict) -ForegroundColor $(if ($verdict -like 'NOT*') { 'Red' } else { 'Green' })
Write-Host ("Report: {0}" -f $ReportPath)
