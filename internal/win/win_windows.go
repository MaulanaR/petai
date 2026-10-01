// Package win holds the small set of raw Win32 bindings PetAI needs.
package win

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procFindWindowExW                = user32.NewProc("FindWindowExW")
	procGetWindowThreadProcessId     = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowLongPtrW            = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW            = user32.NewProc("SetWindowLongPtrW")
	procSetLayeredWindowAttributes   = user32.NewProc("SetLayeredWindowAttributes")
	procSetWindowPos                 = user32.NewProc("SetWindowPos")
	procShowWindow                   = user32.NewProc("ShowWindow")
	procGetCursorPos                 = user32.NewProc("GetCursorPos")
	procGetDpiForWindow              = user32.NewProc("GetDpiForWindow")
	procMonitorFromPoint             = user32.NewProc("MonitorFromPoint")
	procMonitorFromWindow            = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW              = user32.NewProc("GetMonitorInfoW")
	procEnumDisplayMonitors          = user32.NewProc("EnumDisplayMonitors")
	procSetWindowDisplayAffinity     = user32.NewProc("SetWindowDisplayAffinity")
	procSetForegroundWindow          = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow          = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW               = user32.NewProc("GetWindowTextW")
	procGetClassNameW                = user32.NewProc("GetClassNameW")
	procGetWindowRect                = user32.NewProc("GetWindowRect")
	procIsIconic                     = user32.NewProc("IsIconic")
	procIsZoomed                     = user32.NewProc("IsZoomed")
	procIsWindowVisible              = user32.NewProc("IsWindowVisible")
	procGetLastInputInfo             = user32.NewProc("GetLastInputInfo")
	procGetAsyncKeyState             = user32.NewProc("GetAsyncKeyState")
	procSetWindowRgn                 = user32.NewProc("SetWindowRgn")
	procRegisterHotKey               = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey             = user32.NewProc("UnregisterHotKey")
	procGetMessageW                  = user32.NewProc("GetMessageW")
	procPostThreadMessageW           = user32.NewProc("PostThreadMessageW")
	procCreateRectRgn                = gdi32.NewProc("CreateRectRgn")
	procCombineRgn                   = gdi32.NewProc("CombineRgn")
	procDeleteObject                 = gdi32.NewProc("DeleteObject")
	procSHQueryUserNotificationState = shell32.NewProc("SHQueryUserNotificationState")
	procDwmGetWindowAttribute        = dwmapi.NewProc("DwmGetWindowAttribute")
	procGetTickCount                 = kernel32.NewProc("GetTickCount")
	procGetCurrentThreadId           = kernel32.NewProc("GetCurrentThreadId")
)

const (
	GWL_EXSTYLE = -20

	WS_EX_TOPMOST     = 0x00000008
	WS_EX_TRANSPARENT = 0x00000020
	WS_EX_TOOLWINDOW  = 0x00000080
	WS_EX_APPWINDOW   = 0x00040000
	WS_EX_LAYERED     = 0x00080000
	WS_EX_NOACTIVATE  = 0x08000000

	LWA_ALPHA = 0x2

	SWP_NOSIZE       = 0x0001
	SWP_NOMOVE       = 0x0002
	SWP_NOZORDER     = 0x0004
	SWP_NOACTIVATE   = 0x0010
	SWP_FRAMECHANGED = 0x0020
	SWP_SHOWWINDOW   = 0x0040

	SW_HIDE           = 0
	SW_SHOWNOACTIVATE = 4

	MONITOR_DEFAULTTOPRIMARY = 1
	MONITOR_DEFAULTTONEAREST = 2

	WDA_NONE               = 0
	WDA_EXCLUDEFROMCAPTURE = 0x11

	VK_LBUTTON = 0x01

	DWMWA_EXTENDED_FRAME_BOUNDS = 9
	DWMWA_CLOAKED               = 14

	RGN_OR = 2

	WM_HOTKEY = 0x0312
	WM_QUIT   = 0x0012

	MOD_ALT      = 0x1
	MOD_CONTROL  = 0x2
	MOD_SHIFT    = 0x4
	MOD_NOREPEAT = 0x4000

	// QUERY_USER_NOTIFICATION_STATE
	QUNS_NOT_PRESENT             = 1
	QUNS_BUSY                    = 2
	QUNS_RUNNING_D3D_FULL_SCREEN = 3
	QUNS_PRESENTATION_MODE       = 4
	QUNS_ACCEPTS_NOTIFICATIONS   = 5
	QUNS_QUIET_TIME              = 6
	QUNS_APP                     = 7
)

// HWND_TOPMOST is (HWND)-1.
var HWND_TOPMOST = ^uintptr(0)

type Point struct{ X, Y int32 }

type Rect struct{ Left, Top, Right, Bottom int32 }

func (r Rect) W() int32 { return r.Right - r.Left }
func (r Rect) H() int32 { return r.Bottom - r.Top }

type MonitorInfo struct {
	CbSize    uint32
	RcMonitor Rect
	RcWork    Rect
	DwFlags   uint32
}

type lastInputInfo struct {
	CbSize uint32
	DwTime uint32
}

type Msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      Point
}

// FindProcessWindow returns the first top-level window of the given class owned by pid.
func FindProcessWindow(class string, pid uint32) uintptr {
	cls, _ := syscall.UTF16PtrFromString(class)
	var h uintptr
	for {
		h, _, _ = procFindWindowExW.Call(0, h, uintptr(unsafe.Pointer(cls)), 0)
		if h == 0 {
			return 0
		}
		if WindowPID(h) == pid {
			return h
		}
	}
}

func WindowPID(h uintptr) uint32 {
	var pid uint32
	procGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func GetExStyle(h uintptr) uintptr {
	i := int32(GWL_EXSTYLE)
	r, _, _ := procGetWindowLongPtrW.Call(h, uintptr(i))
	return r
}

func SetExStyle(h uintptr, style uintptr) {
	i := int32(GWL_EXSTYLE)
	procSetWindowLongPtrW.Call(h, uintptr(i), style)
}

func SetLayeredAlpha(h uintptr, alpha byte) {
	procSetLayeredWindowAttributes.Call(h, 0, uintptr(alpha), LWA_ALPHA)
}

func SetWindowPos(h, after uintptr, x, y, w, hh int32, flags uint32) bool {
	r, _, _ := procSetWindowPos.Call(h, after, uintptr(x), uintptr(y), uintptr(w), uintptr(hh), uintptr(flags))
	return r != 0
}

func ShowWindow(h uintptr, cmd int32) {
	procShowWindow.Call(h, uintptr(cmd))
}

func CursorPos() (Point, bool) {
	var p Point
	r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p, r != 0
}

func DpiForWindow(h uintptr) uint32 {
	if procGetDpiForWindow.Find() != nil {
		return 96
	}
	r, _, _ := procGetDpiForWindow.Call(h)
	if r == 0 {
		return 96
	}
	return uint32(r)
}

func PrimaryMonitor() uintptr {
	// POINT passed by value: on amd64 the 8-byte struct goes in one register.
	r, _, _ := procMonitorFromPoint.Call(0, MONITOR_DEFAULTTOPRIMARY)
	return r
}

func MonitorOfWindow(h uintptr) uintptr {
	r, _, _ := procMonitorFromWindow.Call(h, MONITOR_DEFAULTTONEAREST)
	return r
}

func GetMonitorInfo(hmon uintptr) (MonitorInfo, bool) {
	mi := MonitorInfo{CbSize: uint32(unsafe.Sizeof(MonitorInfo{}))}
	r, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi)))
	return mi, r != 0
}

// Monitors lists all display monitors in enumeration order.
func Monitors() []uintptr {
	var out []uintptr
	cb := syscall.NewCallback(func(hmon, hdc, lprc, data uintptr) uintptr {
		out = append(out, hmon)
		return 1
	})
	procEnumDisplayMonitors.Call(0, 0, cb, 0)
	return out
}

func SetDisplayAffinity(h uintptr, aff uint32) bool {
	r, _, _ := procSetWindowDisplayAffinity.Call(h, uintptr(aff))
	return r != 0
}

func SetForegroundWindow(h uintptr) bool {
	r, _, _ := procSetForegroundWindow.Call(h)
	return r != 0
}

func ForegroundWindow() uintptr {
	r, _, _ := procGetForegroundWindow.Call()
	return r
}

func WindowText(h uintptr) string {
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func ClassName(h uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

// WindowRect returns the visible frame bounds (DWM extended frame), falling back to GetWindowRect.
func WindowRect(h uintptr) Rect {
	var r Rect
	hr, _, _ := procDwmGetWindowAttribute.Call(h, DWMWA_EXTENDED_FRAME_BOUNDS, uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r))
	if hr == 0 && r.W() > 0 {
		return r
	}
	procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	return r
}

func IsCloaked(h uintptr) bool {
	var v uint32
	hr, _, _ := procDwmGetWindowAttribute.Call(h, DWMWA_CLOAKED, uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
	return hr == 0 && v != 0
}

func IsIconic(h uintptr) bool {
	r, _, _ := procIsIconic.Call(h)
	return r != 0
}

func IsZoomed(h uintptr) bool {
	r, _, _ := procIsZoomed.Call(h)
	return r != 0
}

func IsWindowVisible(h uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(h)
	return r != 0
}

// IdleMillis returns milliseconds since the last user input (keyboard/mouse) system-wide.
func IdleMillis() uint32 {
	li := lastInputInfo{CbSize: uint32(unsafe.Sizeof(lastInputInfo{}))}
	r, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&li)))
	if r == 0 {
		return 0
	}
	now, _, _ := procGetTickCount.Call()
	return uint32(now) - li.DwTime
}

func KeyDown(vk int) bool {
	r, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return r&0x8000 != 0
}

// NotificationState wraps SHQueryUserNotificationState.
func NotificationState() int {
	var st int32
	hr, _, _ := procSHQueryUserNotificationState.Call(uintptr(unsafe.Pointer(&st)))
	if hr != 0 {
		return 0
	}
	return int(st)
}

// SetRegion sets the window region to the union of rects (window-relative physical px).
// An empty slice sets an empty region (window fully click-through and invisible).
// nil rects slice with all=true removes the region.
func SetRegion(h uintptr, rects []Rect, all bool) {
	if all {
		procSetWindowRgn.Call(h, 0, 1)
		return
	}
	rgn, _, _ := procCreateRectRgn.Call(0, 0, 0, 0)
	for _, r := range rects {
		part, _, _ := procCreateRectRgn.Call(uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom))
		procCombineRgn.Call(rgn, rgn, part, RGN_OR)
		procDeleteObject.Call(part)
	}
	// The system owns rgn after a successful SetWindowRgn.
	procSetWindowRgn.Call(h, rgn, 1)
}

func RegisterHotKey(id int, mods, vk uint32) bool {
	r, _, _ := procRegisterHotKey.Call(0, uintptr(id), uintptr(mods|MOD_NOREPEAT), uintptr(vk))
	return r != 0
}

func UnregisterHotKey(id int) {
	procUnregisterHotKey.Call(0, uintptr(id))
}

func GetMessage(m *Msg) int32 {
	r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(m)), 0, 0, 0)
	return int32(r)
}

func CurrentThreadID() uint32 {
	r, _, _ := procGetCurrentThreadId.Call()
	return uint32(r)
}

func PostThreadMessage(tid uint32, msg uint32) {
	procPostThreadMessageW.Call(uintptr(tid), uintptr(msg), 0, 0)
}

// ProcessImageName returns the full exe path of a process, or "".
func ProcessImageName(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}
