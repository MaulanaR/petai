// Package overlay turns the Wails main window into a transparent, always-on-top,
// click-through desktop overlay and keeps click-through in sync with the pet's hit regions.
package overlay

import (
	"context"
	"errors"
	"math"
	"os"
	"runtime"
	"sync"
	"time"

	"petai/internal/win"
)

// Rect is a rectangle in CSS pixels relative to the overlay's top-left corner.
type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

func (r Rect) contains(x, y float64) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H
}

// Monitor describes the monitor the overlay covers.
type Monitor struct {
	Index  int     `json:"index"`
	Bounds Phys    `json:"bounds"` // physical px, virtual-screen coordinates
	Work   Phys    `json:"work"`   // physical px, virtual-screen coordinates
	Scale  float64 `json:"scale"`
	// WorkCSS is the work area in CSS px relative to the overlay origin.
	WorkCSS Rect `json:"workCss"`
	// SizeCSS is the overlay size in CSS px.
	SizeCSS Rect `json:"sizeCss"`
}

type Phys struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
	W int32 `json:"w"`
	H int32 `json:"h"`
}

// Mode selects how click-through is achieved.
const (
	ModeExStyle = "exstyle"
	ModeRegion  = "region"
)

type Overlay struct {
	hwnd uintptr
	mode string

	mu          sync.Mutex
	regions     []Rect
	force       bool
	interactive bool
	focusable   bool
	prevFG      uintptr
	mon         Monitor
	lastCursor  win.Point
	hidden      bool
	tick        int
	// scaleOverride is the WebView devicePixelRatio once the frontend reported it.
	scaleOverride float64

	// OnCursor receives the cursor position in CSS px relative to the overlay (~30 Hz, only on change).
	OnCursor func(x, y float64)
}

// Attach finds the Wails window of this process and applies overlay styles.
func Attach(mode string) (*Overlay, error) {
	pid := uint32(os.Getpid())
	var h uintptr
	for i := 0; i < 50 && h == 0; i++ {
		h = win.FindProcessWindow("wailsWindow", pid)
		if h == 0 {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if h == 0 {
		return nil, errors.New("overlay: wails window not found")
	}
	if mode != ModeRegion {
		mode = ModeExStyle
	}
	o := &Overlay{hwnd: h, mode: mode}
	ex := win.GetExStyle(h)
	ex &^= win.WS_EX_APPWINDOW
	ex |= win.WS_EX_TOOLWINDOW | win.WS_EX_NOACTIVATE | win.WS_EX_TOPMOST
	if mode == ModeExStyle {
		ex |= win.WS_EX_LAYERED | win.WS_EX_TRANSPARENT
	}
	win.SetExStyle(h, ex)
	if mode == ModeExStyle {
		win.SetLayeredAlpha(h, 255)
	} else {
		win.SetRegion(h, nil, false)
	}
	return o, nil
}

func (o *Overlay) HWND() uintptr { return o.hwnd }
func (o *Overlay) Mode() string  { return o.mode }

// ExStyle returns the current extended window style.
func (o *Overlay) ExStyle() uintptr { return win.GetExStyle(o.hwnd) }

// SetExcludeFromCapture hides the overlay from screenshots / screen sharing.
func (o *Overlay) SetExcludeFromCapture(on bool) bool {
	if on {
		return win.SetDisplayAffinity(o.hwnd, win.WDA_EXCLUDEFROMCAPTURE)
	}
	return win.SetDisplayAffinity(o.hwnd, win.WDA_NONE)
}

// FitMonitor moves/resizes the overlay to cover the work area of monitor index (falls back to
// primary) and shows it without activating it.
//
// The overlay deliberately covers the work area (not the full monitor) and is never exactly
// monitor-sized: Windows treats a foreground monitor-sized window as a fullscreen app
// (QUNS_BUSY, taskbar auto-hide), which would also trip our own do-not-disturb detection.
func (o *Overlay) FitMonitor(index int) Monitor {
	mons := win.Monitors()
	hmon := win.PrimaryMonitor()
	idx := 0
	for i, m := range mons {
		if m == hmon {
			idx = i
		}
	}
	if index >= 0 && index < len(mons) {
		hmon, idx = mons[index], index
	}
	mi, _ := win.GetMonitorInfo(hmon)
	w := mi.RcWork
	if w == mi.RcMonitor {
		w.Bottom-- // auto-hide taskbar: stay 1px short of fullscreen
	}
	o.place(w)
	scale := float64(win.DpiForWindow(o.hwnd)) / 96.0
	o.mu.Lock()
	if o.scaleOverride > 0 {
		scale = o.scaleOverride
	}
	o.mu.Unlock()
	m := Monitor{
		Index:   idx,
		Bounds:  Phys{w.Left, w.Top, w.W(), w.H()},
		Work:    Phys{w.Left, w.Top, w.W(), w.H()},
		Scale:   scale,
		WorkCSS: Rect{W: float64(w.W()) / scale, H: float64(w.H()) / scale},
		SizeCSS: Rect{W: float64(w.W()) / scale, H: float64(w.H()) / scale},
	}
	o.mu.Lock()
	o.mon = m
	o.mu.Unlock()
	return m
}

// place positions the window (topmost) and shows it with SetWindowPos, which — unlike the first
// ShowWindow call of a process — never honours STARTUPINFO and therefore never activates.
// If showing still stole the foreground, focus is handed back.
func (o *Overlay) place(r win.Rect) {
	prev := win.ForegroundWindow()
	flags := uint32(win.SWP_NOACTIVATE | win.SWP_FRAMECHANGED)
	o.mu.Lock()
	if !o.hidden {
		flags |= win.SWP_SHOWWINDOW
	}
	o.mu.Unlock()
	win.SetWindowPos(o.hwnd, win.HWND_TOPMOST, r.Left, r.Top, r.W(), r.H(), flags)
	o.giveBackFocus(prev)
}

func (o *Overlay) giveBackFocus(prev uintptr) {
	o.mu.Lock()
	focusable := o.focusable
	o.mu.Unlock()
	if focusable || prev == 0 || prev == o.hwnd {
		return
	}
	if win.ForegroundWindow() == o.hwnd {
		win.SetForegroundWindow(prev)
	}
}

// SetScale overrides the CSS→physical pixel ratio with the WebView's devicePixelRatio, which
// also includes browser zoom / Windows text scaling (DPI alone is not enough).
func (o *Overlay) SetScale(s float64) bool {
	if s <= 0.25 || s > 8 {
		return false
	}
	o.mu.Lock()
	changed := math.Abs(o.mon.Scale-s) > 0.0005
	if changed {
		o.mon.Scale = s
		b := o.mon.Bounds
		o.mon.WorkCSS = Rect{W: float64(b.W) / s, H: float64(b.H) / s}
		o.mon.SizeCSS = o.mon.WorkCSS
		o.scaleOverride = s
	}
	o.mu.Unlock()
	if changed && o.mode == ModeRegion {
		o.applyRegion()
	}
	return changed
}

func (o *Overlay) Monitor() Monitor {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.mon
}

// SetVisible hides or shows the overlay without activating it.
func (o *Overlay) SetVisible(v bool) {
	o.mu.Lock()
	o.hidden = !v
	o.mu.Unlock()
	if v {
		prev := win.ForegroundWindow()
		win.SetWindowPos(o.hwnd, win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE|win.SWP_SHOWWINDOW)
		o.giveBackFocus(prev)
	} else {
		win.ShowWindow(o.hwnd, win.SW_HIDE)
	}
}

func (o *Overlay) Visible() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return !o.hidden
}

// SetRegions replaces the interactive hit regions (CSS px).
func (o *Overlay) SetRegions(r []Rect) {
	o.mu.Lock()
	o.regions = append(o.regions[:0], r...)
	o.mu.Unlock()
	if o.mode == ModeRegion {
		o.applyRegion()
	}
}

// SetForceInteractive keeps the overlay interactive regardless of cursor position (drag, panels).
func (o *Overlay) SetForceInteractive(on bool) {
	o.mu.Lock()
	o.force = on
	o.mu.Unlock()
	if o.mode == ModeRegion {
		o.applyRegion()
	}
}

// SetFocusable lets the overlay take keyboard focus (chat/settings input) or returns focus.
func (o *Overlay) SetFocusable(on bool) {
	o.mu.Lock()
	if o.focusable == on {
		o.mu.Unlock()
		return
	}
	o.focusable = on
	o.mu.Unlock()
	ex := win.GetExStyle(o.hwnd)
	if on {
		fg := win.ForegroundWindow()
		if fg != o.hwnd {
			o.mu.Lock()
			o.prevFG = fg
			o.mu.Unlock()
		}
		win.SetExStyle(o.hwnd, ex&^win.WS_EX_NOACTIVATE)
		o.takeForeground(fg)
		return
	}
	win.SetExStyle(o.hwnd, ex|win.WS_EX_NOACTIVATE)
	o.mu.Lock()
	prev := o.prevFG
	o.prevFG = 0
	o.mu.Unlock()
	if prev != 0 && win.ForegroundWindow() == o.hwnd {
		win.SetForegroundWindow(prev)
	}
}

// takeForeground activates the overlay for keyboard input. A plain SetForegroundWindow is
// often refused by the foreground lock (our clicks never activate the NOACTIVATE window), so
// the input queue of the current foreground thread is attached for the duration of the call.
func (o *Overlay) takeForeground(fg uintptr) {
	if win.SetForegroundWindow(o.hwnd) && win.ForegroundWindow() == o.hwnd {
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cur := win.CurrentThreadID()
	if fgTid := win.WindowThreadID(fg); fg != 0 && fgTid != 0 && fgTid != cur {
		if win.AttachThreadInput(cur, fgTid, true) {
			defer win.AttachThreadInput(cur, fgTid, false)
		}
	}
	win.BringWindowToTop(o.hwnd)
	win.SetForegroundWindow(o.hwnd)
}

// Interactive reports whether the overlay currently receives mouse input.
func (o *Overlay) Interactive() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.interactive
}

// Run polls the cursor and toggles click-through until ctx is done.
func (o *Overlay) Run(ctx context.Context) {
	t := time.NewTicker(16 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			o.step()
		}
	}
}

func (o *Overlay) step() {
	p, ok := win.CursorPos()
	if !ok {
		return
	}
	o.mu.Lock()
	m := o.mon
	scale := m.Scale
	if scale <= 0 {
		scale = 1
	}
	x := float64(p.X-m.Bounds.X) / scale
	y := float64(p.Y-m.Bounds.Y) / scale
	inside := o.force
	if !inside {
		for _, r := range o.regions {
			if r.contains(x, y) {
				inside = true
				break
			}
		}
	}
	// Drags need no special case here: the frontend forces interactivity on mousedown and the
	// WebView holds mouse capture. Keeping the overlay interactive while a button is down
	// swallowed fast clicks just outside the pet.
	if o.hidden {
		inside = false
	}
	changed := inside != o.interactive
	o.interactive = inside
	o.tick++
	emit := o.tick%2 == 0 && p != o.lastCursor
	if emit {
		o.lastCursor = p
	}
	cb := o.OnCursor
	o.mu.Unlock()

	if changed && o.mode == ModeExStyle {
		ex := win.GetExStyle(o.hwnd)
		if inside {
			ex &^= win.WS_EX_TRANSPARENT
		} else {
			ex |= win.WS_EX_TRANSPARENT
		}
		win.SetExStyle(o.hwnd, ex)
	}
	if emit && cb != nil {
		cb(math.Round(x*10)/10, math.Round(y*10)/10)
	}
}

func (o *Overlay) applyRegion() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.force {
		win.SetRegion(o.hwnd, nil, true)
		return
	}
	s := o.mon.Scale
	if s <= 0 {
		s = 1
	}
	rs := make([]win.Rect, 0, len(o.regions))
	for _, r := range o.regions {
		rs = append(rs, win.Rect{
			Left: int32(r.X * s), Top: int32(r.Y * s),
			Right: int32(math.Ceil((r.X + r.W) * s)), Bottom: int32(math.Ceil((r.Y + r.H) * s)),
		})
	}
	win.SetRegion(o.hwnd, rs, false)
}
