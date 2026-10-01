# QaCommon.ps1 - shared helpers for the PetAI QA harness (PowerShell 5.1).
# Dot-source it:  . (Join-Path $PSScriptRoot 'QaCommon.ps1')
# Black-box only: talks to the app via Win32, the screen and the contract's debug API,
# and to the mock AI server via its /mock/* control endpoints.

$ErrorActionPreference = 'Stop'
Set-StrictMode -Off

$script:QaScriptsDir = $PSScriptRoot
$script:QaRoot = Split-Path -Parent $PSScriptRoot
$script:QaRepoRoot = Split-Path -Parent $script:QaRoot
$script:QaArtifactsRoot = Join-Path $script:QaRoot 'artifacts'

# Contract constants
$script:QaOverlayClass = 'wailsWindow'
$script:QaOverlayTitle = 'PetAI Overlay'
$script:QaBuiltinAnimations = @('idle','walk','float','sleep','jump','wave','happy_bounce','surprised','dangle','fall','land','sit','look_around')
$script:QaDefaultBlocklist = @('1password','bitwarden','keepass','inprivate','incognito','bank','bca','mandiri','bri','bni')
$script:QaCharacters = @('blob','cat','chick')
$script:QaModes = @('stay','ground','free')

# Win32 constants
$script:WS_EX_TOPMOST = 0x00000008
$script:WS_EX_TRANSPARENT = 0x00000020
$script:WS_EX_TOOLWINDOW = 0x00000080
$script:WS_EX_APPWINDOW = 0x00040000
$script:WS_EX_LAYERED = 0x00080000
$script:WS_EX_NOACTIVATE = 0x08000000
$script:WS_CAPTION = 0x00C00000
$script:WS_THICKFRAME = 0x00040000
$script:WS_POPUP = 0x80000000
$script:WS_VISIBLE = 0x10000000

# ------------------------------------------------------------------ disk / go environment
# C: on the dev machine is nearly full. Large temporary data (PETAI_DATA_DIRs incl. WebView2 caches)
# and the Go build cache go to D:\petai-build when a D: drive exists. Override with PETAI_QA_TMP.
function Get-QaTempRoot {
    $root = $env:PETAI_QA_TMP
    if (-not $root) {
        if (Test-Path 'D:\') { $root = 'D:\petai-build\qa-tmp' } else { $root = Join-Path $env:TEMP 'petai-qa' }
    }
    New-Item -ItemType Directory -Force -Path $root | Out-Null
    return $root
}

function Set-QaGoEnv {
    # Offline, local toolchain; build cache + temp on D: when available. Applies to this process and its children.
    [Environment]::SetEnvironmentVariable('GOPROXY', 'off', 'Process')
    [Environment]::SetEnvironmentVariable('GOTOOLCHAIN', 'local', 'Process')
    [Environment]::SetEnvironmentVariable('GOFLAGS', '', 'Process')
    if (Test-Path 'D:\') {
        foreach ($pair in @(@('GOCACHE', 'D:\petai-build\gocache'), @('GOTMPDIR', 'D:\petai-build\tmp'))) {
            $cur = [Environment]::GetEnvironmentVariable($pair[0], 'Process')
            if (-not $cur -or $cur -notlike 'D:*') {
                New-Item -ItemType Directory -Force -Path $pair[1] | Out-Null
                [Environment]::SetEnvironmentVariable($pair[0], $pair[1], 'Process')
            }
        }
    }
}

function Copy-QaDataEvidence {
    # Copies the small, interesting parts of a PETAI_DATA_DIR (config, db, animations, logs) - not WebView2 caches.
    param([string]$DataDir, [string]$Dest)
    if (-not (Test-Path $DataDir)) { return }
    New-Item -ItemType Directory -Force -Path $Dest | Out-Null
    foreach ($f in @('config.json', 'petai.db', 'petai.db-wal', 'petai.db-shm')) {
        $src = Join-Path $DataDir $f
        if (Test-Path $src) { Copy-Item -LiteralPath $src -Destination $Dest -Force -ErrorAction SilentlyContinue }
    }
    foreach ($d in @('animations', 'logs')) {
        $src = Join-Path $DataDir $d
        if (Test-Path $src) { Copy-Item -LiteralPath $src -Destination $Dest -Recurse -Force -ErrorAction SilentlyContinue }
    }
}

function Remove-QaTempDir {
    # Deletes a directory only if it lies under the QA temp root or qa\artifacts (never anything else).
    param([string]$Path)
    if (-not $Path -or -not (Test-Path $Path)) { return }
    $full = (Resolve-Path $Path).Path
    $allowed = @((Get-QaTempRoot), $script:QaArtifactsRoot) | ForEach-Object { (Resolve-Path $_).Path.TrimEnd('\') + '\' }
    if (@($allowed | Where-Object { $full.StartsWith($_, [StringComparison]::OrdinalIgnoreCase) }).Count -eq 0) {
        Write-Warning "refusing to delete $full (outside QA temp/artifacts)"
        return
    }
    Remove-Item -LiteralPath $full -Recurse -Force -ErrorAction SilentlyContinue
}

# ------------------------------------------------------------------ native helpers
function Initialize-QaNative {
    if ('PetQa.Native' -as [type]) { return }
    $src = @'
using System;
using System.Collections.Generic;
using System.Drawing;
using System.Drawing.Imaging;
using System.Runtime.InteropServices;
using System.Text;

namespace PetQa
{
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }
    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    public struct MONITORINFOEX
    {
        public int cbSize; public RECT rcMonitor; public RECT rcWork; public uint dwFlags;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 32)] public string szDevice;
    }
    [StructLayout(LayoutKind.Sequential)] public struct MOUSEINPUT { public int dx; public int dy; public uint mouseData; public uint dwFlags; public uint time; public IntPtr dwExtraInfo; }
    [StructLayout(LayoutKind.Sequential)] public struct KEYBDINPUT { public ushort wVk; public ushort wScan; public uint dwFlags; public uint time; public IntPtr dwExtraInfo; }
    [StructLayout(LayoutKind.Explicit)] public struct InputUnion { [FieldOffset(0)] public MOUSEINPUT mi; [FieldOffset(0)] public KEYBDINPUT ki; }
    [StructLayout(LayoutKind.Sequential)] public struct INPUT { public uint type; public InputUnion u; }

    public class WinInfo
    {
        public long Hwnd; public string ClassName; public string Title; public int Pid;
        public uint Style; public uint ExStyle; public int Left, Top, Width, Height;
        public bool Visible; public bool Iconic; public long Owner; public int Cloaked;
    }

    public static class Native
    {
        public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lParam);
        [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc cb, IntPtr lParam);
        [DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern int GetClassName(IntPtr h, StringBuilder sb, int n);
        [DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern int GetWindowText(IntPtr h, StringBuilder sb, int n);
        [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
        [DllImport("user32.dll", EntryPoint = "GetWindowLongPtrW")] public static extern IntPtr GetWindowLongPtr(IntPtr h, int idx);
        [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
        [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
        [DllImport("user32.dll")] public static extern bool IsIconic(IntPtr h);
        [DllImport("user32.dll")] public static extern bool IsWindow(IntPtr h);
        [DllImport("user32.dll")] public static extern IntPtr GetWindow(IntPtr h, uint cmd);
        [DllImport("user32.dll")] public static extern IntPtr WindowFromPoint(POINT p);
        [DllImport("user32.dll")] public static extern IntPtr GetAncestor(IntPtr h, uint flags);
        [DllImport("user32.dll")] public static extern bool GetCursorPos(out POINT p);
        [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
        [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
        [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
        [DllImport("user32.dll")] public static extern bool BringWindowToTop(IntPtr h);
        [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int cmd);
        [DllImport("user32.dll")] public static extern bool AttachThreadInput(uint a, uint b, bool attach);
        [DllImport("kernel32.dll")] public static extern uint GetCurrentThreadId();
        [DllImport("user32.dll")] public static extern IntPtr MonitorFromWindow(IntPtr h, uint flags);
        [DllImport("user32.dll")] public static extern IntPtr MonitorFromPoint(POINT p, uint flags);
        [DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern bool GetMonitorInfo(IntPtr hmon, ref MONITORINFOEX mi);
        [DllImport("user32.dll")] public static extern bool GetLayeredWindowAttributes(IntPtr h, out uint key, out byte alpha, out uint flags);
        [DllImport("user32.dll")] public static extern bool GetWindowDisplayAffinity(IntPtr h, out uint aff);
        [DllImport("dwmapi.dll")] public static extern int DwmGetWindowAttribute(IntPtr h, int attr, out int val, int size);
        [DllImport("user32.dll")] public static extern IntPtr SetThreadDpiAwarenessContext(IntPtr ctx);
        [DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr ctx);
        [DllImport("user32.dll")] public static extern IntPtr GetThreadDpiAwarenessContext();
        [DllImport("user32.dll")] public static extern int GetAwarenessFromDpiAwarenessContext(IntPtr ctx);
        [DllImport("user32.dll")] public static extern uint GetDpiForWindow(IntPtr h);
        [DllImport("user32.dll")] public static extern uint SendInput(uint n, INPUT[] inputs, int size);
        [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint msg, IntPtr w, IntPtr l);
        [DllImport("shell32.dll")] public static extern int SHQueryUserNotificationState(out int state);
        [DllImport("user32.dll")] public static extern uint GetDoubleClickTime();
        [DllImport("user32.dll")] public static extern int GetSystemMetrics(int idx);

        public const uint GA_ROOT = 2;
        public const uint GW_OWNER = 4;

        public static WinInfo Info(IntPtr h)
        {
            WinInfo w = new WinInfo();
            w.Hwnd = h.ToInt64();
            StringBuilder sb = new StringBuilder(512);
            GetClassName(h, sb, sb.Capacity); w.ClassName = sb.ToString();
            sb.Length = 0; GetWindowText(h, sb, sb.Capacity); w.Title = sb.ToString();
            uint pid; GetWindowThreadProcessId(h, out pid); w.Pid = (int)pid;
            w.Style = (uint)(GetWindowLongPtr(h, -16).ToInt64() & 0xFFFFFFFF);
            w.ExStyle = (uint)(GetWindowLongPtr(h, -20).ToInt64() & 0xFFFFFFFF);
            RECT r; GetWindowRect(h, out r);
            w.Left = r.Left; w.Top = r.Top; w.Width = r.Right - r.Left; w.Height = r.Bottom - r.Top;
            w.Visible = IsWindowVisible(h); w.Iconic = IsIconic(h);
            w.Owner = GetWindow(h, GW_OWNER).ToInt64();
            int cloaked = 0;
            try { DwmGetWindowAttribute(h, 14, out cloaked, 4); } catch (Exception) { }
            w.Cloaked = cloaked;
            return w;
        }

        public static List<WinInfo> FindWindows(string cls, string title, int pid)
        {
            List<WinInfo> res = new List<WinInfo>();
            EnumWindows(delegate(IntPtr h, IntPtr l)
            {
                WinInfo w = Info(h);
                if (!string.IsNullOrEmpty(cls) && w.ClassName != cls) return true;
                if (!string.IsNullOrEmpty(title) && w.Title != title) return true;
                if (pid > 0 && w.Pid != pid) return true;
                res.Add(w);
                return true;
            }, IntPtr.Zero);
            return res;
        }

        public static long RootWindowAt(int x, int y)
        {
            POINT p; p.X = x; p.Y = y;
            IntPtr h = WindowFromPoint(p);
            if (h == IntPtr.Zero) return 0;
            IntPtr root = GetAncestor(h, GA_ROOT);
            return (root == IntPtr.Zero ? h : root).ToInt64();
        }

        public static MONITORINFOEX MonitorOfWindow(IntPtr h)
        {
            IntPtr hm = MonitorFromWindow(h, 2);
            MONITORINFOEX mi = new MONITORINFOEX(); mi.cbSize = Marshal.SizeOf(typeof(MONITORINFOEX));
            GetMonitorInfo(hm, ref mi);
            return mi;
        }

        public static MONITORINFOEX MonitorAt(int x, int y)
        {
            POINT p; p.X = x; p.Y = y;
            IntPtr hm = MonitorFromPoint(p, 1);
            MONITORINFOEX mi = new MONITORINFOEX(); mi.cbSize = Marshal.SizeOf(typeof(MONITORINFOEX));
            GetMonitorInfo(hm, ref mi);
            return mi;
        }

        public static string EnableDpiAwareness()
        {
            IntPtr pmv2 = new IntPtr(-4);
            bool p = false;
            try { p = SetProcessDpiAwarenessContext(pmv2); } catch (Exception) { }
            try { SetThreadDpiAwarenessContext(pmv2); } catch (Exception) { }
            int aw = -1;
            try { aw = GetAwarenessFromDpiAwarenessContext(GetThreadDpiAwarenessContext()); } catch (Exception) { }
            return "process=" + p + " threadAwareness=" + aw;
        }

        static INPUT MouseIn(uint flags, int data)
        {
            INPUT i = new INPUT(); i.type = 0; i.u.mi.dwFlags = flags; i.u.mi.mouseData = (uint)data; return i;
        }
        static INPUT KeyIn(ushort vk, ushort scan, uint flags)
        {
            INPUT i = new INPUT(); i.type = 1; i.u.ki.wVk = vk; i.u.ki.wScan = scan; i.u.ki.dwFlags = flags; return i;
        }
        public static uint Mouse(uint flags)
        {
            INPUT[] a = new INPUT[] { MouseIn(flags, 0) };
            return SendInput(1, a, Marshal.SizeOf(typeof(INPUT)));
        }
        public static uint Key(ushort vk, bool up)
        {
            INPUT[] a = new INPUT[] { KeyIn(vk, 0, up ? 2u : 0u) };
            return SendInput(1, a, Marshal.SizeOf(typeof(INPUT)));
        }
        public static uint TypeText(string s)
        {
            uint n = 0;
            foreach (char c in s)
            {
                INPUT[] a = new INPUT[] { KeyIn(0, c, 4u), KeyIn(0, c, 4u | 2u) };
                n += SendInput(2, a, Marshal.SizeOf(typeof(INPUT)));
                System.Threading.Thread.Sleep(8);
            }
            return n;
        }

        public static bool ForceForeground(IntPtr h)
        {
            if (GetForegroundWindow() == h) return true;
            SetForegroundWindow(h);
            if (GetForegroundWindow() == h) return true;
            IntPtr fg = GetForegroundWindow();
            uint fgPid;
            uint fgThread = GetWindowThreadProcessId(fg, out fgPid);
            uint me = GetCurrentThreadId();
            bool attached = false;
            if (fgThread != 0 && fgThread != me) attached = AttachThreadInput(me, fgThread, true);
            BringWindowToTop(h);
            ShowWindow(h, 5);
            SetForegroundWindow(h);
            if (attached) AttachThreadInput(me, fgThread, false);
            return GetForegroundWindow() == h;
        }
    }

    public static class Img
    {
        public static Bitmap Capture(int x, int y, int w, int h)
        {
            Bitmap b = new Bitmap(Math.Max(1, w), Math.Max(1, h), PixelFormat.Format32bppArgb);
            using (Graphics g = Graphics.FromImage(b))
            {
                g.CopyFromScreen(x, y, 0, 0, new Size(w, h), CopyPixelOperation.SourceCopy);
            }
            return b;
        }

        static int[] Pixels(Bitmap b, Rectangle r)
        {
            r.Intersect(new Rectangle(0, 0, b.Width, b.Height));
            if (r.Width <= 0 || r.Height <= 0) return new int[0];
            BitmapData d = b.LockBits(r, ImageLockMode.ReadOnly, PixelFormat.Format32bppArgb);
            int[] px = new int[r.Width * r.Height];
            for (int row = 0; row < r.Height; row++)
                Marshal.Copy(new IntPtr(d.Scan0.ToInt64() + (long)row * d.Stride), px, row * r.Width, r.Width);
            b.UnlockBits(d);
            return px;
        }

        static bool Near(int argb, int r, int g, int bl, int tol)
        {
            int pr = (argb >> 16) & 255, pg = (argb >> 8) & 255, pb = argb & 255;
            return Math.Abs(pr - r) <= tol && Math.Abs(pg - g) <= tol && Math.Abs(pb - bl) <= tol;
        }

        // Fraction of pixels in rect that are NOT the given background colour.
        public static double ForegroundFraction(Bitmap b, Rectangle r, int bgR, int bgG, int bgB, int tol)
        {
            int[] px = Pixels(b, r);
            if (px.Length == 0) return 0;
            int n = 0;
            foreach (int p in px) if (!Near(p, bgR, bgG, bgB, tol)) n++;
            return (double)n / px.Length;
        }

        // n x n silhouette mask ('1' = non-background) of rect, nearest sampling.
        public static string Mask(Bitmap b, Rectangle r, int bgR, int bgG, int bgB, int tol, int n)
        {
            r.Intersect(new Rectangle(0, 0, b.Width, b.Height));
            StringBuilder sb = new StringBuilder(n * n);
            if (r.Width <= 0 || r.Height <= 0) return new string('0', n * n);
            int[] px = Pixels(b, r);
            for (int j = 0; j < n; j++)
                for (int i = 0; i < n; i++)
                {
                    int x = Math.Min(r.Width - 1, (int)((i + 0.5) * r.Width / n));
                    int y = Math.Min(r.Height - 1, (int)((j + 0.5) * r.Height / n));
                    sb.Append(Near(px[y * r.Width + x], bgR, bgG, bgB, tol) ? '0' : '1');
                }
            return sb.ToString();
        }

        public static double IoU(string a, string b)
        {
            int inter = 0, uni = 0;
            for (int i = 0; i < Math.Min(a.Length, b.Length); i++)
            {
                bool x = a[i] == '1', y = b[i] == '1';
                if (x && y) inter++;
                if (x || y) uni++;
            }
            return uni == 0 ? 1.0 : (double)inter / uni;
        }

        // Mean RGB of non-background pixels, "r,g,b".
        public static string MeanForegroundColor(Bitmap b, Rectangle r, int bgR, int bgG, int bgB, int tol)
        {
            int[] px = Pixels(b, r);
            long sr = 0, sg = 0, sbb = 0, n = 0;
            foreach (int p in px)
            {
                if (Near(p, bgR, bgG, bgB, tol)) continue;
                sr += (p >> 16) & 255; sg += (p >> 8) & 255; sbb += p & 255; n++;
            }
            if (n == 0) return "0,0,0";
            return (sr / n) + "," + (sg / n) + "," + (sbb / n);
        }
    }
}
'@
    Add-Type -TypeDefinition $src -Language CSharp -ReferencedAssemblies System.Drawing
}

function Enable-QaDpiAwareness {
    Initialize-QaNative
    return [PetQa.Native]::EnableDpiAwareness()
}

# ------------------------------------------------------------------ context & results
function Initialize-QaContext {
    param(
        [string]$ScriptName,
        [string]$ResultsFile = '',
        [string]$ArtifactsDir = '',
        [string]$PidRegistry = ''
    )
    Initialize-QaNative
    $dpi = Enable-QaDpiAwareness
    if (-not $ArtifactsDir) { $ArtifactsDir = Join-Path $script:QaArtifactsRoot ('adhoc-' + (Get-Date -Format 'yyyyMMdd-HHmmss')) }
    New-Item -ItemType Directory -Force -Path $ArtifactsDir | Out-Null
    $global:PetQa = @{
        ScriptName   = $ScriptName
        ResultsFile  = $ResultsFile
        ArtifactsDir = (Resolve-Path $ArtifactsDir).Path
        PidRegistry  = $PidRegistry
        Results      = New-Object System.Collections.ArrayList
        Started      = New-Object System.Collections.ArrayList
        Dpi          = $dpi
        SavedCursor  = $null
    }
    Write-Host ("== {0}  (dpi: {1}; artifacts: {2})" -f $ScriptName, $dpi, $global:PetQa.ArtifactsDir) -ForegroundColor Cyan
}

function Add-QaResult {
    param(
        [Parameter(Mandatory = $true)][string]$Id,
        [Parameter(Mandatory = $true)][ValidateSet('PASS', 'FAIL', 'SKIP', 'WARN')][string]$Status,
        [string]$Message = '',
        $Evidence = $null
    )
    $r = [pscustomobject]@{
        id = $Id; status = $Status; message = $Message; evidence = $Evidence
        script = $global:PetQa.ScriptName; time = (Get-Date).ToString('s')
    }
    [void]$global:PetQa.Results.Add($r)
    $color = @{ PASS = 'Green'; FAIL = 'Red'; SKIP = 'DarkYellow'; WARN = 'Yellow' }[$Status]
    Write-Host ("[{0}] {1,-6} {2}" -f $Status, $Id, $Message) -ForegroundColor $color
    if ($global:PetQa.ResultsFile) {
        $line = ConvertTo-Json -InputObject $r -Depth 10 -Compress
        [IO.File]::AppendAllText($global:PetQa.ResultsFile, $line + "`r`n", (New-Object Text.UTF8Encoding($false)))
    }
}

function Write-QaSummary {
    $res = @($global:PetQa.Results)
    $by = $res | Group-Object status | ForEach-Object { '{0}={1}' -f $_.Name, $_.Count }
    Write-Host ("== {0} done: {1}" -f $global:PetQa.ScriptName, ($by -join ' ')) -ForegroundColor Cyan
}

function Get-QaArtifactPath {
    param([string]$Name)
    return Join-Path $global:PetQa.ArtifactsDir $Name
}

function Write-QaArtifact {
    param([string]$Name, [string]$Text)
    $p = Get-QaArtifactPath $Name
    [IO.File]::WriteAllText($p, $Text, (New-Object Text.UTF8Encoding($false)))
    return $p
}

# ------------------------------------------------------------------ small utils
function Get-QaProp {
    param($Object, [string]$Path, $Default = $null)
    $cur = $Object
    foreach ($part in $Path.Split('.')) {
        if ($null -eq $cur) { return $Default }
        if ($cur -is [System.Collections.IDictionary]) {
            if (-not $cur.Contains($part)) { return $Default }
            $cur = $cur[$part]
        } else {
            $p = $cur.PSObject.Properties[$part]
            if ($null -eq $p) { return $Default }
            $cur = $p.Value
        }
    }
    return $cur
}

function Test-QaHasProp {
    param($Object, [string]$Name)
    if ($null -eq $Object) { return $false }
    if ($Object -is [System.Collections.IDictionary]) { return $Object.Contains($Name) }
    return ($null -ne $Object.PSObject.Properties[$Name])
}

function Wait-QaUntil {
    # Polls $Condition until truthy. Locals are prefixed to avoid shadowing caller variables
    # (the scriptblock is resolved through this function's scope).
    param([scriptblock]$Condition, [int]$TimeoutMs = 5000, [int]$IntervalMs = 200)
    $__qaWaitSw = [Diagnostics.Stopwatch]::StartNew()
    while ($__qaWaitSw.ElapsedMilliseconds -lt $TimeoutMs) {
        if (& $Condition) { return $true }
        Start-Sleep -Milliseconds $IntervalMs
    }
    return [bool](& $Condition)
}

function Get-QaFingerprint {
    param([string]$Key)
    if (-not $Key) { return '' }
    $sha = [Security.Cryptography.SHA256]::Create()
    $h = $sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($Key))
    return (($h | ForEach-Object { $_.ToString('x2') }) -join '').Substring(0, 8)
}

function New-QaToken {
    # Letters only: digit runs would be (correctly) redacted to [num] by the app.
    param([string]$Prefix = 'QATOK')
    $letters = 'ABCDEFGHJKLMNPQRSTUVWXYZ'
    $rnd = New-Object Random
    $s = -join (1..8 | ForEach-Object { $letters[$rnd.Next($letters.Length)] })
    return ('{0}{1}' -f $Prefix, $s)
}

function Format-QaJson {
    param($Object, [int]$Depth = 12)
    if ($null -eq $Object) { return 'null' }
    return (ConvertTo-Json -InputObject $Object -Depth $Depth -Compress)
}

# ------------------------------------------------------------------ HTTP
function Invoke-QaHttp {
    param(
        [string]$Method = 'GET',
        [Parameter(Mandatory = $true)][string]$Url,
        $Body = $null,
        [int]$TimeoutSec = 30
    )
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $res = [ordered]@{ Ok = $false; Status = 0; Text = ''; Json = $null; Ms = 0; Error = '' }
    $resp = $null
    try {
        $req = [System.Net.HttpWebRequest]::Create($Url)
        $req.Method = $Method
        $req.Proxy = $null
        $req.Timeout = $TimeoutSec * 1000
        $req.ReadWriteTimeout = $TimeoutSec * 1000
        $req.KeepAlive = $false
        if ($null -ne $Body) {
            $json = if ($Body -is [string]) { $Body } else { ConvertTo-Json -InputObject $Body -Depth 30 -Compress }
            $bytes = [Text.Encoding]::UTF8.GetBytes($json)
            $req.ContentType = 'application/json'
            $req.ContentLength = $bytes.Length
            $st = $req.GetRequestStream(); $st.Write($bytes, 0, $bytes.Length); $st.Close()
        } elseif ($Method -ne 'GET') {
            $req.ContentLength = 0
        }
        try {
            $resp = $req.GetResponse()
        } catch {
            $ex = $_.Exception
            while ($null -ne $ex -and -not ($ex -is [System.Net.WebException])) { $ex = $ex.InnerException }
            if ($null -ne $ex -and $null -ne $ex.Response) { $resp = $ex.Response } else { throw }
        }
        $res.Status = [int]$resp.StatusCode
        $sr = New-Object IO.StreamReader($resp.GetResponseStream(), [Text.Encoding]::UTF8)
        $res.Text = $sr.ReadToEnd()
        $sr.Close()
        if ($res.Text) {
            try { $res.Json = ConvertFrom-Json -InputObject $res.Text } catch { $res.Error = 'response is not JSON' }
        }
        $res.Ok = ($res.Status -ge 200 -and $res.Status -lt 300)
    } catch {
        $ex = $_.Exception
        while ($null -ne $ex.InnerException) { $ex = $ex.InnerException }
        $res.Error = $ex.Message
    } finally {
        if ($null -ne $resp) { try { $resp.Close() } catch { } }
    }
    $res.Ms = [int]$sw.ElapsedMilliseconds
    return [pscustomobject]$res
}

# ------------------------------------------------------------------ app debug API
function Get-PetBase { param([string]$DebugAddr) return "http://$DebugAddr" }

function Get-PetState {
    param([string]$DebugAddr, [int]$TimeoutSec = 5)
    $r = Invoke-QaHttp -Url ((Get-PetBase $DebugAddr) + '/debug/state') -TimeoutSec $TimeoutSec
    if ($r.Ok -and $null -ne $r.Json) { return $r.Json }
    return $null
}

function Test-PetAlive {
    param([string]$DebugAddr, [int]$TimeoutSec = 3)
    $r = Invoke-QaHttp -Url ((Get-PetBase $DebugAddr) + '/debug/state') -TimeoutSec $TimeoutSec
    return [pscustomobject]@{ Alive = ($r.Ok -and $null -ne $r.Json); Ms = $r.Ms; Error = $r.Error; Status = $r.Status }
}

function Set-PetConfig {
    param([string]$DebugAddr, $Partial, [int]$SettleMs = 400)
    $r = Invoke-QaHttp -Method POST -Url ((Get-PetBase $DebugAddr) + '/debug/config') -Body $Partial -TimeoutSec 15
    if (-not $r.Ok) { throw ("POST /debug/config failed: status={0} err={1} body={2}" -f $r.Status, $r.Error, $r.Text) }
    if ($SettleMs -gt 0) { Start-Sleep -Milliseconds $SettleMs }
    return $r.Json
}

function Get-PetConfig {
    param([string]$DebugAddr)
    $s = Get-PetState -DebugAddr $DebugAddr
    if ($null -eq $s) { throw 'GET /debug/state failed' }
    return $s.config
}

function Invoke-PetTrigger {
    param([string]$DebugAddr, [string]$Occasion, [int]$TimeoutSec = 90)
    return Invoke-QaHttp -Method POST -Url ((Get-PetBase $DebugAddr) + '/debug/trigger') -Body @{ occasion = $Occasion } -TimeoutSec $TimeoutSec
}

function Invoke-PetChat {
    param([string]$DebugAddr, [string]$Text, [int]$TimeoutSec = 90)
    return Invoke-QaHttp -Method POST -Url ((Get-PetBase $DebugAddr) + '/debug/chat') -Body @{ text = $Text } -TimeoutSec $TimeoutSec
}

function Invoke-PetPlay {
    param([string]$DebugAddr, [string]$Name)
    return Invoke-QaHttp -Method POST -Url ((Get-PetBase $DebugAddr) + '/debug/play') -Body @{ name = $Name } -TimeoutSec 15
}

function Invoke-PetUi {
    # POST /debug/ui {"open":"settings|chat|menu"} - opens the UI like tray / double-click / right-click would.
    param([string]$DebugAddr, [ValidateSet('settings', 'chat', 'menu')][string]$Open)
    return Invoke-QaHttp -Method POST -Url ((Get-PetBase $DebugAddr) + '/debug/ui') -Body @{ open = $Open } -TimeoutSec 15
}

function Get-PetAnimations {
    param([string]$DebugAddr)
    $r = Invoke-QaHttp -Url ((Get-PetBase $DebugAddr) + '/debug/animations') -TimeoutSec 10
    if (-not $r.Ok) { return $null }
    return @($r.Json)
}

function Get-PetMemories {
    param([string]$DebugAddr)
    $r = Invoke-QaHttp -Url ((Get-PetBase $DebugAddr) + '/debug/memories') -TimeoutSec 10
    if (-not $r.Ok) { return $null }
    return @($r.Json)
}

function Get-PetBox {
    # Pet bbox from /debug/state in physical pixels, with centre & bottom.
    param($State)
    if ($null -eq $State) { return $null }
    $p = $State.pet
    if ($null -eq $p) { return $null }
    $x = [double]$p.x; $y = [double]$p.y; $w = [double]$p.w; $h = [double]$p.h
    return [pscustomobject]@{ X = $x; Y = $y; W = $w; H = $h; CX = $x + $w / 2; CY = $y + $h / 2; Bottom = $y + $h; Right = $x + $w
        State = $p.state; Animation = $p.animation; Mode = $p.mode; Character = $p.character; Visible = $p.visible }
}

# ------------------------------------------------------------------ mock AI control
function Get-MockBase { param([string]$MockAddr) return "http://$MockAddr" }

function Get-MockHealth {
    param([string]$MockAddr)
    $r = Invoke-QaHttp -Url ((Get-MockBase $MockAddr) + '/mock/health') -TimeoutSec 5
    if ($r.Ok) { return $r.Json }
    return $null
}

function Reset-Mock {
    param([string]$MockAddr, [string]$Scenario = '', [hashtable]$Options = @{})
    $body = @{}
    foreach ($k in $Options.Keys) { $body[$k] = $Options[$k] }
    if ($Scenario) { $body['scenario'] = $Scenario }
    $r = Invoke-QaHttp -Method POST -Url ((Get-MockBase $MockAddr) + '/mock/reset') -Body $body -TimeoutSec 10
    if (-not $r.Ok) { throw "mock reset failed: $($r.Status) $($r.Error) $($r.Text)" }
    return $r.Json
}

function Set-MockScenario {
    param([string]$MockAddr, [string]$Scenario, [hashtable]$Options = @{})
    $body = @{ scenario = $Scenario }
    foreach ($k in $Options.Keys) { $body[$k] = $Options[$k] }
    $r = Invoke-QaHttp -Method POST -Url ((Get-MockBase $MockAddr) + '/mock/scenario') -Body $body -TimeoutSec 10
    if (-not $r.Ok) { throw "mock scenario failed: $($r.Status) $($r.Text)" }
    return $r.Json
}

function Get-MockRequests {
    param([string]$MockAddr, [string]$Provider = '', [string]$Task = '', [string]$Endpoint = '', [long]$Since = 0, [switch]$Full)
    $q = @()
    if ($Provider) { $q += "provider=$Provider" }
    if ($Task) { $q += "task=$Task" }
    if ($Endpoint) { $q += "endpoint=$Endpoint" }
    if ($Since -gt 0) { $q += "since=$Since" }
    if (-not $Full) { $q += 'brief=1' }
    $url = (Get-MockBase $MockAddr) + '/mock/requests'
    if ($q.Count) { $url += '?' + ($q -join '&') }
    $r = Invoke-QaHttp -Url $url -TimeoutSec 20
    if (-not $r.Ok) { throw "mock requests failed: $($r.Status) $($r.Error)" }
    return @($r.Json | Where-Object { $null -ne $_ })
}

function Get-MockAiRequests {
    # Only completion calls (messages / chat_completions), not models/count_tokens.
    param([string]$MockAddr, [long]$Since = 0, [string]$Provider = '')
    return @(Get-MockRequests -MockAddr $MockAddr -Since $Since -Provider $Provider | Where-Object { $_.endpoint -eq 'messages' -or $_.endpoint -eq 'chat_completions' })
}

function Search-Mock {
    param([string]$MockAddr, [string[]]$Needles, [long]$Since = 0, [string]$Provider = '')
    $r = Invoke-QaHttp -Method POST -Url ((Get-MockBase $MockAddr) + '/mock/search') -Body @{ needles = @($Needles); since = $Since; provider = $Provider } -TimeoutSec 20
    if (-not $r.Ok) { throw "mock search failed: $($r.Status) $($r.Error)" }
    $out = @{}
    foreach ($n in $Needles) {
        $v = Get-QaProp $r.Json.results $n
        $out[$n] = @($v | Where-Object { $null -ne $_ })
    }
    return $out
}

function Test-MockSpec {
    param([string]$MockAddr, [string]$JsonText)
    $r = Invoke-QaHttp -Method POST -Url ((Get-MockBase $MockAddr) + '/mock/validate') -Body $JsonText -TimeoutSec 10
    return $r.Json
}

function Get-MockLastSeq {
    param([string]$MockAddr)
    $h = Get-MockHealth -MockAddr $MockAddr
    if ($null -eq $h) { return 0 }
    return [long]$h.lastSeq
}

# ------------------------------------------------------------------ windows
function Get-QaWindowReport {
    param([long]$Hwnd)
    Initialize-QaNative
    $w = [PetQa.Native]::Info([IntPtr]$Hwnd)
    $ex = [uint32]$w.ExStyle
    $st = [uint32]$w.Style
    $mi = [PetQa.Native]::MonitorOfWindow([IntPtr]$Hwnd)
    $aff = [uint32]0
    $null = [PetQa.Native]::GetWindowDisplayAffinity([IntPtr]$Hwnd, [ref]$aff)
    $key = [uint32]0; $alpha = [byte]0; $lflags = [uint32]0
    $hasLayeredAttr = [PetQa.Native]::GetLayeredWindowAttributes([IntPtr]$Hwnd, [ref]$key, [ref]$alpha, [ref]$lflags)
    $taskbar = $w.Visible -and ($w.Owner -eq 0) -and ((($ex -band $script:WS_EX_APPWINDOW) -ne 0) -or (($ex -band $script:WS_EX_TOOLWINDOW) -eq 0))
    return [pscustomobject]@{
        Hwnd = $w.Hwnd; ClassName = $w.ClassName; Title = $w.Title; Pid = $w.Pid
        Style = ('0x{0:X8}' -f $st); ExStyle = ('0x{0:X8}' -f $ex); ExStyleValue = $ex
        TOPMOST = (($ex -band $script:WS_EX_TOPMOST) -ne 0)
        TOOLWINDOW = (($ex -band $script:WS_EX_TOOLWINDOW) -ne 0)
        APPWINDOW = (($ex -band $script:WS_EX_APPWINDOW) -ne 0)
        NOACTIVATE = (($ex -band $script:WS_EX_NOACTIVATE) -ne 0)
        LAYERED = (($ex -band $script:WS_EX_LAYERED) -ne 0)
        TRANSPARENT = (($ex -band $script:WS_EX_TRANSPARENT) -ne 0)
        HasCaption = (($st -band $script:WS_CAPTION) -eq $script:WS_CAPTION)
        HasThickFrame = (($st -band $script:WS_THICKFRAME) -ne 0)
        Visible = $w.Visible; Iconic = $w.Iconic; Cloaked = $w.Cloaked; Owner = $w.Owner
        Rect = [pscustomobject]@{ X = $w.Left; Y = $w.Top; W = $w.Width; H = $w.Height }
        Monitor = [pscustomobject]@{ X = $mi.rcMonitor.Left; Y = $mi.rcMonitor.Top; W = ($mi.rcMonitor.Right - $mi.rcMonitor.Left); H = ($mi.rcMonitor.Bottom - $mi.rcMonitor.Top); Primary = (($mi.dwFlags -band 1) -ne 0); Device = $mi.szDevice }
        Work = [pscustomobject]@{ X = $mi.rcWork.Left; Y = $mi.rcWork.Top; W = ($mi.rcWork.Right - $mi.rcWork.Left); H = ($mi.rcWork.Bottom - $mi.rcWork.Top); Bottom = $mi.rcWork.Bottom; Right = $mi.rcWork.Right }
        DisplayAffinity = ('0x{0:X}' -f $aff); DisplayAffinityValue = $aff
        LayeredAttr = if ($hasLayeredAttr) { 'alpha={0} key=0x{1:X} flags=0x{2:X}' -f $alpha, $key, $lflags } else { 'n/a' }
        Dpi = [PetQa.Native]::GetDpiForWindow([IntPtr]$Hwnd)
        HasTaskbarButton = $taskbar
    }
}

function Find-PetOverlay {
    # Returns overlay window reports (class wailsWindow, title "PetAI Overlay"), optionally for one PID.
    param([int]$ProcessId = 0)
    Initialize-QaNative
    $wins = [PetQa.Native]::FindWindows($script:QaOverlayClass, $script:QaOverlayTitle, $ProcessId)
    return @($wins | ForEach-Object { Get-QaWindowReport -Hwnd $_.Hwnd })
}

function Get-QaProcessWindows {
    param([int]$ProcessId)
    Initialize-QaNative
    return @([PetQa.Native]::FindWindows('', '', $ProcessId) | ForEach-Object { Get-QaWindowReport -Hwnd $_.Hwnd })
}

function Get-QaForeground {
    Initialize-QaNative
    $h = [PetQa.Native]::GetForegroundWindow()
    if ($h -eq [IntPtr]::Zero) { return $null }
    $w = [PetQa.Native]::Info($h)
    return [pscustomobject]@{ Hwnd = $w.Hwnd; Title = $w.Title; ClassName = $w.ClassName; Pid = $w.Pid; X = $w.Left; Y = $w.Top; W = $w.Width; H = $w.Height }
}

function Get-QaRootWindowAt {
    param([int]$X, [int]$Y)
    Initialize-QaNative
    return [PetQa.Native]::RootWindowAt($X, $Y)
}

function Get-QaNotificationState {
    # 1 NOT_PRESENT 2 BUSY 3 RUNNING_D3D_FULL_SCREEN 4 PRESENTATION_MODE 5 ACCEPTS_NOTIFICATIONS 6 QUIET_TIME 7 APP
    Initialize-QaNative
    $s = 0
    $null = [PetQa.Native]::SHQueryUserNotificationState([ref]$s)
    return $s
}

function Get-QaPrimaryMonitor {
    Initialize-QaNative
    $mi = [PetQa.Native]::MonitorAt(0, 0)
    return [pscustomobject]@{
        X = $mi.rcMonitor.Left; Y = $mi.rcMonitor.Top; W = ($mi.rcMonitor.Right - $mi.rcMonitor.Left); H = ($mi.rcMonitor.Bottom - $mi.rcMonitor.Top)
        Right = $mi.rcMonitor.Right; Bottom = $mi.rcMonitor.Bottom
        WorkX = $mi.rcWork.Left; WorkY = $mi.rcWork.Top; WorkRight = $mi.rcWork.Right; WorkBottom = $mi.rcWork.Bottom
    }
}

# ------------------------------------------------------------------ input (cursor / mouse / keyboard)
function Save-QaCursor {
    Initialize-QaNative
    $p = New-Object PetQa.POINT
    $null = [PetQa.Native]::GetCursorPos([ref]$p)
    $global:PetQa.SavedCursor = $p
    return $p
}

function Restore-QaCursor {
    if ($null -ne $global:PetQa -and $null -ne $global:PetQa.SavedCursor) {
        $p = $global:PetQa.SavedCursor
        $null = [PetQa.Native]::SetCursorPos($p.X, $p.Y)
    }
}

function Move-QaCursor {
    param([double]$X, [double]$Y)
    $null = [PetQa.Native]::SetCursorPos([int][Math]::Round($X), [int][Math]::Round($Y))
    $null = [PetQa.Native]::Mouse(0x0001)  # MOUSEEVENTF_MOVE (0,0) -> generates WM_MOUSEMOVE
}

function Set-QaClickAllow {
    # Safety guard: once set, injected clicks/drags only happen when the top-level window under the
    # point belongs to one of these PIDs (the pet) or to a process this script started (QA windows).
    param([int[]]$Pids)
    $global:PetQa.ClickAllowPids = @($Pids)
}

function Test-QaClickTarget {
    param([double]$X, [double]$Y)
    if ($null -eq $global:PetQa -or -not $global:PetQa.ContainsKey('ClickAllowPids') -or @($global:PetQa.ClickAllowPids).Count -eq 0) { return $true }
    $allow = @($global:PetQa.ClickAllowPids) + @($global:PetQa.Started | ForEach-Object { try { $_.Id } catch { } })
    $root = Get-QaRootWindowAt ([int]$X) ([int]$Y)
    if ($root -eq 0) { return $false }
    $w = [PetQa.Native]::Info([IntPtr]$root)
    if ($allow -contains $w.Pid) { return $true }
    $msg = "skipped injected click at ({0},{1}): window '{2}' (pid {3}) is not the pet or a QA window" -f [int]$X, [int]$Y, $w.Title, $w.Pid
    Write-Warning $msg
    if (-not $global:PetQa.ContainsKey('SkippedClicks')) { $global:PetQa.SkippedClicks = New-Object System.Collections.ArrayList }
    [void]$global:PetQa.SkippedClicks.Add($msg)
    return $false
}

function Invoke-QaClick {
    param([double]$X, [double]$Y, [ValidateSet('left', 'right')][string]$Button = 'left', [int]$HoverMs = 250)
    Move-QaCursor $X $Y
    Start-Sleep -Milliseconds $HoverMs
    if (-not (Test-QaClickTarget $X $Y)) { return }
    if ($Button -eq 'left') { $down = 0x0002; $up = 0x0004 } else { $down = 0x0008; $up = 0x0010 }
    $null = [PetQa.Native]::Mouse($down)
    Start-Sleep -Milliseconds 60
    $null = [PetQa.Native]::Mouse($up)
}

function Invoke-QaDoubleClick {
    param([double]$X, [double]$Y, [int]$HoverMs = 250)
    Move-QaCursor $X $Y
    Start-Sleep -Milliseconds $HoverMs
    if (-not (Test-QaClickTarget $X $Y)) { return }
    foreach ($i in 1..2) {
        $null = [PetQa.Native]::Mouse(0x0002); Start-Sleep -Milliseconds 30
        $null = [PetQa.Native]::Mouse(0x0004); Start-Sleep -Milliseconds 70
    }
}

function Invoke-QaDrag {
    # Drags from (X1,Y1) to (X2,Y2). $Probe (optional) runs at the midpoint and at the end,
    # while the button is still held. Returns @{ Mid = ...; End = ... } with the probe results.
    param([double]$X1, [double]$Y1, [double]$X2, [double]$Y2, [int]$Steps = 20, [int]$StepMs = 25, [scriptblock]$Probe = $null, [int]$HoverMs = 300)
    Move-QaCursor $X1 $Y1
    Start-Sleep -Milliseconds $HoverMs
    $out = @{ Mid = $null; End = $null }
    if (-not (Test-QaClickTarget $X1 $Y1)) { return $out }
    $null = [PetQa.Native]::Mouse(0x0002)
    try {
        for ($i = 1; $i -le $Steps; $i++) {
            Move-QaCursor ($X1 + ($X2 - $X1) * $i / $Steps) ($Y1 + ($Y2 - $Y1) * $i / $Steps)
            Start-Sleep -Milliseconds $StepMs
            if ($i -eq [int]($Steps / 2) -and $null -ne $Probe) { $out.Mid = & $Probe }
        }
        Start-Sleep -Milliseconds 200
        if ($null -ne $Probe) { $out.End = & $Probe }
    } finally {
        $null = [PetQa.Native]::Mouse(0x0004)
    }
    return $out
}

function Send-QaKey {
    param([int]$VirtualKey)
    $null = [PetQa.Native]::Key([uint16]$VirtualKey, $false)
    Start-Sleep -Milliseconds 30
    $null = [PetQa.Native]::Key([uint16]$VirtualKey, $true)
}

function Send-QaText {
    param([string]$Text)
    return [PetQa.Native]::TypeText($Text)
}

# ------------------------------------------------------------------ helper window (QaWindow.exe)
function Get-QaHelperExe {
    # Compiles QaWindow.cs once into qa\artifacts\bin\ (+ renamed copies used as distinct "apps").
    param([ValidateSet('QaWindow', 'QaEditor', 'KeePass')][string]$Name = 'QaWindow')
    $bin = Join-Path $script:QaArtifactsRoot 'bin'
    New-Item -ItemType Directory -Force -Path $bin | Out-Null
    $main = Join-Path $bin 'QaWindow.exe'
    $src = Join-Path $script:QaScriptsDir 'QaWindow.cs'
    if (-not (Test-Path $main) -or ((Get-Item $src).LastWriteTimeUtc -gt (Get-Item $main).LastWriteTimeUtc)) {
        $code = [IO.File]::ReadAllText($src)
        Add-Type -TypeDefinition $code -Language CSharp -OutputAssembly $main -OutputType WindowsApplication -ReferencedAssemblies System.Windows.Forms, System.Drawing
    }
    switch ($Name) {
        'QaWindow' { return $main }
        'QaEditor' { $dst = Join-Path $bin 'QaEditor.exe' }
        'KeePass' {
            $simDir = Join-Path $bin 'sim'
            New-Item -ItemType Directory -Force -Path $simDir | Out-Null
            $dst = Join-Path $simDir 'KeePass.exe'
        }
    }
    if (-not (Test-Path $dst) -or ((Get-Item $main).LastWriteTimeUtc -gt (Get-Item $dst).LastWriteTimeUtc)) {
        Copy-Item -LiteralPath $main -Destination $dst -Force
    }
    return $dst
}

function Register-QaPid {
    param([System.Diagnostics.Process]$Process, [string]$Role)
    [void]$global:PetQa.Started.Add($Process)
    if ($global:PetQa.PidRegistry) {
        $line = '{0}|{1}|{2}|{3}' -f $Process.Id, $Process.StartTime.ToFileTimeUtc(), $Process.ProcessName, $Role
        [IO.File]::AppendAllText($global:PetQa.PidRegistry, $line + "`r`n")
    }
}

function Start-QaWindow {
    param(
        [string]$Title = 'QA Window',
        [ValidateSet('QaWindow', 'QaEditor', 'KeePass')][string]$App = 'QaWindow',
        [int]$X = 200, [int]$Y = 200, [int]$W = 700, [int]$H = 450,
        [string]$Color = 'FF00FF',
        [switch]$Fullscreen, [switch]$NoActivate, [switch]$TopMost,
        [string]$ClickLog = '', [int]$LifetimeSec = 900, [string]$Label = '',
        [switch]$Foreground
    )
    Initialize-QaNative
    $exe = Get-QaHelperExe -Name $App
    $argList = @('--title', ('"{0}"' -f $Title), '--x', $X, '--y', $Y, '--w', $W, '--h', $H, '--color', $Color, '--lifetime', $LifetimeSec)
    if ($Fullscreen) { $argList += '--fullscreen' }
    if ($NoActivate) { $argList += '--noactivate' }
    if ($TopMost) { $argList += '--topmost' }
    if ($ClickLog) { $argList += @('--clicklog', ('"{0}"' -f $ClickLog)) }
    if ($Label) { $argList += @('--label', ('"{0}"' -f $Label)) }
    $p = Start-Process -FilePath $exe -ArgumentList $argList -PassThru
    Register-QaPid -Process $p -Role "qawindow:$App"
    $box = @{ Hwnd = [long]0 }
    $null = Wait-QaUntil -TimeoutMs 8000 -IntervalMs 100 -Condition {
        $ws = [PetQa.Native]::FindWindows('', $Title, $p.Id)
        if ($ws.Count -gt 0) { $box.Hwnd = [long]$ws[0].Hwnd; return $true }
        return $false
    }
    $hwnd = $box.Hwnd
    $obj = [pscustomobject]@{ Process = $p; Hwnd = [long]$hwnd; Title = $Title; App = $App; ClickLog = $ClickLog }
    if ($Foreground -and $hwnd) { $null = Set-QaForeground -Hwnd $hwnd }
    return $obj
}

function Start-QaBackdrop {
    # A magenta window filling the work area of the overlay's monitor (inset so it is never
    # "fullscreen" for SHQueryUserNotificationState). Sits UNDER the topmost overlay: real clicks
    # that pass through the overlay land here (and are logged), screenshots get a known background.
    # A label occupies roughly the top-left 800x150 px of the backdrop (avoid it in pixel checks).
    param($Work = $null, [string]$Title = 'PetAI QA Backdrop', [string]$ClickLog = '', [switch]$NoActivate, [int]$Inset = 6)
    if ($null -eq $Work) {
        $m = Get-QaPrimaryMonitor
        $Work = [pscustomobject]@{ X = $m.WorkX; Y = $m.WorkY; W = ($m.WorkRight - $m.WorkX); H = ($m.WorkBottom - $m.WorkY) }
    }
    $w = Start-QaWindow -Title $Title -X ($Work.X + $Inset) -Y ($Work.Y + $Inset) -W ($Work.W - 2 * $Inset) -H ($Work.H - 2 * $Inset) `
        -Color 'FF00FF' -ClickLog $ClickLog -NoActivate:$NoActivate -Foreground:(-not $NoActivate) `
        -Label 'PetAI QA backdrop - automated test running. Please do not use mouse/keyboard.'
    $w | Add-Member -NotePropertyName Rect -NotePropertyValue ([pscustomobject]@{ X = $Work.X + $Inset; Y = $Work.Y + $Inset; W = $Work.W - 2 * $Inset; H = $Work.H - 2 * $Inset })
    Start-Sleep -Milliseconds 600
    return $w
}

function Test-QaInLabelZone {
    param($Backdrop, [double]$X, [double]$Y)
    return ($X -lt ($Backdrop.Rect.X + 820) -and $Y -lt ($Backdrop.Rect.Y + 160))
}

function Get-QaFarPoints {
    # Points inside $Area (X,Y,W,H) at least $MinDist px away from the pet box, outside the backdrop label zone.
    param($Area, $Box, [int]$MinDist = 220, [int]$Grid = 6, $Backdrop = $null)
    $pts = @()
    for ($i = 0; $i -lt $Grid; $i++) {
        for ($j = 0; $j -lt $Grid; $j++) {
            $x = [int]($Area.X + 40 + ($Area.W - 80) * ($i + 0.5) / $Grid)
            $y = [int]($Area.Y + 40 + ($Area.H - 80) * ($j + 0.5) / $Grid)
            if ($null -ne $Backdrop -and (Test-QaInLabelZone $Backdrop $x $y)) { continue }
            if ($null -ne $Box) {
                $dx = [Math]::Max(0, [Math]::Max($Box.X - $x, $x - $Box.Right))
                $dy = [Math]::Max(0, [Math]::Max($Box.Y - $y, $y - $Box.Bottom))
                if ([Math]::Sqrt($dx * $dx + $dy * $dy) -lt $MinDist) { continue }
            }
            $pts += [pscustomobject]@{ X = $x; Y = $y }
        }
    }
    return $pts
}

function Get-QaOverlayWork {
    # Work area (X,Y,W,H,Bottom,Right) + monitor of the overlay, falling back to the primary monitor.
    param([long]$Hwnd = 0)
    if ($Hwnd -ne 0) {
        $r = Get-QaWindowReport -Hwnd $Hwnd
        return [pscustomobject]@{ Work = $r.Work; Monitor = $r.Monitor }
    }
    $m = Get-QaPrimaryMonitor
    return [pscustomobject]@{
        Work = [pscustomobject]@{ X = $m.WorkX; Y = $m.WorkY; W = ($m.WorkRight - $m.WorkX); H = ($m.WorkBottom - $m.WorkY); Bottom = $m.WorkBottom; Right = $m.WorkRight }
        Monitor = [pscustomobject]@{ X = $m.X; Y = $m.Y; W = $m.W; H = $m.H; Primary = $true }
    }
}

function Set-QaForeground {
    param([long]$Hwnd, [int]$TimeoutMs = 2000)
    Initialize-QaNative
    $ok = [PetQa.Native]::ForceForeground([IntPtr]$Hwnd)
    if (-not $ok) {
        $ok = Wait-QaUntil -TimeoutMs $TimeoutMs -IntervalMs 150 -Condition { [PetQa.Native]::ForceForeground([IntPtr]$Hwnd) }
    }
    return $ok
}

function Stop-QaWindow {
    param($Window)
    if ($null -eq $Window -or $null -eq $Window.Process) { return }
    try {
        if (-not $Window.Process.HasExited) {
            $Window.Process.Kill()
            $null = $Window.Process.WaitForExit(3000)
        }
    } catch { }
}

function Stop-QaStartedProcesses {
    # Stops only processes started by THIS script (tracked in $global:PetQa.Started).
    if ($null -eq $global:PetQa) { return }
    foreach ($p in @($global:PetQa.Started)) {
        try {
            if (-not $p.HasExited) { $p.Kill(); $null = $p.WaitForExit(3000) }
        } catch { }
    }
    $global:PetQa.Started.Clear()
}

function Read-QaClickLog {
    param([string]$Path)
    if (-not $Path -or -not (Test-Path $Path)) { return @() }
    return @(Get-Content -LiteralPath $Path | Where-Object { $_ -match ' down ' })
}

# ------------------------------------------------------------------ screen
function Save-QaScreenshot {
    # Captures a screen rectangle (physical px) to a PNG in the artifacts dir. Returns path + bitmap (caller disposes).
    param([int]$X, [int]$Y, [int]$W, [int]$H, [string]$Name, [switch]$KeepBitmap)
    Initialize-QaNative
    $bmp = [PetQa.Img]::Capture($X, $Y, $W, $H)
    $path = Get-QaArtifactPath $Name
    $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
    if ($KeepBitmap) { return [pscustomobject]@{ Path = $path; Bitmap = $bmp } }
    $bmp.Dispose()
    return [pscustomobject]@{ Path = $path; Bitmap = $null }
}

function Get-QaClampedRect {
    param([double]$X, [double]$Y, [double]$W, [double]$H, $Monitor)
    $x1 = [Math]::Max($Monitor.X, [int][Math]::Floor($X)); $y1 = [Math]::Max($Monitor.Y, [int][Math]::Floor($Y))
    $x2 = [Math]::Min($Monitor.X + $Monitor.W, [int][Math]::Ceiling($X + $W)); $y2 = [Math]::Min($Monitor.Y + $Monitor.H, [int][Math]::Ceiling($Y + $H))
    return [pscustomobject]@{ X = $x1; Y = $y1; W = [Math]::Max(1, $x2 - $x1); H = [Math]::Max(1, $y2 - $y1) }
}

# ------------------------------------------------------------------ files
function Find-QaStringInFiles {
    # Byte-level search (UTF-8/ASCII and UTF-16LE) of $Needle in all files under $Root. Returns matching paths.
    param([string]$Root, [string]$Needle, [long]$MaxBytes = 200MB)
    $hits = @()
    if (-not $Needle -or -not (Test-Path $Root)) { return $hits }
    $pat8 = [Text.Encoding]::UTF8.GetBytes($Needle)
    $pat16 = [Text.Encoding]::Unicode.GetBytes($Needle)
    foreach ($f in Get-ChildItem -LiteralPath $Root -Recurse -File -Force -ErrorAction SilentlyContinue) {
        if ($f.Length -gt $MaxBytes) { continue }
        try {
            $fs = New-Object IO.FileStream($f.FullName, [IO.FileMode]::Open, [IO.FileAccess]::Read, ([IO.FileShare]::ReadWrite -bor [IO.FileShare]::Delete))
            try {
                $buf = New-Object byte[] $fs.Length
                $read = 0
                while ($read -lt $buf.Length) { $n = $fs.Read($buf, $read, $buf.Length - $read); if ($n -le 0) { break }; $read += $n }
            } finally { $fs.Close() }
            if ((Test-QaBytesContain $buf $pat8) -or (Test-QaBytesContain $buf $pat16)) { $hits += $f.FullName }
        } catch { }
    }
    return $hits
}

function Test-QaBytesContain {
    param([byte[]]$Haystack, [byte[]]$Needle)
    if ($Needle.Length -eq 0 -or $Haystack.Length -lt $Needle.Length) { return $false }
    # Latin-1 maps bytes 1:1 to chars, so IndexOf on strings is a byte search.
    $enc = [Text.Encoding]::GetEncoding(28591)
    return ($enc.GetString($Haystack).IndexOf($enc.GetString($Needle), [StringComparison]::Ordinal) -ge 0)
}

# ------------------------------------------------------------------ app process
function Start-PetApp {
    # Launches the app with the QA environment. Env vars are set only for the child and restored afterwards.
    param(
        [Parameter(Mandatory = $true)][string]$Exe,
        [Parameter(Mandatory = $true)][string]$DataDir,
        [string]$DebugAddr = '127.0.0.1:47611',
        [string]$MockAddr = '127.0.0.1:47700',
        [string]$AnthropicKey = 'sk-test-qa-anthropic-FAKE-7d1e',
        [string]$OpenAIKey = 'sk-test-qa-openai-FAKE-9c2b',
        [switch]$ExcludeCaptureOff,
        [switch]$NoFast,
        [hashtable]$ExtraEnv = @{},
        [string]$LogPrefix = ''
    )
    New-Item -ItemType Directory -Force -Path $DataDir | Out-Null
    $envs = [ordered]@{
        PETAI_DATA_DIR           = $DataDir
        PETAI_DEBUG_ADDR         = $DebugAddr
        PETAI_ANTHROPIC_BASE_URL = "http://$MockAddr/anthropic"
        PETAI_OPENAI_BASE_URL    = "http://$MockAddr/openai"
        PETAI_API_KEY_ANTHROPIC  = $AnthropicKey
        PETAI_API_KEY_OPENAI     = $OpenAIKey
        PETAI_FAST               = $(if ($NoFast) { $null } else { '1' })
        PETAI_EXCLUDE_CAPTURE    = $(if ($ExcludeCaptureOff) { '0' } else { $null })
    }
    foreach ($k in $ExtraEnv.Keys) { $envs[$k] = $ExtraEnv[$k] }
    $saved = @{}
    foreach ($k in $envs.Keys) { $saved[$k] = [Environment]::GetEnvironmentVariable($k, 'Process') }
    try {
        foreach ($k in $envs.Keys) { [Environment]::SetEnvironmentVariable($k, $envs[$k], 'Process') }
        $spArgs = @{ FilePath = $Exe; PassThru = $true; WorkingDirectory = (Split-Path -Parent $Exe) }
        if ($LogPrefix) {
            $spArgs['RedirectStandardOutput'] = "$LogPrefix.stdout.txt"
            $spArgs['RedirectStandardError'] = "$LogPrefix.stderr.txt"
        }
        $p = Start-Process @spArgs
    } finally {
        foreach ($k in $saved.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k], 'Process') }
    }
    $null = $p.Handle  # keep a handle so ExitCode stays readable
    if ($null -ne $global:PetQa) { Register-QaPid -Process $p -Role 'petai' }
    return $p
}

function Wait-PetReady {
    param([string]$DebugAddr, [System.Diagnostics.Process]$Process, [int]$TimeoutSec = 45)
    $sw = [Diagnostics.Stopwatch]::StartNew()
    while ($sw.Elapsed.TotalSeconds -lt $TimeoutSec) {
        if ($null -ne $Process -and $Process.HasExited) { return $false }
        $s = Get-PetState -DebugAddr $DebugAddr -TimeoutSec 2
        if ($null -ne $s) {
            # also wait for the overlay window to exist
            $ov = @(Find-PetOverlay -ProcessId $Process.Id)
            if ($ov.Count -gt 0) { return $true }
        }
        Start-Sleep -Milliseconds 500
    }
    return $false
}

function Stop-PetApp {
    # Graceful WM_CLOSE to the overlay, then kill. Only ever called with a process we started.
    param([System.Diagnostics.Process]$Process, [int]$GraceMs = 6000)
    if ($null -eq $Process) { return 'none' }
    try { if ($Process.HasExited) { return 'already-exited' } } catch { return 'unknown' }
    Initialize-QaNative
    foreach ($w in [PetQa.Native]::FindWindows('', '', $Process.Id)) {
        if ($w.ClassName -eq $script:QaOverlayClass) { $null = [PetQa.Native]::PostMessage([IntPtr]$w.Hwnd, 0x0010, [IntPtr]::Zero, [IntPtr]::Zero) }
    }
    if ($Process.WaitForExit($GraceMs)) { return 'graceful' }
    try { $Process.Kill(); $null = $Process.WaitForExit(5000) } catch { }
    return 'killed'
}

function Get-QaProcessTree {
    # The process plus all descendants (e.g. msedgewebview2.exe), via CIM (read-only).
    param([int]$RootPid)
    $all = @(Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Select-Object ProcessId, ParentProcessId, Name)
    $ids = New-Object System.Collections.Generic.HashSet[int]
    [void]$ids.Add($RootPid)
    $changed = $true
    while ($changed) {
        $changed = $false
        foreach ($p in $all) {
            if ($ids.Contains([int]$p.ParentProcessId) -and -not $ids.Contains([int]$p.ProcessId)) { [void]$ids.Add([int]$p.ProcessId); $changed = $true }
        }
    }
    return @($all | Where-Object { $ids.Contains([int]$_.ProcessId) })
}

function Get-QaPetProcess {
    # Finds the running petai process: prefer the PID of the overlay window.
    param([int]$ProcessId = 0)
    if ($ProcessId -gt 0) { return Get-Process -Id $ProcessId -ErrorAction SilentlyContinue }
    $ov = @(Find-PetOverlay)
    if ($ov.Count -gt 0) { return Get-Process -Id $ov[0].Pid -ErrorAction SilentlyContinue }
    return $null
}
