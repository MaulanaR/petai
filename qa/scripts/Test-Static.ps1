<#
.SYNOPSIS
  Static checks of the repository (no app run needed).
.DESCRIPTION
  AC-01 Go + Wails v2           AC-02 frontend = vanilla JS (deps only three (+vite), no TS/JSX/Vue/Svelte)
  AC-03 three.js used           AC-04 go vet ./... and go test ./... pass (offline: GOPROXY=off)
  AC-05 NSIS installer project / built installer
  AC-31 three character modules (blob, cat, chick)
  AC-43 keys: keyring library used, no key fields in config structs, frontend never talks to AI APIs
  AC-65 AI text rendering: innerHTML-style sinks listed for review
  AC-84 no eval / new Function / string timers in the frontend
  AC-85 the 13 built-in animations exist as DSL JSON and pass the contract validator (mockai -validate)
#>
[CmdletBinding()]
param(
    [string]$RepoRoot = '',
    [string]$MockaiExe = '',
    [switch]$NoGoTests,
    [int]$GoTimeoutSec = 900,
    [string]$ResultsFile = '',
    [string]$ArtifactsDir = '',
    [string]$PidRegistry = '',
    [string]$DebugAddr = '',
    [string]$MockAddr = ''
)
. (Join-Path $PSScriptRoot 'QaCommon.ps1')
Initialize-QaContext -ScriptName 'Test-Static' -ResultsFile $ResultsFile -ArtifactsDir $ArtifactsDir -PidRegistry $PidRegistry
if (-not $RepoRoot) { $RepoRoot = $script:QaRepoRoot }
$RepoRoot = (Resolve-Path $RepoRoot).Path
$fe = Join-Path $RepoRoot 'frontend'
$feSrc = Join-Path $fe 'src'
$excludeDirs = @('node_modules', 'dist', 'wailsjs', '.git', 'qa', 'build')

function Get-QaFilesByExt {
    # NOTE: Windows PowerShell 5.1 ignores -Include together with -LiteralPath, so filter by extension.
    param([string]$Root, [string[]]$Ext)
    if (-not (Test-Path $Root)) { return @() }
    return @(Get-ChildItem -LiteralPath $Root -Recurse -File -ErrorAction SilentlyContinue | Where-Object { $Ext -contains $_.Extension.ToLower() })
}

function Get-RepoFiles {
    param([string]$Root, [string[]]$Ext)
    if (-not (Test-Path $Root)) { return @() }
    return @(Get-QaFilesByExt -Root $Root -Ext $Ext | Where-Object {
            $rel = $_.FullName.Substring($RepoRoot.Length).ToLower()
            -not (@($excludeDirs | Where-Object { $rel -like "*\$_\*" }).Count -gt 0)
        })
}

function Invoke-Go {
    param([string[]]$GoArgs, [string]$Label)
    $out = Get-QaArtifactPath "go-$Label.txt"
    $saved = @{}
    foreach ($k in 'GOPROXY', 'GOTOOLCHAIN', 'GOFLAGS', 'GOCACHE', 'GOTMPDIR') { $saved[$k] = [Environment]::GetEnvironmentVariable($k, 'Process') }
    try {
        Set-QaGoEnv   # offline + build cache on D: (C: is nearly full)
        $p = Start-Process -FilePath 'go' -ArgumentList $GoArgs -WorkingDirectory $RepoRoot -NoNewWindow -PassThru -RedirectStandardOutput $out -RedirectStandardError "$out.err"
        $null = $p.Handle
        if (-not $p.WaitForExit($GoTimeoutSec * 1000)) { try { $p.Kill() } catch { }; return [pscustomobject]@{ Code = -1; Out = "timeout after $GoTimeoutSec s"; File = $out } }
        $text = ''
        if (Test-Path $out) { $text += [IO.File]::ReadAllText($out) }
        if (Test-Path "$out.err") { $text += [IO.File]::ReadAllText("$out.err") }
        [IO.File]::WriteAllText($out, $text)
        Remove-Item "$out.err" -ErrorAction SilentlyContinue
        return [pscustomobject]@{ Code = $p.ExitCode; Out = $text; File = $out }
    } finally {
        foreach ($k in $saved.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k], 'Process') }
    }
}

try {
    # ------------------------------------------------------------ AC-01 Go + Wails v2
    $gomod = Join-Path $RepoRoot 'go.mod'
    $f = @()
    if (-not (Test-Path $gomod)) { $f += 'go.mod missing' }
    else {
        $gm = [IO.File]::ReadAllText($gomod)
        if ($gm -notmatch '(?m)^\s*(require\s+)?github\.com/wailsapp/wails/v2\s+v2\.') { $f += 'go.mod does not require github.com/wailsapp/wails/v2' }
        if ($gm -match 'wailsapp/wails/v3') { $f += 'go.mod references wails v3' }
        $wv = [regex]::Match($gm, 'github\.com/wailsapp/wails/v2\s+(v[0-9.]+)').Groups[1].Value
    }
    $goFiles = Get-RepoFiles -Root $RepoRoot -Ext @('.go')
    $usesRun = @($goFiles | Select-String -Pattern 'wails\.Run\(' -List).Count -gt 0
    if (-not $usesRun) { $f += 'no wails.Run( call found' }
    if (-not (Test-Path (Join-Path $RepoRoot 'wails.json'))) { $f += 'wails.json missing' }
    if ($f.Count -eq 0) { Add-QaResult -Id 'AC-01' -Status PASS -Message ("Go module with wails/v2 {0}, wails.Run, wails.json" -f $wv) }
    else { Add-QaResult -Id 'AC-01' -Status FAIL -Message ($f -join '; ') }

    # ------------------------------------------------------------ AC-02 vanilla JS frontend
    $pkgPath = Join-Path $fe 'package.json'
    $f = @(); $deps = @(); $dev = @()
    if (-not (Test-Path $pkgPath)) { $f += 'frontend/package.json missing' }
    else {
        $pkg = ConvertFrom-Json -InputObject ([IO.File]::ReadAllText($pkgPath))
        if ($null -ne $pkg.dependencies) { $deps = @($pkg.dependencies.PSObject.Properties | ForEach-Object { $_.Name }) }
        if ($null -ne $pkg.devDependencies) { $dev = @($pkg.devDependencies.PSObject.Properties | ForEach-Object { $_.Name }) }
        $banned = 'react|react-dom|vue|svelte|@sveltejs|angular|@angular|preact|solid-js|lit|jquery|backbone|ember|alpinejs|htmx|next|nuxt|typescript|@babylonjs|pixi|phaser|tailwindcss|bootstrap'
        $badDeps = @(@($deps) + @($dev) | Where-Object { $_ -match "^($banned)(/|$)" })
        if ($badDeps.Count -gt 0) { $f += ('framework/TS packages: ' + ($badDeps -join ', ')) }
        $extraDeps = @($deps | Where-Object { $_ -ne 'three' })
        if ($extraDeps.Count -gt 0) { $f += ('runtime dependencies besides three: ' + ($extraDeps -join ', ')) }
        $extraDev = @($dev | Where-Object { @('vite', '@types/three') -notcontains $_ })
        if ($extraDev.Count -gt 0) { Add-QaResult -Id 'AC-02' -Status WARN -Message ('extra devDependencies (review): ' + ($extraDev -join ', ')) }
        if (@($deps) + @($dev) -notcontains 'three') { $f += 'three is not a dependency' }
    }
    $badSrc = @(Get-QaFilesByExt -Root $feSrc -Ext @('.ts', '.tsx', '.jsx', '.vue', '.svelte') | Where-Object { $_.Name -notlike '*.d.ts' })
    if ($badSrc.Count -gt 0) { $f += ('non-vanilla source files: ' + (($badSrc | Select-Object -First 5 | ForEach-Object { $_.Name }) -join ', ')) }
    if ($f.Count -eq 0) { Add-QaResult -Id 'AC-02' -Status PASS -Message ("frontend deps: [{0}] dev: [{1}]; only .js sources" -f ($deps -join ','), ($dev -join ',')) }
    else { Add-QaResult -Id 'AC-02' -Status FAIL -Message ($f -join '; ') }

    # ------------------------------------------------------------ AC-03 three.js
    $jsFiles = @(Get-QaFilesByExt -Root $feSrc -Ext @('.js', '.mjs'))
    $imports = @($jsFiles | Select-String -Pattern "from\s+['""]three(/[^'""]*)?['""]|import\s+\*\s+as\s+THREE")
    $renderer = @($jsFiles | Select-String -Pattern 'WebGLRenderer')
    $toon = @($jsFiles | Select-String -Pattern 'MeshToonMaterial')
    $ev = @{ importFiles = @($imports | ForEach-Object { $_.Filename } | Select-Object -Unique); rendererFiles = @($renderer | ForEach-Object { $_.Filename } | Select-Object -Unique); toonMaterial = ($toon.Count -gt 0) }
    if ($imports.Count -gt 0 -and $renderer.Count -gt 0) { Add-QaResult -Id 'AC-03' -Status PASS -Message ("three imported in {0} file(s), WebGLRenderer used; MeshToonMaterial (doodle/toon look): {1}" -f $ev.importFiles.Count, $ev.toonMaterial) -Evidence $ev }
    else { Add-QaResult -Id 'AC-03' -Status FAIL -Message ("three imports: {0}, WebGLRenderer uses: {1}" -f $imports.Count, $renderer.Count) -Evidence $ev }

    # ------------------------------------------------------------ AC-31 characters (static)
    $charDir = Join-Path $feSrc 'characters'
    $missing = @(); $found = @()
    foreach ($c in $script:QaCharacters) {
        if (Test-Path (Join-Path $charDir "$c.js")) { $found += "characters/$c.js" }
        else {
            $alt = @($jsFiles | Select-String -Pattern ("['""]{0}['""]" -f $c) -List)
            if ($alt.Count -gt 0) { $found += "$c (in $($alt[0].Filename))" } else { $missing += $c }
        }
    }
    if ($missing.Count -eq 0) { Add-QaResult -Id 'AC-31' -Status PASS -Message ('character modules: ' + ($found -join ', ')) }
    else { Add-QaResult -Id 'AC-31' -Status FAIL -Message ('no frontend code for character(s): ' + ($missing -join ', ')) }

    # ------------------------------------------------------------ AC-84 / AC-65 frontend code execution & HTML sinks
    $exec = @($jsFiles | Select-String -CaseSensitive -Pattern '\beval\s*\(|new\s+Function\s*\(|(?<![\w.])Function\s*\(|set(Timeout|Interval)\s*\(\s*[''"`]|import\s*\(\s*[^''"`\s)]')
    if ($exec.Count -eq 0) { Add-QaResult -Id 'AC-84' -Status PASS -Message 'no eval / new Function / string timers / dynamic import in frontend/src' }
    else { Add-QaResult -Id 'AC-84' -Status FAIL -Message ('code-execution sinks in the frontend: ' + (($exec | Select-Object -First 6 | ForEach-Object { '{0}:{1}' -f $_.Filename, $_.LineNumber }) -join ', ')) -Evidence @{ lines = @($exec | Select-Object -First 10 | ForEach-Object { $_.Line.Trim() }) } }
    $goExec = @($goFiles | Select-String -Pattern 'ExecJS\(|WindowExecJS|\.Eval\(')
    if ($goExec.Count -gt 0) { Add-QaResult -Id 'AC-84' -Status WARN -Message ('Go side executes JS (review that no AI text is interpolated): ' + (($goExec | Select-Object -First 5 | ForEach-Object { '{0}:{1}' -f $_.Filename, $_.LineNumber }) -join ', ')) }
    $sinks = @($jsFiles | Select-String -Pattern '\.innerHTML\s*[+]?=|\.outerHTML\s*=|insertAdjacentHTML|document\.write\(')
    if ($sinks.Count -eq 0) { Add-QaResult -Id 'AC-65' -Status PASS -Message 'no innerHTML/outerHTML/insertAdjacentHTML/document.write in frontend/src (AI text cannot be rendered as HTML)' }
    else { Add-QaResult -Id 'AC-65' -Status WARN -Message ("{0} HTML sink(s) - verify none receives AI/user text (run the 'xss' mock scenario, M-12): {1}" -f $sinks.Count, (($sinks | Select-Object -First 6 | ForEach-Object { '{0}:{1}' -f $_.Filename, $_.LineNumber }) -join ', ')) -Evidence @{ lines = @($sinks | Select-Object -First 12 | ForEach-Object { '{0}:{1}: {2}' -f $_.Filename, $_.LineNumber, $_.Line.Trim() }) } }

    # ------------------------------------------------------------ AC-43 key handling (heuristics)
    $f = @()
    $keyring = @($goFiles | Select-String -Pattern 'github\.com/zalando/go-keyring|github\.com/danieljoos/wincred|CredWriteW|CredReadW' -List)
    if ($keyring.Count -eq 0) { $f += 'no Credential Manager library/API used in Go code' }
    $tagHits = @($goFiles | Where-Object { $_.FullName -notmatch '\\internal\\secrets\\' -and $_.Name -notlike '*_test.go' } | Select-String -Pattern 'json:"[^"]*(?i:api_?key|apikey|secret|token)[^"]*"')
    $tagHits = @($tagHits | Where-Object { $_.Line -notmatch '(?i)max_?tokens|maxTokens|input_tokens|output_tokens|prompt_tokens|completion_tokens|tokens"' })
    if ($tagHits.Count -gt 0) { $f += ('struct fields that would serialise a key: ' + (($tagHits | Select-Object -First 4 | ForEach-Object { '{0}:{1}' -f $_.Filename, $_.LineNumber }) -join ', ')) }
    $feAi = @($jsFiles | Select-String -Pattern 'x-api-key|anthropic-version|[''"]?Authorization[''"]?\s*:|Bearer\s+[$`''"+]|fetch\s*\([^)]*api\.(anthropic|openai)\.com|XMLHttpRequest')
    if ($feAi.Count -gt 0) { $f += ('frontend talks to AI APIs directly / handles keys: ' + (($feAi | Select-Object -First 4 | ForEach-Object { '{0}:{1}' -f $_.Filename, $_.LineNumber }) -join ', ')) }
    $logKey = @($goFiles | Select-String -Pattern '(?i)(log|slog|fmt)\.\w*\(.*\b(apiKey|api_key|key)\b' | Where-Object { $_.Line -notmatch '(?i)mask|redact|fingerprint|hasKey|len\(' })
    if ($logKey.Count -gt 0) { Add-QaResult -Id 'AC-43' -Status WARN -Message ('log/print statements mentioning a key variable (review): ' + (($logKey | Select-Object -First 5 | ForEach-Object { '{0}:{1}' -f $_.Filename, $_.LineNumber }) -join ', ')) }
    if ($f.Count -eq 0) { Add-QaResult -Id 'AC-43' -Status PASS -Message ('static: keyring API used (' + (($keyring | ForEach-Object { $_.Filename } | Select-Object -Unique) -join ', ') + '); no key fields in serialised structs; frontend has no AI endpoints/keys') }
    else { Add-QaResult -Id 'AC-43' -Status FAIL -Message ('static: ' + ($f -join '; ')) }

    # ------------------------------------------------------------ AC-85 built-in animations
    $jsonFiles = Get-RepoFiles -Root $RepoRoot -Ext @('.json')
    $byName = @{}
    foreach ($jf in $jsonFiles) {
        try {
            $o = ConvertFrom-Json -InputObject ([IO.File]::ReadAllText($jf.FullName))
            if ($null -ne $o -and $o.PSObject.Properties['name'] -and $o.PSObject.Properties['tracks']) { $byName["$($o.name)"] = $jf.FullName }
        } catch { }
    }
    $missing = @($script:QaBuiltinAnimations | Where-Object { -not $byName.ContainsKey($_) })
    $files = @($script:QaBuiltinAnimations | Where-Object { $byName.ContainsKey($_) } | ForEach-Object { $byName[$_] })
    $f = @()
    if ($missing.Count -gt 0) { $f += ('built-in DSL files not found in the repo: ' + ($missing -join ', ')) }
    if ($files.Count -gt 0 -and $MockaiExe -and (Test-Path $MockaiExe)) {
        $vout = Get-QaArtifactPath 'builtin-validate.json'
        $p = Start-Process -FilePath $MockaiExe -ArgumentList (@('-validate') + @($files | ForEach-Object { '"{0}"' -f $_ })) -NoNewWindow -PassThru -RedirectStandardOutput $vout -RedirectStandardError "$vout.err"
        $null = $p.Handle; $null = $p.WaitForExit(60000)
        $vr = ConvertFrom-Json -InputObject ([IO.File]::ReadAllText($vout))
        foreach ($prop in $vr.PSObject.Properties) {
            $rep = $prop.Value
            if (-not $rep.valid) { $f += ("{0}: {1}" -f (Split-Path -Leaf $prop.Name), (@($rep.errors) -join ' | ')) }
            elseif (@($rep.clampViolations).Count -gt 0) { $f += ("{0}: out of clamp range: {1}" -f (Split-Path -Leaf $prop.Name), (@($rep.clampViolations) -join ' | ')) }
        }
        foreach ($fp in $files) {
            $o = ConvertFrom-Json -InputObject ([IO.File]::ReadAllText($fp))
            if ($o.target -ne 'generic') { $f += ("{0}: target '{1}' (contract: built-ins are generic)" -f (Split-Path -Leaf $fp), $o.target) }
        }
    } elseif ($files.Count -gt 0) { Add-QaResult -Id 'AC-85' -Status WARN -Message 'mockai.exe not available: built-in DSL files found but not validated' }
    if ($f.Count -eq 0) { Add-QaResult -Id 'AC-85' -Status PASS -Message ("all {0} built-ins present as DSL JSON and valid per the contract" -f $script:QaBuiltinAnimations.Count) -Evidence @{ files = $files } }
    else { Add-QaResult -Id 'AC-85' -Status FAIL -Message ($f -join '; ') -Evidence @{ files = $files } }

    # ------------------------------------------------------------ AC-05 installer
    $nsi = @(Get-ChildItem -LiteralPath (Join-Path $RepoRoot 'build\windows') -Recurse -File -Filter '*.nsi' -ErrorAction SilentlyContinue)
    $inst = @(Get-ChildItem -LiteralPath (Join-Path $RepoRoot 'build\bin') -File -Filter '*installer*.exe' -ErrorAction SilentlyContinue)
    if ($inst.Count -gt 0) { Add-QaResult -Id 'AC-05' -Status PASS -Message ("installer built: {0} ({1:N1} MB) - install/uninstall on a clean machine is manual (M-20)" -f $inst[0].Name, ($inst[0].Length / 1MB)) }
    elseif ($nsi.Count -gt 0) { Add-QaResult -Id 'AC-05' -Status FAIL -Message ("NSIS project present ({0}) but no installer in build/bin - run 'wails build -nsis'" -f $nsi[0].Name) }
    else { Add-QaResult -Id 'AC-05' -Status FAIL -Message "no NSIS installer project or installer ('wails build -nsis' never run)" }

    # ------------------------------------------------------------ AC-04 go vet / go test
    if (-not $NoGoTests) {
        $vet = Invoke-Go -GoArgs @('vet', './...') -Label 'vet'
        if ($vet.Code -eq 0) { Add-QaResult -Id 'AC-04' -Status PASS -Message 'go vet ./... clean' -Evidence @{ log = $vet.File } }
        else { Add-QaResult -Id 'AC-04' -Status FAIL -Message ("go vet ./... exit {0}: {1}" -f $vet.Code, (($vet.Out -split "`n" | Where-Object { $_ } | Select-Object -First 4) -join ' / ')) -Evidence @{ log = $vet.File } }
        $test = Invoke-Go -GoArgs @('test', '-count=1', './...') -Label 'test'
        $pkgs = @(($test.Out -split "`n") | Where-Object { $_ -match '^(ok|FAIL|---)\s' })
        $noTests = @(($test.Out -split "`n") | Where-Object { $_ -match 'no test files' }).Count
        $okPkgs = @($pkgs | Where-Object { $_ -match '^ok\s' }).Count
        if ($test.Code -eq 0) { Add-QaResult -Id 'AC-04' -Status PASS -Message ("go test ./... passed ({0} packages with tests, {1} without)" -f $okPkgs, $noTests) -Evidence @{ log = $test.File } }
        else { Add-QaResult -Id 'AC-04' -Status FAIL -Message ("go test ./... exit {0}: {1}" -f $test.Code, (($pkgs | Where-Object { $_ -notmatch '^ok' } | Select-Object -First 5) -join ' / ')) -Evidence @{ log = $test.File } }
        if ($test.Code -eq 0 -and $okPkgs -lt 3) { Add-QaResult -Id 'AC-04' -Status WARN -Message "only $okPkgs package(s) have tests (plan: validate, redact, engine, providers, memory ops)" }
    }
} catch {
    Add-QaResult -Id 'AC-01' -Status FAIL -Message ('Test-Static error: ' + $_.Exception.Message + ' @ line ' + $_.InvocationInfo.ScriptLineNumber)
} finally {
    Write-QaSummary
}
