<#
.SYNOPSIS
  Idle CPU / memory of petai and all its child processes (WebView2) (AC-91).
.DESCRIPTION
  Measures the CPU time of the whole process tree over -Seconds in mode "stay" (idle, AI off, no
  input) and then in mode "free", normalised to total machine capacity (all logical cores).
  Plan target: < ~3% idle. Also reports working set / private bytes. Config restored afterwards.
#>
[CmdletBinding()]
param(
    [string]$DebugAddr = '127.0.0.1:47611',
    [int]$ProcessId = 0,
    [int]$Seconds = 30,
    [double]$IdleCpuLimit = 3.0,
    [double]$FreeCpuWarn = 6.0,
    [double]$MemoryWarnMB = 600,
    [switch]$NoInput,
    [string]$ResultsFile = '',
    [string]$ArtifactsDir = '',
    [string]$PidRegistry = '',
    [string]$MockAddr = ''
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
Initialize-QaContext -ScriptName 'Test-Performance' -ResultsFile $ResultsFile -ArtifactsDir $ArtifactsDir -PidRegistry $PidRegistry

function Get-TreeCpu {
    param([int]$RootPid)
    $tree = @(Get-QaProcessTree -RootPid $RootPid)
    $t = @{}
    foreach ($p in $tree) {
        $gp = Get-Process -Id $p.ProcessId -ErrorAction SilentlyContinue
        if ($null -ne $gp) { try { $t[[int]$p.ProcessId] = @{ Cpu = $gp.TotalProcessorTime.TotalMilliseconds; Name = $gp.ProcessName; WS = $gp.WorkingSet64; Priv = $gp.PrivateMemorySize64 } } catch { } }
    }
    return $t
}

function Measure-Cpu {
    param([int]$RootPid, [int]$Sec)
    $a = Get-TreeCpu $RootPid
    $sw = [Diagnostics.Stopwatch]::StartNew()
    Start-Sleep -Seconds $Sec
    $b = Get-TreeCpu $RootPid
    $el = $sw.Elapsed.TotalMilliseconds
    $used = 0.0
    foreach ($k in $b.Keys) { $prev = if ($a.ContainsKey($k)) { $a[$k].Cpu } else { 0 }; $used += [Math]::Max(0, $b[$k].Cpu - $prev) }
    $cores = [Environment]::ProcessorCount
    $ws = 0; $priv = 0
    foreach ($k in $b.Keys) { $ws += $b[$k].WS; $priv += $b[$k].Priv }
    $names = ($b.Values | ForEach-Object { $_.Name } | Group-Object | ForEach-Object { '{0}x{1}' -f $_.Count, $_.Name }) -join ' '
    return [pscustomobject]@{ CpuPct = [Math]::Round(100.0 * $used / ($el * $cores), 2); CpuPctOneCore = [Math]::Round(100.0 * $used / $el, 1); Cores = $cores
        WorkingSetMB = [Math]::Round($ws / 1MB, 1); PrivateMB = [Math]::Round($priv / 1MB, 1); Processes = $names }
}

$orig = $null
try {
    $proc = Get-QaPetProcess -ProcessId $ProcessId
    $st = Get-PetState -DebugAddr $DebugAddr
    if ($null -eq $proc -or $null -eq $st) { Add-QaResult -Id 'AC-91' -Status FAIL -Message 'petai process or /debug/state not found'; return }
    $orig = $st.config
    if (-not $NoInput) {
        $null = Save-QaCursor
        $mon = Get-QaPrimaryMonitor
        Move-QaCursor ($mon.X + $mon.W - 3) ($mon.Y + 3)
    }
    $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ ai = @{ enabled = $false }; movement = @{ mode = 'stay' } } -SettleMs 5000
    Write-Host "measuring idle (stay) for $Seconds s ..."
    $idle = Measure-Cpu -RootPid $proc.Id -Sec $Seconds
    $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ movement = @{ mode = 'free' } } -SettleMs 2000
    Write-Host "measuring wandering (free) for $Seconds s ..."
    $free = Measure-Cpu -RootPid $proc.Id -Sec $Seconds
    $fps = Get-QaProp $st 'config.general.fps'
    $ev = @{ idle = $idle; free = $free; fpsSetting = $fps; note = 'CpuPct = share of the whole machine (all logical cores); CpuPctOneCore = share of one core' }
    Write-Host ("idle: {0}% ({1}% of one core), free: {2}%, WS {3} MB, private {4} MB, processes: {5}" -f $idle.CpuPct, $idle.CpuPctOneCore, $free.CpuPct, $free.WorkingSetMB, $free.PrivateMB, $free.Processes)
    if ($idle.CpuPct -le $IdleCpuLimit) { Add-QaResult -Id 'AC-91' -Status PASS -Message ("idle CPU {0}% of the machine (<= {1}%), {2}% of one core; free-mode {3}%" -f $idle.CpuPct, $IdleCpuLimit, $idle.CpuPctOneCore, $free.CpuPct) -Evidence $ev }
    else { Add-QaResult -Id 'AC-91' -Status FAIL -Message ("idle CPU {0}% of the machine > {1}% target ({2}% of one core)" -f $idle.CpuPct, $IdleCpuLimit, $idle.CpuPctOneCore) -Evidence $ev }
    if ($free.CpuPct -gt $FreeCpuWarn) { Add-QaResult -Id 'AC-91' -Status WARN -Message ("wandering (free) CPU {0}% > {1}%" -f $free.CpuPct, $FreeCpuWarn) -Evidence $ev }
    if ($free.WorkingSetMB -gt $MemoryWarnMB) { Add-QaResult -Id 'AC-91' -Status WARN -Message ("working set {0} MB > {1} MB for a desktop pet" -f $free.WorkingSetMB, $MemoryWarnMB) -Evidence $ev }
} catch {
    Add-QaResult -Id 'AC-91' -Status FAIL -Message ('Test-Performance error: ' + $_.Exception.Message)
} finally {
    if (-not $NoInput) { Restore-QaCursor }
    if ($null -ne $orig) {
        try { $null = Set-PetConfig -DebugAddr $DebugAddr -Partial @{ ai = @{ enabled = (Get-QaProp $orig 'ai.enabled' $true) }; movement = @{ mode = (Get-QaProp $orig 'movement.mode' 'ground') } } -SettleMs 0 } catch { }
    }
    Write-QaSummary
}
