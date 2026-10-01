package main

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"petai/internal/ai"
	"petai/internal/anim"
	"petai/internal/brain"
	"petai/internal/config"
	"petai/internal/debugapi"
	"petai/internal/logx"
	"petai/internal/overlay"
	"petai/internal/paths"
	"petai/internal/secrets"
	"petai/internal/store"
	"petai/internal/sys"
	"petai/internal/tray"
	"petai/internal/watcher"
	"petai/internal/win"
)

// appVersion is overridden at release build time: -ldflags "-X main.appVersion=x.y.z".
var appVersion = "0.1.0"

// PetState is reported by the frontend (CSS px relative to the overlay).
type PetState struct {
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	W         float64 `json:"w"`
	H         float64 `json:"h"`
	Character string  `json:"character"`
	Mode      string  `json:"mode"`
	State     string  `json:"state"`
	Animation string  `json:"animation"`
	Visible   bool    `json:"visible"`
	// Viewport diagnostics (CSS px).
	ViewW   float64 `json:"viewW"`
	ViewH   float64 `json:"viewH"`
	ScrollY float64 `json:"scrollY"`
	DPR     float64 `json:"dpr"`
}

// Bootstrap is everything the frontend needs at load.
type Bootstrap struct {
	Config     config.Config     `json:"config"`
	Monitor    overlay.Monitor   `json:"monitor"`
	Animations []anim.Spec       `json:"animations"`
	Keys       map[string]string `json:"keys"`
	Fast       bool              `json:"fast"`
	Version    string            `json:"version"`
	InitError  string            `json:"initError"`
	Monitors   int               `json:"monitors"`
}

type TestResult struct {
	OK     bool     `json:"ok"`
	Models []string `json:"models"`
	Error  string   `json:"error"`
}

type App struct {
	ctx    context.Context
	cancel context.CancelFunc

	log  *logx.Logger
	cfg  *config.Manager
	st   *store.Store
	lib  *anim.Library
	eng  *brain.Engine
	wat  *watcher.Watcher
	ov   *overlay.Overlay
	fast bool

	mu          sync.Mutex
	regions     []overlay.Rect
	pet         PetState
	hidden      bool // user hid the pet (tray / hide 1h)
	busy        bool // fullscreen app in front
	hideTimer   *time.Timer
	quitting    bool
	initErr     string
	stopHotkey  func()
	stopDebug   func()
	lastMonitor int
	launchFG    uintptr
	feLogAt     time.Time
}

func NewApp() *App { return &App{fast: os.Getenv("PETAI_FAST") == "1"} }

func (a *App) emit(name string, data any) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, name, data)
	}
}

func (a *App) logf(format string, args ...any) { a.log.Printf(format, args...) }

// ---------------- lifecycle ----------------

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.log = logx.Open(paths.Sub("logs", "petai.log"))
	a.logf("PetAI %s starting (data=%s fast=%v)", appVersion, paths.DataDir(), a.fast)

	var errs []string
	cm, err := config.Load(paths.Sub("config.json"))
	if err != nil {
		errs = append(errs, "config: "+err.Error())
		cm, _ = config.Load(paths.Sub("config.recovered.json"))
	}
	a.cfg = cm
	if a.st, err = store.Open(paths.Sub("petai.db")); err != nil {
		errs = append(errs, "database: "+err.Error())
	} else {
		days := cm.Get().Privacy.RetentionDays
		if n, err := a.st.PurgeActivity(time.Now().AddDate(0, 0, -days)); err == nil && n > 0 {
			a.logf("purged %d activity rows older than %d days", n, days)
		}
	}
	if a.lib, err = anim.Open(filepath.Join(paths.DataDir(), "animations")); err != nil {
		errs = append(errs, "animations: "+err.Error())
	}
	a.initErr = strings.Join(errs, "; ")
	if a.initErr != "" {
		a.logf("init errors: %s", a.initErr)
	}

	a.eng = brain.New(brain.Deps{
		Config:   a.cfg,
		Store:    a.st,
		Lib:      a.lib,
		Provider: a.provider,
		Emit:     a.emit,
		Capture:  a.capture,
		Logf:     a.logf,
		Fast:     a.fast,
		HasKey:   func(c config.Config) bool { return secrets.Get(c.AI.Provider) != "" },
	})
	a.wat = watcher.New(watcher.Callbacks{
		OnForeground: a.onForeground,
		OnIdle:       a.onIdle,
		OnBusy:       a.onBusy,
	})
	a.cfg.Subscribe(a.onConfigChanged)

	tray.Start(trayIcon, tray.Actions{
		ToggleVisible: func() bool { v := !a.isHidden(); a.setHidden(v); return !v },
		OpenChat:      func() { a.showFromTray(); a.emit("ui:open", "chat") },
		OpenSettings:  func() { a.showFromTray(); a.emit("ui:open", "settings") },
		TogglePause:   func() bool { p := !a.eng.Paused(); a.eng.SetPaused(p); a.emit("watch:paused", p); return p },
		HideHour:      func() { a.HideFor(60) },
		Quit:          a.Quit,
	})
	a.stopHotkey = sys.Hotkey(win.MOD_CONTROL|win.MOD_ALT, 'P', func() { a.showFromTray(); a.emit("ui:open", "chat") })

	if addr := os.Getenv("PETAI_DEBUG_ADDR"); addr != "" {
		if stop, err := debugapi.Start(addr, debugBackend{a}, a.logf); err != nil {
			a.logf("debug api: %v", err)
		} else {
			a.stopDebug = stop
		}
	}
}

func (a *App) domReady(ctx context.Context) {
	if a.ov != nil {
		return
	}
	mode := os.Getenv("PETAI_CLICKTHROUGH")
	ov, err := overlay.Attach(mode)
	if err != nil {
		a.logf("overlay: %v", err)
		return
	}
	a.ov = ov
	cfg := a.cfg.Get()
	a.applyCapture(cfg)
	a.lastMonitor = cfg.General.Monitor
	m := ov.FitMonitor(cfg.General.Monitor)
	a.mu.Lock()
	ov.SetRegions(a.regions)
	a.mu.Unlock()
	ov.OnCursor = func(x, y float64) { runtime.EventsEmit(ctx, "cursor", x, y) }

	rctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	go ov.Run(rctx)
	go a.wat.Run(rctx)
	go a.eng.Run(rctx)
	a.emit("monitor", m)
	// WebView2 creation activates the host window; give focus back to whoever had it.
	go func() {
		for i := 0; i < 10; i++ {
			if fg := win.ForegroundWindow(); fg == ov.HWND() && a.launchFG != 0 && a.launchFG != ov.HWND() {
				win.SetForegroundWindow(a.launchFG)
			}
			time.Sleep(150 * time.Millisecond)
		}
	}()
	a.logf("overlay attached hwnd=%#x mode=%s monitor=%+v", ov.HWND(), ov.Mode(), m.Bounds)
}

func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.quitting // Alt+F4 on the overlay must not kill the pet; use tray → Keluar
}

func (a *App) shutdown(ctx context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
	if a.stopHotkey != nil {
		a.stopHotkey()
	}
	if a.stopDebug != nil {
		a.stopDebug()
	}
	tray.Stop()
	if a.st != nil {
		_ = a.st.Close()
	}
	a.logf("PetAI stopped")
	a.log.Close()
}

func (a *App) onSecondInstance(_ options.SecondInstanceData) {
	a.showFromTray()
	a.emit("ui:open", "settings")
}

// ---------------- helpers ----------------

func (a *App) provider(cfg config.Config) (ai.Provider, error) {
	pc := cfg.AI.Anthropic
	envBase := os.Getenv("PETAI_ANTHROPIC_BASE_URL")
	if cfg.AI.Provider == "openai" {
		pc = cfg.AI.OpenAI
		envBase = os.Getenv("PETAI_OPENAI_BASE_URL")
	}
	base := pc.BaseURL
	if envBase != "" {
		base = envBase
	}
	return ai.New(ai.Config{Provider: cfg.AI.Provider, Model: pc.Model, BaseURL: base, APIKey: secrets.Get(cfg.AI.Provider)})
}

func (a *App) capture() ([]byte, error) {
	if a.ov == nil {
		return nil, errors.New("overlay not ready")
	}
	b := a.ov.Monitor().Bounds
	return watcher.CaptureJPEG(image.Rect(int(b.X), int(b.Y), int(b.X+b.W), int(b.Y+b.H)), 1280, 70)
}

func (a *App) applyCapture(cfg config.Config) {
	if a.ov == nil {
		return
	}
	exclude := cfg.Privacy.ExcludeFromCapture
	if os.Getenv("PETAI_EXCLUDE_CAPTURE") == "0" {
		exclude = false
	}
	a.ov.SetExcludeFromCapture(exclude)
}

func (a *App) onConfigChanged(cfg config.Config) {
	a.applyCapture(cfg)
	if a.ov != nil && cfg.General.Monitor != a.lastMonitor {
		a.lastMonitor = cfg.General.Monitor
		a.emit("monitor", a.ov.FitMonitor(cfg.General.Monitor))
	}
	if cfg.General.Autostart != sys.AutostartEnabled() {
		if err := sys.SetAutostart(cfg.General.Autostart); err != nil {
			a.logf("autostart: %v", err)
		}
	}
	a.emit("config:changed", cfg)
}

func (a *App) onForeground(f watcher.Foreground) {
	a.eng.OnForeground(brain.Activity{App: f.App, Title: f.Title, Desktop: f.Desktop})
	if a.ov == nil {
		return
	}
	// Window geometry for perching (local UI only, never sent to the AI).
	if f.Desktop || f.Maximized || f.HWND == 0 {
		a.emit("fg:window", nil)
		return
	}
	m := a.ov.Monitor()
	s := m.Scale
	a.emit("fg:window", map[string]float64{
		"x": float64(f.Rect.Left-m.Bounds.X) / s, "y": float64(f.Rect.Top-m.Bounds.Y) / s,
		"w": float64(f.Rect.W()) / s, "h": float64(f.Rect.H()) / s,
	})
}

func (a *App) onIdle(idle time.Duration) {
	before := a.eng.Idle()
	a.eng.OnIdle(idle)
	th := 5 * time.Minute
	if a.fast {
		th /= 60
	}
	if (before >= th) != (idle >= th) {
		a.emit("user:presence", map[string]any{"away": idle >= th, "idleSec": int(idle.Seconds())})
	}
}

func (a *App) onBusy(busy bool) {
	a.eng.OnBusy(busy)
	a.mu.Lock()
	a.busy = busy
	a.mu.Unlock()
	a.applyVisibility()
	a.emit("user:busy", busy)
}

func (a *App) isHidden() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hidden
}

func (a *App) setHidden(h bool) {
	a.mu.Lock()
	a.hidden = h
	if !h && a.hideTimer != nil {
		a.hideTimer.Stop()
		a.hideTimer = nil
	}
	a.mu.Unlock()
	a.applyVisibility()
}

func (a *App) applyVisibility() {
	a.mu.Lock()
	hide := a.hidden || (a.busy && a.cfg.Get().General.RespectFullscreen)
	a.mu.Unlock()
	a.eng.SetHidden(hide)
	if a.ov != nil && a.ov.Visible() == hide {
		a.ov.SetVisible(!hide)
	}
}

func (a *App) showFromTray() {
	if a.isHidden() {
		a.setHidden(false)
	}
}

// ---------------- bound methods (frontend) ----------------

func (a *App) GetBootstrap() Bootstrap {
	cfg := a.cfg.Get()
	b := Bootstrap{
		Config:    cfg,
		Keys:      map[string]string{"anthropic": secrets.Masked("anthropic"), "openai": secrets.Masked("openai")},
		Fast:      a.fast,
		Version:   appVersion,
		InitError: a.initErr,
		Monitors:  len(win.Monitors()),
	}
	if a.lib != nil {
		b.Animations = a.lib.Specs(cfg.Pet.Character)
	}
	if a.ov != nil {
		b.Monitor = a.ov.Monitor()
	}
	return b
}

func (a *App) GetMonitor() overlay.Monitor {
	if a.ov == nil {
		return overlay.Monitor{}
	}
	return a.ov.Monitor()
}

func (a *App) GetAnimations(character string) []anim.Spec {
	if a.lib == nil {
		return nil
	}
	return a.lib.Specs(character)
}

func (a *App) ListAllAnimations() []anim.Meta {
	if a.lib == nil {
		return nil
	}
	return a.lib.All()
}

func (a *App) DeleteAnimation(target, name string) error {
	return a.lib.Delete(target, name)
}

func (a *App) ExportAnimation(target, name string) (string, error) {
	s, ok := a.lib.Lookup(name, target)
	if !ok {
		return "", errors.New("animasi tidak ditemukan")
	}
	b, err := json.MarshalIndent(s, "", "  ")
	return string(b), err
}

func (a *App) ImportAnimation(text string) (anim.Meta, error) {
	var s anim.Spec
	if err := json.Unmarshal([]byte(text), &s); err != nil {
		return anim.Meta{}, errors.New("JSON tidak valid: " + err.Error())
	}
	saved, err := a.lib.Save(s)
	if err != nil {
		return anim.Meta{}, err
	}
	a.emit("pet:anim-added", saved)
	return saved.Meta(), nil
}

func (a *App) SetHitRegions(r []overlay.Rect) {
	a.mu.Lock()
	a.regions = r
	a.mu.Unlock()
	if a.ov != nil {
		a.ov.SetRegions(r)
	}
}

func (a *App) SetForceInteractive(on bool) {
	if a.ov != nil {
		a.ov.SetForceInteractive(on)
	}
}

func (a *App) SetFocusable(on bool) {
	if a.ov != nil {
		a.ov.SetFocusable(on)
	}
}

func (a *App) ReportPetState(s PetState) {
	a.mu.Lock()
	a.pet = s
	a.mu.Unlock()
	if a.ov != nil && s.DPR > 0 {
		a.ov.SetScale(s.DPR)
	}
}

func (a *App) SaveConfig(c config.Config) (config.Config, error) {
	return a.cfg.Set(c)
}

// SetAPIKey stores a key in the Windows Credential Manager and returns its masked form.
func (a *App) SetAPIKey(provider, key string) (string, error) {
	if err := secrets.Set(provider, key); err != nil {
		return "", err
	}
	a.logf("api key updated for %s", provider)
	return secrets.Masked(provider), nil
}

// TestProvider checks a provider with the stored key and lists its models.
func (a *App) TestProvider(provider, baseURL string) TestResult {
	cfg := a.cfg.Get()
	cfg.AI.Provider = provider
	if provider == "openai" {
		cfg.AI.OpenAI.BaseURL = baseURL
	} else {
		cfg.AI.Anthropic.BaseURL = baseURL
	}
	p, err := a.provider(cfg)
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	models, err := p.ListModels(ctx)
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	return TestResult{OK: true, Models: models}
}

func (a *App) Chat(text string) (*brain.PetAction, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	return a.eng.Chat(ctx, text)
}

func (a *App) PetClicked(n int) {
	a.eng.PetClicked(a.ctx, n)
}

func (a *App) ListMemories() ([]store.Memory, error) {
	if a.st == nil {
		return nil, errors.New("database unavailable")
	}
	return a.st.Memories()
}

func (a *App) AddMemory(kind, content string) error {
	_, err := a.st.AddMemory(kind, content, "user", 0.9, time.Now())
	return err
}

func (a *App) UpdateMemory(id int64, kind, content string) error {
	return a.st.UpdateMemory(id, kind, content, time.Now())
}

func (a *App) DeleteMemory(id int64) error { return a.st.DeleteMemory(id) }

func (a *App) WipeMemories() error { return a.st.WipeMemories() }

// WipeAllData deletes activity logs, memories and chat (config, keys and animations are kept).
func (a *App) WipeAllData() error {
	a.logf("user wiped all personal data")
	return a.st.WipeAll()
}

func (a *App) ActivitySummary() (store.Stats, error) {
	return a.st.ActivityStats(time.Now(), 7)
}

func (a *App) HideFor(minutes int) {
	a.setHidden(true)
	a.mu.Lock()
	if a.hideTimer != nil {
		a.hideTimer.Stop()
	}
	a.hideTimer = time.AfterFunc(time.Duration(minutes)*time.Minute, func() { a.setHidden(false) })
	a.mu.Unlock()
}

// LogFrontend records frontend errors in the log file (rate limited).
func (a *App) LogFrontend(msg string) {
	a.mu.Lock()
	now := time.Now()
	if now.Sub(a.feLogAt) < time.Second {
		a.mu.Unlock()
		return
	}
	a.feLogAt = now
	a.mu.Unlock()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	a.logf("frontend: %s", msg)
}

func (a *App) OpenDataFolder() {
	_ = exec.Command("explorer.exe", paths.DataDir()).Start()
}

func (a *App) Quit() {
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	runtime.Quit(a.ctx)
}

// ---------------- debug API backend ----------------

type debugBackend struct{ a *App }

func (d debugBackend) State() any {
	a := d.a
	cfg := a.cfg.Get()
	a.mu.Lock()
	pet := a.pet
	a.mu.Unlock()
	out := map[string]any{"config": cfg}
	winOut := map[string]any{}
	petOut := map[string]any{
		"character": pet.Character, "mode": pet.Mode, "state": pet.State, "animation": pet.Animation,
		"visible": pet.Visible, "x": 0, "y": 0, "w": 0, "h": 0,
		"viewport": map[string]any{"w": pet.ViewW, "h": pet.ViewH, "scrollY": pet.ScrollY, "dpr": pet.DPR},
	}
	if a.ov != nil {
		m := a.ov.Monitor()
		winOut = map[string]any{
			"hwnd": a.ov.HWND(), "exStyle": a.ov.ExStyle(), "clickThrough": a.ov.Mode(),
			"interactive": a.ov.Interactive(), "scale": m.Scale,
			"rect": map[string]any{"x": m.Bounds.X, "y": m.Bounds.Y, "w": m.Bounds.W, "h": m.Bounds.H},
		}
		s := m.Scale
		petOut["x"] = int(pet.X*s) + int(m.Bounds.X)
		petOut["y"] = int(pet.Y*s) + int(m.Bounds.Y)
		petOut["w"] = int(pet.W * s)
		petOut["h"] = int(pet.H * s)
		petOut["visible"] = pet.Visible && a.ov.Visible()
	}
	out["window"] = winOut
	out["pet"] = petOut
	pc := cfg.AI.Anthropic
	if cfg.AI.Provider == "openai" {
		pc = cfg.AI.OpenAI
	}
	out["ai"] = map[string]any{
		"provider": cfg.AI.Provider, "model": pc.Model,
		"hasKey": secrets.Get(cfg.AI.Provider) != "", "callsLastHour": a.eng.CallsLastHour(),
	}
	return out
}

func (d debugBackend) MergeConfig(patch []byte) (any, error) { return d.a.cfg.Merge(patch) }

func (d debugBackend) Trigger(occ string) (any, error) {
	switch occ {
	case "greet", "long_focus", "app_switch", "late_night", "random_chatter", "user_click", "screenshot_insight", "consolidate":
	default:
		return nil, errors.New("unknown occasion")
	}
	ctx, cancel := context.WithTimeout(d.a.ctx, 3*time.Minute)
	defer cancel()
	act, err := d.a.eng.RunOccasion(ctx, occ, true, "")
	if act == nil {
		return nil, err
	}
	return act, err
}

func (d debugBackend) Chat(text string) (any, error) {
	act, err := d.a.Chat(text)
	if act == nil {
		return nil, err
	}
	return act, err
}

func (d debugBackend) Animations() any { return d.a.lib.All() }

func (d debugBackend) Play(name string) error {
	if _, ok := d.a.lib.Lookup(name, d.a.cfg.Get().Pet.Character); !ok {
		return errors.New("unknown animation")
	}
	d.a.emit("pet:play", anim.Slug(name))
	return nil
}

func (d debugBackend) Memories() (any, error) { return d.a.ListMemories() }

func (d debugBackend) Activity(name string) error {
	for _, a := range brain.Activities {
		if a == name {
			d.a.emit("pet:activity", name)
			return nil
		}
	}
	return errors.New("activity must be one of football|basketball|golf|toilet")
}

func (d debugBackend) OpenUI(what string) error {
	switch what {
	case "settings", "chat", "menu":
		d.a.emit("ui:open", what)
		return nil
	}
	return errors.New("open must be settings|chat|menu")
}
