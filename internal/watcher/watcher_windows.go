// Package watcher observes the foreground window, user idle time and do-not-disturb state.
// It never reads keystrokes or the clipboard.
package watcher

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kbinani/screenshot"
	"golang.org/x/image/draw"

	"petai/internal/win"
)

// Foreground describes the active window. Rect is in physical virtual-screen px.
type Foreground struct {
	HWND      uintptr  `json:"-"`
	App       string   `json:"app"`
	Title     string   `json:"title"`
	PID       uint32   `json:"pid"`
	Rect      win.Rect `json:"rect"`
	Maximized bool     `json:"maximized"`
	Desktop   bool     `json:"desktop"` // shell/desktop/taskbar
}

func (f Foreground) same(o Foreground) bool {
	return f.HWND == o.HWND && f.Title == o.Title && f.Rect == o.Rect && f.Maximized == o.Maximized
}

type Callbacks struct {
	OnForeground func(Foreground)
	OnIdle       func(idle time.Duration)
	OnBusy       func(busy bool)
}

type Watcher struct {
	ownPID uint32
	cb     Callbacks

	mu   sync.Mutex
	fg   Foreground
	busy bool
}

func New(cb Callbacks) *Watcher {
	return &Watcher{ownPID: uint32(os.Getpid()), cb: cb}
}

var shellClasses = map[string]bool{
	"Progman": true, "WorkerW": true, "Shell_TrayWnd": true, "Shell_SecondaryTrayWnd": true,
	"Windows.UI.Core.CoreWindow": true, "NotifyIconOverflowWindow": true, "TopLevelWindowForOverflowXamlIsland": true,
}

// Current reads the foreground window now. ok=false when it is our own window.
func (w *Watcher) Current() (Foreground, bool) {
	h := win.ForegroundWindow()
	if h == 0 {
		return Foreground{Desktop: true}, true
	}
	pid := win.WindowPID(h)
	if pid == w.ownPID {
		return Foreground{}, false
	}
	cls := win.ClassName(h)
	f := Foreground{HWND: h, PID: pid}
	exe := win.ProcessImageName(pid)
	f.App = strings.ToLower(filepath.Base(exe))
	if shellClasses[cls] {
		f.Desktop = true
		return f, true
	}
	f.Title = win.WindowText(h)
	if win.IsIconic(h) || win.IsCloaked(h) || !win.IsWindowVisible(h) {
		f.Desktop = true
		return f, true
	}
	f.Rect = win.WindowRect(h)
	f.Maximized = win.IsZoomed(h)
	return f, true
}

func (w *Watcher) Foreground() Foreground {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fg
}

func (w *Watcher) Busy() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.busy
}

// Run polls once per second until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		w.poll()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (w *Watcher) poll() {
	f, ok := w.Current()
	if ok {
		w.mu.Lock()
		changed := !f.same(w.fg)
		if changed {
			w.fg = f
		}
		w.mu.Unlock()
		if changed && w.cb.OnForeground != nil {
			w.cb.OnForeground(f)
		}
	}
	if w.cb.OnIdle != nil {
		w.cb.OnIdle(time.Duration(win.IdleMillis()) * time.Millisecond)
	}
	if !ok {
		return // our own overlay is in front (chat/settings): never "busy"
	}
	busy := IsBusy(w.Foreground())
	w.mu.Lock()
	changed := busy != w.busy
	w.busy = busy
	w.mu.Unlock()
	if changed && w.cb.OnBusy != nil {
		w.cb.OnBusy(busy)
	}
}

// IsBusy is true for fullscreen games, presentations and fullscreen apps (videos etc.).
func IsBusy(fg Foreground) bool {
	switch win.NotificationState() {
	case win.QUNS_BUSY, win.QUNS_RUNNING_D3D_FULL_SCREEN, win.QUNS_PRESENTATION_MODE:
		return true
	}
	if fg.Desktop || fg.HWND == 0 {
		return false
	}
	mi, ok := win.GetMonitorInfo(win.MonitorOfWindow(fg.HWND))
	return ok && fg.Rect == mi.RcMonitor
}

// CaptureJPEG grabs the monitor containing bounds, downscales to ≤maxW px wide and encodes JPEG.
// The image only ever lives in memory.
func CaptureJPEG(bounds image.Rectangle, maxW int, quality int) ([]byte, error) {
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, err
	}
	var src image.Image = img
	if b := img.Bounds(); b.Dx() > maxW {
		h := b.Dy() * maxW / b.Dx()
		dst := image.NewRGBA(image.Rect(0, 0, maxW, h))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		src = dst
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
