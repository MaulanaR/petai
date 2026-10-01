package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"petai/internal/ai"
	"petai/internal/audio"
	"petai/internal/brain"
	"petai/internal/config"
	"petai/internal/launcher"
	"petai/internal/secrets"
	"petai/internal/voice"
)

// VoiceInfo is what the frontend needs to decide whether double-click starts voice mode.
type VoiceInfo struct {
	Ready     bool     `json:"ready"`
	Supported bool     `json:"supported"`
	Checking  bool     `json:"checking"`
	Model     string   `json:"model"`
	Error     string   `json:"error"`
	Suggest   []string `json:"suggest"`
	CheckedAt string   `json:"checkedAt"`
	Active    bool     `json:"active"`
}

type voiceState struct {
	mu       sync.Mutex
	checking string // key being probed
	cancel   context.CancelFunc
	spoke    chan struct{}
	phase    string
	pending  map[string]launcher.Request
	debounce *time.Timer
}

// ---------------- provider helpers ----------------

func (a *App) effectiveBase(cfg config.Config) string {
	if cfg.AI.Provider == "openai" {
		if v := os.Getenv("PETAI_OPENAI_BASE_URL"); v != "" {
			return v
		}
		return cfg.AI.OpenAI.BaseURL
	}
	if v := os.Getenv("PETAI_ANTHROPIC_BASE_URL"); v != "" {
		return v
	}
	return cfg.AI.Anthropic.BaseURL
}

func voiceModel(cfg config.Config) string {
	if cfg.AI.Voice.Model != "" {
		return cfg.AI.Voice.Model
	}
	if cfg.AI.Provider == "openai" {
		return cfg.AI.OpenAI.Model
	}
	return cfg.AI.Anthropic.Model
}

func (a *App) voiceProvider(cfg config.Config) (ai.Provider, error) {
	return ai.New(ai.Config{Provider: cfg.AI.Provider, Model: voiceModel(cfg), BaseURL: a.effectiveBase(cfg), APIKey: secrets.Get(cfg.AI.Provider)})
}

func (a *App) voiceKey(cfg config.Config) string { return cfg.VoiceKey(a.effectiveBase(cfg)) }

func (a *App) voiceInfo(cfg config.Config) VoiceInfo {
	key := a.voiceKey(cfg)
	v := cfg.AI.Voice
	a.vs.mu.Lock()
	checking := a.vs.checking == key
	active := a.vs.cancel != nil
	a.vs.mu.Unlock()
	info := VoiceInfo{Model: voiceModel(cfg), Checking: checking, Active: active, Suggest: []string{}}
	if v.Key == key {
		info.Supported, info.Error, info.CheckedAt = v.Supported, v.Error, v.CheckedAt
		if v.Suggest != nil {
			info.Suggest = v.Suggest
		}
	}
	info.Ready = cfg.VoiceReady(key)
	return info
}

func (a *App) emitVoiceInfo() { a.emit("voice:capability", a.voiceInfo(a.cfg.Get())) }

// ---------------- capability check ----------------

// checkVoice probes the voice model unless a result for the current provider/endpoint/model
// already exists (force re-tests). The result is stored in config.ai.voice.
func (a *App) checkVoice(force bool) voice.Result {
	cfg := a.cfg.Get()
	key := a.voiceKey(cfg)
	if !force && cfg.AI.Voice.Key == key && cfg.AI.Voice.CheckedAt != "" {
		return voice.Result{Supported: cfg.AI.Voice.Supported, Error: cfg.AI.Voice.Error}
	}
	if secrets.Get(cfg.AI.Provider) == "" || voiceModel(cfg) == "" {
		a.emitVoiceInfo()
		return voice.Result{Error: "API key / model belum diatur"}
	}
	a.vs.mu.Lock()
	if a.vs.checking == key {
		a.vs.mu.Unlock()
		return voice.Result{Error: "sedang dites"}
	}
	a.vs.checking = key
	a.vs.mu.Unlock()
	a.emitVoiceInfo()

	res := voice.Result{Error: "provider error"}
	if p, err := a.voiceProvider(cfg); err == nil {
		res = voice.Probe(a.ctx, p)
	} else {
		res.Error = err.Error()
	}
	a.logf("voice capability %s: supported=%v err=%q heard=%q", voiceModel(cfg), res.Supported, res.Error, res.Heard)

	a.vs.mu.Lock()
	a.vs.checking = ""
	a.vs.mu.Unlock()
	// Only store when the settings still point at the model we tested.
	if a.voiceKey(a.cfg.Get()) == key {
		next := a.cfg.Get()
		next.AI.Voice.Supported = res.Supported
		next.AI.Voice.Key = key
		next.AI.Voice.CheckedAt = time.Now().Format(time.RFC3339)
		next.AI.Voice.Error = res.Error
		next.AI.Voice.Suggest = res.Suggestions
		_, _ = a.cfg.Set(next)
	}
	a.emitVoiceInfo()
	return res
}

// scheduleVoiceCheck probes once the provider/model settings have been stable for a moment.
func (a *App) scheduleVoiceCheck() {
	a.vs.mu.Lock()
	defer a.vs.mu.Unlock()
	if a.vs.debounce != nil {
		a.vs.debounce.Stop()
	}
	a.vs.debounce = time.AfterFunc(1500*time.Millisecond, func() { a.checkVoice(false) })
	go a.emitVoiceInfo()
}

// keepVoiceResult stops a settings save from clobbering the stored capability result.
func (a *App) keepVoiceResult(c *config.Config) {
	cur := a.cfg.Get().AI.Voice
	c.AI.Voice.Supported, c.AI.Voice.Key, c.AI.Voice.CheckedAt = cur.Supported, cur.Key, cur.CheckedAt
	c.AI.Voice.Error, c.AI.Voice.Suggest = cur.Error, cur.Suggest
}

// RecheckVoice re-runs the capability test (Settings → Voice → "Tes ulang").
func (a *App) RecheckVoice() VoiceInfo {
	a.checkVoice(true)
	return a.voiceInfo(a.cfg.Get())
}

func (a *App) GetVoiceInfo() VoiceInfo { return a.voiceInfo(a.cfg.Get()) }

// ---------------- voice session ----------------

// StartVoice begins a hands-free voice conversation. Returns "" or a reason it can't start.
func (a *App) StartVoice() string {
	cfg := a.cfg.Get()
	if !cfg.VoiceReady(a.voiceKey(cfg)) {
		return "voice tidak tersedia untuk model ini"
	}
	a.vs.mu.Lock()
	if a.vs.cancel != nil {
		a.vs.mu.Unlock()
		return ""
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.vs.cancel = cancel
	a.vs.spoke = make(chan struct{}, 1)
	a.vs.mu.Unlock()
	go a.voiceLoop(ctx)
	return ""
}

// StopVoice ends the voice conversation and closes the microphone.
func (a *App) StopVoice() {
	a.vs.mu.Lock()
	if a.vs.cancel != nil {
		a.vs.cancel()
	}
	a.vs.mu.Unlock()
}

// VoiceSpoken tells the loop the pet finished (or skipped) speaking its reply.
func (a *App) VoiceSpoken() {
	a.vs.mu.Lock()
	ch := a.vs.spoke
	a.vs.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (a *App) setVoicePhase(p string) {
	a.vs.mu.Lock()
	a.vs.phase = p
	a.vs.mu.Unlock()
	a.emit("voice:state", p)
}

func (a *App) voiceLoop(ctx context.Context) {
	a.eng.SetTalking(true)
	defer func() {
		a.eng.SetTalking(false)
		a.vs.mu.Lock()
		a.vs.cancel, a.vs.spoke = nil, nil
		a.vs.mu.Unlock()
		a.setVoicePhase("idle")
		a.logf("voice session ended")
	}()
	a.vs.mu.Lock()
	spoke := a.vs.spoke
	a.vs.mu.Unlock()
	a.logf("voice session started (%s)", voiceModel(a.cfg.Get()))
	for {
		cfg := a.cfg.Get()
		// Drop a stale "spoken" signal (barge-in + the TTS promise can both report it).
		select {
		case <-spoke:
		default:
		}
		a.setVoicePhase("listening")
		var lastLevel time.Time
		wav, err := audio.Listen(ctx, audio.Options{
			SilenceMs:  cfg.AI.Voice.SilenceMs,
			WaitSpeech: 30 * time.Second,
			OnLevel: func(rms float64, speaking bool) {
				if time.Since(lastLevel) < 60*time.Millisecond {
					return
				}
				lastLevel = time.Now()
				lvl := rms / 3000
				if lvl > 1 {
					lvl = 1
				}
				a.emit("voice:level", map[string]any{"level": lvl, "speaking": speaking})
			},
		})
		if err != nil {
			if errors.Is(err, audio.ErrNoSpeech) {
				a.emit("voice:end", "timeout")
			} else if ctx.Err() == nil {
				a.emit("voice:error", err.Error())
			}
			return
		}
		a.setVoicePhase("thinking")
		act, err := a.eng.Voice(ctx, wav)
		if err != nil {
			if errors.Is(err, ai.ErrAudioUnsupported) {
				next := a.cfg.Get()
				next.AI.Voice.Supported, next.AI.Voice.Error = false, err.Error()
				_, _ = a.cfg.Set(next)
				a.emitVoiceInfo()
			}
			if ctx.Err() == nil {
				a.emit("voice:error", err.Error())
			}
			return
		}
		a.setVoicePhase("speaking")
		select {
		case <-ctx.Done():
			return
		case <-spoke:
		case <-time.After(90 * time.Second):
		}
		if act.EndVoice || !cfg.AI.Voice.Continuous {
			a.emit("voice:end", "done")
			return
		}
	}
}

// ---------------- launcher ----------------

func (a *App) openApp(req brain.OpenApp) {
	lreq := launcher.Request{AppID: req.AppID, Query: req.Query}
	if req.Document != nil {
		lreq.Doc = &launcher.Doc{Title: req.Document.Title, Content: req.Document.Content}
	}
	cfg := a.cfg.Get()
	app, ok := launcher.Find(cfg.Launcher.Apps, req.AppID)
	if !ok {
		a.emit("launcher:error", map[string]string{"app": req.AppID, "error": launcher.ErrUnknownApp.Error()})
		return
	}
	if cfg.Launcher.Confirm {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		token := hex.EncodeToString(b)
		a.vs.mu.Lock()
		if a.vs.pending == nil {
			a.vs.pending = map[string]launcher.Request{}
		}
		a.vs.pending[token] = lreq
		a.vs.mu.Unlock()
		a.emit("launcher:confirm", map[string]string{"token": token, "app": app.Name})
		return
	}
	_, _ = a.doLaunch(lreq)
}

func (a *App) doLaunch(lreq launcher.Request) (launcher.Result, error) {
	cfg := a.cfg.Get()
	res, err := a.launch.Launch(cfg.Launcher.Apps, cfg.Launcher.DocsFolder, lreq)
	if err != nil {
		a.logf("launcher %s: %v", lreq.AppID, err)
		a.emit("launcher:error", map[string]string{"app": res.App, "error": err.Error()})
		return res, err
	}
	a.logf("launcher opened %s (file=%v clipboard=%v)", res.App, res.File != "", res.Clipboard)
	a.emit("launcher:opened", res)
	return res, nil
}

// ConfirmLaunch answers a pending "open this app?" question (when confirmation is enabled).
func (a *App) ConfirmLaunch(token string, ok bool) {
	a.vs.mu.Lock()
	req, found := a.vs.pending[token]
	delete(a.vs.pending, token)
	a.vs.mu.Unlock()
	if found && ok {
		_, _ = a.doLaunch(req)
	}
}

func (a *App) GetAppPresets() []config.App { return launcher.Presets() }

func (a *App) ListStartMenuApps() []launcher.Shortcut { return launcher.StartMenuShortcuts() }

func (a *App) DefaultDocsFolder() string { return launcher.DefaultDocsFolder() }

// TestLaunch opens an app entry from the settings page (no document, no query).
func (a *App) TestLaunch(app config.App) string {
	if app.ID == "" {
		app.ID = "test"
	}
	_, err := a.launch.Launch([]config.App{app}, "", launcher.Request{AppID: app.ID})
	if err != nil {
		return err.Error()
	}
	return ""
}

// PickProgram shows a file dialog for an .exe / .lnk / any file.
func (a *App) PickProgram() string {
	p, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Pilih aplikasi",
		Filters: []runtime.FileFilter{
			{DisplayName: "Aplikasi (*.exe;*.lnk)", Pattern: "*.exe;*.lnk"},
			{DisplayName: "Semua file", Pattern: "*.*"},
		},
	})
	if err != nil {
		return ""
	}
	return p
}

// MicTest is the result of the settings page microphone check.
type MicTest struct {
	OK     bool    `json:"ok"`
	Speech bool    `json:"speech"`
	Peak   float64 `json:"peak"`
	Error  string  `json:"error"`
}

// TestMic listens for a few seconds and streams "mic:level" so the settings page can show a meter.
// Nothing is recorded or sent anywhere.
func (a *App) TestMic(seconds int) MicTest {
	if seconds <= 0 || seconds > 10 {
		seconds = 4
	}
	a.vs.mu.Lock()
	busy := a.vs.cancel != nil
	a.vs.mu.Unlock()
	if busy {
		return MicTest{Error: "mode voice sedang aktif"}
	}
	ctx, cancel := context.WithTimeout(a.ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	var res MicTest
	var last time.Time
	_, err := audio.Listen(ctx, audio.Options{
		SilenceMs:  1500,
		MaxSeconds: seconds,
		WaitSpeech: time.Duration(seconds) * time.Second,
		OnLevel: func(rms float64, speaking bool) {
			lvl := rms / 3000
			if lvl > 1 {
				lvl = 1
			}
			if lvl > res.Peak {
				res.Peak = lvl
			}
			res.Speech = res.Speech || speaking
			if time.Since(last) >= 60*time.Millisecond {
				last = time.Now()
				a.emit("mic:level", map[string]any{"level": lvl, "speaking": speaking})
			}
		},
	})
	if errors.Is(err, audio.ErrMicUnavailable) {
		res.Error = err.Error()
		return res
	}
	res.OK = true
	return res
}
