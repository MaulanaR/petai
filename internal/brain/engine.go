// Package brain decides when the pet talks, builds prompts, calls the AI provider and
// applies the structured result (speech, animation, new animations, memories).
package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"petai/internal/ai"
	"petai/internal/anim"
	"petai/internal/config"
	"petai/internal/store"
	"petai/internal/watcher"
)

// Deps are the engine's collaborators (all injectable for tests).
type Deps struct {
	Config   *config.Manager
	Store    *store.Store
	Lib      *anim.Library
	Provider func(cfg config.Config) (ai.Provider, error)
	Emit     func(name string, data any)
	Capture  func() ([]byte, error)
	Now      func() time.Time
	Logf     func(format string, args ...any)
	Fast     bool
	Rand     func() float64
	// HasKey reports whether the selected provider has an API key (nil = assume yes).
	HasKey func(cfg config.Config) bool
	// VoiceProvider returns the provider/model used for voice turns (nil = Provider).
	VoiceProvider func(cfg config.Config) (ai.Provider, error)
	// OpenApp executes an AI request to open a whitelisted app (nil = ignored).
	OpenApp func(req OpenApp)
}

// Activity is the subset of the foreground window the engine cares about.
type Activity struct {
	App     string
	Title   string
	Desktop bool
}

type Engine struct {
	d Deps

	mu          sync.Mutex
	started     time.Time
	greeted     bool
	calls       []time.Time // automatic calls (rolling hour)
	lastAuto    time.Time
	lastByOcc   map[string]time.Time
	appCooldown map[string]time.Time
	fg          Activity
	fgSince     time.Time
	longFocused bool
	session     int64
	sessionApp  string
	sessionTtl  string
	idle        time.Duration
	away        bool
	busy        bool
	hidden      bool
	paused      bool
	talking     bool // a voice conversation is running: no automatic comments
	inflight    int
	mood        string
	clicks      int
	lastShot    time.Time
	genInflight map[string]bool
}

func New(d Deps) *Engine {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Logf == nil {
		d.Logf = func(string, ...any) {}
	}
	if d.Emit == nil {
		d.Emit = func(string, any) {}
	}
	if d.Rand == nil {
		d.Rand = rand.Float64
	}
	return &Engine{
		d: d, started: d.Now(), lastByOcc: map[string]time.Time{}, appCooldown: map[string]time.Time{},
		genInflight: map[string]bool{}, mood: "happy",
	}
}

// dur scales a threshold (PETAI_FAST divides minutes into seconds).
func (e *Engine) dur(x time.Duration) time.Duration {
	if e.d.Fast {
		return x / 60
	}
	return x
}

// TickInterval is how often Tick should be called.
func (e *Engine) TickInterval() time.Duration {
	if e.d.Fast {
		return 500 * time.Millisecond
	}
	return 15 * time.Second
}

// ---- inputs ----

func (e *Engine) OnForeground(a Activity) {
	now := e.d.Now()
	e.mu.Lock()
	if a.App != e.fg.App || a.Desktop != e.fg.Desktop {
		e.fgSince = now
		e.longFocused = false
	}
	e.fg = a
	e.mu.Unlock()
	e.logActivity(now)
}

func (e *Engine) OnIdle(idle time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.idle = idle
	if idle >= e.dur(10*time.Minute) {
		e.away = true
	}
}

func (e *Engine) Idle() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.idle
}

func (e *Engine) OnBusy(b bool) {
	e.mu.Lock()
	e.busy = b
	e.mu.Unlock()
}

func (e *Engine) SetHidden(h bool) {
	e.mu.Lock()
	e.hidden = h
	e.mu.Unlock()
}

// SetTalking suppresses automatic occasions while the user is in a voice conversation.
func (e *Engine) SetTalking(t bool) {
	e.mu.Lock()
	e.talking = t
	e.mu.Unlock()
}

// SetPaused stops activity observation-driven occasions (tray "Pause pengamatan").
func (e *Engine) SetPaused(p bool) {
	e.mu.Lock()
	e.paused = p
	e.mu.Unlock()
}

func (e *Engine) Paused() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.paused
}

// CallsLastHour returns the number of automatic AI calls in the rolling window.
func (e *Engine) CallsLastHour() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.countCalls(e.d.Now())
}

func (e *Engine) countCalls(now time.Time) int {
	win := e.dur(time.Hour)
	kept := e.calls[:0]
	for _, t := range e.calls {
		if now.Sub(t) < win {
			kept = append(kept, t)
		}
	}
	e.calls = kept
	return len(kept)
}

// ---- decision ----

func (e *Engine) watching(c config.Config) bool {
	return c.Privacy.WatchActivity && !e.paused
}

func (e *Engine) blocked(c config.Config) bool {
	return watcher.Blocked(e.fg.App, e.fg.Title, c.Privacy.Blocklist)
}

// Decide returns the automatic occasion to run now, or "".
func (e *Engine) Decide() string {
	now := e.d.Now()
	c := e.d.Config.Get()
	e.mu.Lock()
	defer e.mu.Unlock()
	if !c.AI.Enabled || e.inflight > 0 || e.hidden || e.talking {
		return ""
	}
	if e.d.HasKey != nil && !e.d.HasKey(c) {
		return ""
	}
	if e.busy && c.General.RespectFullscreen {
		return ""
	}
	if !e.lastAuto.IsZero() && now.Sub(e.lastAuto) < e.dur(3*time.Minute) {
		return ""
	}
	if e.countCalls(now) >= c.AI.MaxCallsPerHour {
		return ""
	}
	present := e.idle < e.dur(5*time.Minute)
	if e.away && e.idle < e.dur(time.Minute) {
		e.away = false
		return "greet"
	}
	if !present {
		return ""
	}
	if !e.greeted && now.Sub(e.started) >= e.dur(10*time.Second) {
		e.greeted = true
		return "greet"
	}
	if e.watching(c) && !e.fg.Desktop && e.fg.App != "" && !e.blocked(c) {
		dwell := now.Sub(e.fgSince)
		if c.ScreenshotsEffective() && now.Sub(e.lastShot) >= e.dur(time.Duration(c.Privacy.ScreenshotIntervalMin)*time.Minute) && dwell >= e.dur(time.Minute) {
			return "screenshot_insight"
		}
		if !e.longFocused && dwell >= e.dur(50*time.Minute) {
			e.longFocused = true
			return "long_focus"
		}
		if dwell >= e.dur(2*time.Minute) && dwell < e.dur(10*time.Minute) {
			if last, ok := e.appCooldown[e.fg.App]; !ok || now.Sub(last) >= e.dur(time.Hour) {
				e.appCooldown[e.fg.App] = now
				return "app_switch"
			}
		}
	}
	h := now.Hour()
	if (h >= 23 || h < 4) && now.Sub(e.lastByOcc["late_night"]) >= e.dur(90*time.Minute) {
		return "late_night"
	}
	if now.Sub(e.lastByOcc["random_chatter"]) >= e.dur(20*time.Minute) && now.Sub(e.started) >= e.dur(5*time.Minute) {
		if e.d.Rand() < 0.02+0.08*c.Movement.Activity {
			return "random_chatter"
		}
	}
	if e.d.Store != nil && now.Sub(e.started) >= e.dur(2*time.Minute) {
		last := e.d.Store.GetMeta("last_consolidate")
		if t, err := time.Parse(time.RFC3339, last); err != nil || now.Sub(t) >= e.dur(24*time.Hour) {
			return "consolidate"
		}
	}
	return ""
}

// Run evaluates rules periodically until ctx is done.
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(e.TickInterval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.logActivity(e.d.Now())
			if occ := e.Decide(); occ != "" {
				go func() {
					if _, err := e.RunOccasion(ctx, occ, false, ""); err != nil {
						e.d.Logf("occasion %s: %v", occ, err)
					}
				}()
			}
		}
	}
}

// PetClicked is called by the UI on clicks; occasionally answers with an AI quip.
func (e *Engine) PetClicked(ctx context.Context, n int) {
	now := e.d.Now()
	c := e.d.Config.Get()
	e.mu.Lock()
	e.clicks = n
	ok := c.AI.Enabled && e.inflight == 0 && now.Sub(e.lastByOcc["user_click"]) >= e.dur(10*time.Minute) &&
		e.countCalls(now) < c.AI.MaxCallsPerHour && e.d.Rand() < 0.35
	e.mu.Unlock()
	if ok {
		go func() { _, _ = e.RunOccasion(ctx, "user_click", false, "") }()
	}
}

// ---- execution ----

var errBusy = errors.New("pet sedang berpikir")

// Chat answers a user message (not rate limited, not counted as automatic).
func (e *Engine) Chat(ctx context.Context, text string) (*PetAction, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("pesan kosong")
	}
	if !e.d.Config.Get().AI.Enabled {
		err := errors.New("AI dinonaktifkan di Pengaturan → AI")
		e.d.Emit("ai:error", err.Error())
		return nil, err
	}
	if len([]rune(text)) > 1000 {
		text = string([]rune(text)[:1000])
	}
	return e.RunOccasion(ctx, "chat", true, text)
}

// Voice answers a spoken message: the recording goes straight to the voice model, which also
// returns what it heard. Not rate limited.
func (e *Engine) Voice(ctx context.Context, wav []byte) (*PetAction, error) {
	if len(wav) == 0 {
		return nil, errors.New("rekaman kosong")
	}
	return e.runTurn(ctx, "voice", true, "", wav)
}

// RunOccasion performs one AI turn. force=true bypasses rate limits and is not counted
// as an automatic call (debug triggers, chat).
func (e *Engine) RunOccasion(ctx context.Context, occ string, force bool, userText string) (*PetAction, error) {
	return e.runTurn(ctx, occ, force, userText, nil)
}

func (e *Engine) runTurn(ctx context.Context, occ string, force bool, userText string, audio []byte) (*PetAction, error) {
	now := e.d.Now()
	cfg := e.d.Config.Get()
	if !cfg.AI.Enabled {
		return nil, errors.New("AI dinonaktifkan di Pengaturan → AI")
	}
	e.mu.Lock()
	if e.inflight > 0 && occ != "chat" && occ != "voice" && !force {
		e.mu.Unlock()
		return nil, errBusy
	}
	e.inflight++
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.inflight--
		e.mu.Unlock()
	}()

	prov, err := e.d.Provider(cfg)
	if err != nil {
		if occ == "chat" || occ == "voice" {
			e.d.Emit("ai:error", err.Error())
		}
		return nil, err
	}
	turnProv := prov
	if occ == "voice" && e.d.VoiceProvider != nil {
		if turnProv, err = e.d.VoiceProvider(cfg); err != nil {
			return nil, err
		}
	}
	// Only count calls that actually reach a provider.
	e.mu.Lock()
	if !force {
		e.calls = append(e.calls, now)
		e.lastAuto = now
	}
	e.lastByOcc[occ] = now
	e.mu.Unlock()

	if occ == "consolidate" {
		return nil, e.consolidate(ctx, prov, cfg)
	}
	e.d.Emit("ai:thinking", true)
	defer e.d.Emit("ai:thinking", false)

	var image []byte
	if occ == "screenshot_insight" {
		image = e.screenshot(cfg)
		e.mu.Lock()
		e.lastShot = now
		e.mu.Unlock()
	}
	userMsg := "[task:pet_action]\n" + e.contextJSON(cfg, occ, userText, image != nil)
	res, err := turnProv.Generate(ctx, ai.Request{
		System:     []string{personaPrompt(cfg), catalogPrompt(e.d.Lib.Catalog(cfg.Pet.Character)) + appsPrompt(cfg.Launcher.Apps)},
		User:       userMsg,
		Image:      image,
		Audio:      audio,
		SchemaName: "PetAction",
		Schema:     PetActionSchema,
		Effort:     "low",
		MaxTokens:  4096,
	})
	if err != nil {
		if occ == "chat" || occ == "voice" {
			e.d.Emit("ai:error", err.Error())
		}
		return nil, err
	}
	var act PetAction
	if err := json.Unmarshal([]byte(res.Text), &act); err != nil {
		return nil, fmt.Errorf("invalid PetAction JSON: %w", err)
	}
	act.Sanitize()
	e.d.Logf("ai %s ok model=%s in=%d out=%d cache=%d", occ, res.Model, res.InputTokens, res.OutputTokens, res.CacheRead)

	if occ != "voice" {
		act.Heard, act.EndVoice = "", false
	} else {
		act.Heard = watcher.Redact(act.Heard)
	}
	if occ == "chat" && e.d.Store != nil {
		_ = e.d.Store.AppendChat("user", userText, now)
	}
	if occ == "voice" && act.Heard != "" && e.d.Store != nil {
		_ = e.d.Store.AppendChat("user", act.Heard, now)
	}
	if act.Speech != "" && e.d.Store != nil {
		_ = e.d.Store.AppendChat("pet", act.Speech, now)
	}
	e.applyMemoryOps(act.MemoryOps)
	e.mu.Lock()
	e.mood = act.Mood
	e.mu.Unlock()

	// Resolve animation: catalog name → play; unknown → drop.
	emitted := act
	if emitted.Animation != "" {
		if _, ok := e.d.Lib.Lookup(emitted.Animation, cfg.Pet.Character); !ok {
			emitted.Animation = ""
		} else {
			emitted.Animation = anim.Slug(emitted.Animation)
		}
	}
	var gen *AnimReq
	if r := act.NewAnimationRequest; r != nil {
		name := anim.Slug(r.Name)
		if _, ok := e.d.Lib.Lookup(name, cfg.Pet.Character); ok {
			emitted.Animation = name // cached: reuse without generating
		} else if name != "" {
			gen = &AnimReq{Name: name, Description: r.Description}
		}
	}
	e.d.Emit("pet:action", map[string]any{"occasion": occ, "action": emitted})

	if act.OpenApp != nil && e.d.OpenApp != nil {
		e.d.OpenApp(*act.OpenApp)
	}
	if gen != nil {
		if spec, err := e.generateAnimation(ctx, prov, cfg, *gen); err != nil {
			e.d.Logf("animation %s rejected: %v", gen.Name, err)
		} else {
			e.d.Emit("pet:anim-added", spec)
			e.d.Emit("pet:play", spec.Name)
		}
	}
	return &act, nil
}

func (e *Engine) screenshot(cfg config.Config) []byte {
	if !cfg.ScreenshotsEffective() || e.d.Capture == nil {
		return nil
	}
	e.mu.Lock()
	blocked := e.fg.Desktop || e.blocked(cfg)
	e.mu.Unlock()
	if blocked {
		return nil
	}
	e.d.Emit("pet:watching", true)
	defer e.d.Emit("pet:watching", false)
	time.Sleep(e.dur(3 * time.Second)) // heads-up (glasses on) before capturing
	img, err := e.d.Capture()
	if err != nil {
		e.d.Logf("capture: %v", err)
		return nil
	}
	return img
}

func (e *Engine) contextJSON(cfg config.Config, occ, userText string, hasImage bool) string {
	now := e.d.Now()
	e.mu.Lock()
	fg, since, idle, mood, clicks := e.fg, e.fgSince, e.idle, e.mood, e.clicks
	watching := e.watching(cfg)
	blocked := e.blocked(cfg)
	e.mu.Unlock()

	ctx := map[string]any{
		"occasion":  occ,
		"localTime": now.Format("2006-01-02 15:04 (Monday)"),
		"pet": map[string]any{
			"name": cfg.Pet.Name, "character": cfg.Pet.Character, "mood": mood, "movementMode": cfg.Movement.Mode,
		},
		"user": map[string]any{"idleSeconds": int(idle.Seconds())},
	}
	if watching && !fg.Desktop && fg.App != "" && !blocked {
		ctx["activity"] = map[string]any{
			"app":          fg.App,
			"title":        watcher.Redact(fg.Title),
			"minutesInApp": int(now.Sub(since).Minutes()),
		}
	}
	if hasImage {
		ctx["screenshot"] = "attached: downscaled image of the user's screen (pet itself is hidden)"
	}
	if watching && e.d.Store != nil {
		if st, err := e.d.Store.ActivityStats(now, 1); err == nil && len(st.TodayTopApps) > 0 {
			ctx["todayTopApps"] = st.TodayTopApps
		}
	}
	if e.d.Store != nil {
		if mems, err := e.d.Store.TopMemories(12, now); err == nil && len(mems) > 0 {
			list := make([]map[string]any, 0, len(mems))
			for _, m := range mems {
				list = append(list, map[string]any{"id": m.ID, "kind": m.Kind, "content": m.Content})
			}
			ctx["memories"] = list
		}
		if chat, err := e.d.Store.RecentChat(8); err == nil && len(chat) > 0 {
			conv := make([]map[string]string, 0, len(chat))
			for _, c := range chat {
				conv = append(conv, map[string]string{"from": c.Role, "text": c.Text})
			}
			ctx["recentConversation"] = conv
		}
	}
	if occ == "chat" {
		ctx["userMessage"] = userText
	}
	if occ == "voice" {
		ctx["userMessage"] = "(spoken - listen to the attached audio and put the exact transcript in heard)"
	}
	if occ == "user_click" {
		ctx["clicksJustNow"] = clicks
	}
	b, _ := json.Marshal(ctx)
	return string(b)
}

func (e *Engine) applyMemoryOps(ops []MemoryOp) {
	if e.d.Store == nil {
		return
	}
	now := e.d.Now()
	changed := false
	for _, op := range ops {
		content := watcher.Redact(op.Content)
		if strings.Contains(content, "[secret]") || strings.Contains(content, "[email]") || strings.Contains(content, "[num]") {
			continue
		}
		switch op.Op {
		case "add":
			if _, err := e.d.Store.AddMemory(op.Kind, content, "ai", 0.6, now); err == nil {
				changed = true
			}
		case "update":
			if op.ID > 0 && e.d.Store.UpdateMemory(op.ID, op.Kind, content, now) == nil {
				changed = true
			}
		case "forget":
			if op.ID > 0 && e.d.Store.DeleteMemory(op.ID) == nil {
				changed = true
			}
		}
	}
	if changed {
		e.d.Emit("memory:changed", true)
	}
}

func (e *Engine) generateAnimation(ctx context.Context, prov ai.Provider, cfg config.Config, req AnimReq) (anim.Spec, error) {
	e.mu.Lock()
	if e.genInflight[req.Name] {
		e.mu.Unlock()
		return anim.Spec{}, errors.New("already generating")
	}
	e.genInflight[req.Name] = true
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.genInflight, req.Name)
		e.mu.Unlock()
	}()
	// Another turn may have produced it meanwhile.
	if s, ok := e.d.Lib.Lookup(req.Name, cfg.Pet.Character); ok {
		return s, nil
	}
	in, _ := json.Marshal(map[string]any{
		"name": req.Name, "description": req.Description, "character": cfg.Pet.Character,
	})
	res, err := prov.Generate(ctx, ai.Request{
		System:     []string{animationSystemPrompt(cfg.Pet.Character)},
		User:       "[task:animation_spec]\n" + string(in),
		SchemaName: "AnimationSpec",
		Schema:     AnimationSpecSchema,
		Effort:     "medium",
		MaxTokens:  12000,
	})
	if err != nil {
		return anim.Spec{}, err
	}
	var spec anim.Spec
	if err := json.Unmarshal([]byte(res.Text), &spec); err != nil {
		return anim.Spec{}, fmt.Errorf("invalid AnimationSpec JSON: %w", err)
	}
	spec.Name = req.Name
	if spec.Target != "generic" {
		spec.Target = cfg.Pet.Character
	}
	if strings.TrimSpace(spec.Description) == "" {
		spec.Description = req.Description
	}
	saved, err := e.d.Lib.Save(spec)
	if err != nil {
		return anim.Spec{}, err
	}
	e.d.Logf("animation %s/%s generated and saved", saved.Target, saved.Name)
	return saved, nil
}

func (e *Engine) consolidate(ctx context.Context, prov ai.Provider, cfg config.Config) error {
	if e.d.Store == nil {
		return nil
	}
	now := e.d.Now()
	_ = e.d.Store.SetMeta("last_consolidate", now.Format(time.RFC3339))
	input := map[string]any{}
	if cfg.Privacy.WatchActivity {
		if st, err := e.d.Store.ActivityStats(now, 7); err == nil && st.ActiveDays > 0 {
			input["usageStats7d"] = st
		}
	}
	if chat, err := e.d.Store.RecentChat(30); err == nil && len(chat) > 0 {
		input["recentConversation"] = chat
	}
	mems, _ := e.d.Store.Memories()
	input["memories"] = mems
	if len(input) == 1 && len(mems) == 0 {
		return nil // nothing to learn from yet
	}
	b, _ := json.Marshal(input)
	res, err := prov.Generate(ctx, ai.Request{
		System:     []string{memorySystemPrompt(cfg)},
		User:       "[task:memory_ops]\n" + string(b),
		SchemaName: "MemoryOps",
		Schema:     MemoryOpsSchema,
		Effort:     "medium",
		MaxTokens:  6000,
	})
	if err != nil {
		return err
	}
	var r memoryOpsReply
	if err := json.Unmarshal([]byte(res.Text), &r); err != nil {
		return fmt.Errorf("invalid MemoryOps JSON: %w", err)
	}
	if len(r.MemoryOps) > 6 {
		r.MemoryOps = r.MemoryOps[:6]
	}
	e.applyMemoryOps(r.MemoryOps)
	return nil
}

// logActivity keeps the local activity log in sync (only when watching is allowed).
func (e *Engine) logActivity(now time.Time) {
	if e.d.Store == nil {
		return
	}
	cfg := e.d.Config.Get()
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.watching(cfg) || e.fg.Desktop || e.fg.App == "" || e.blocked(cfg) || e.idle >= e.dur(5*time.Minute) {
		e.session = 0
		return
	}
	title := watcher.Redact(e.fg.Title)
	if e.session != 0 && e.sessionApp == e.fg.App && e.sessionTtl == title {
		_ = e.d.Store.TouchSession(e.session, now)
		return
	}
	if id, err := e.d.Store.StartSession(e.fg.App, title, now); err == nil {
		e.session, e.sessionApp, e.sessionTtl = id, e.fg.App, title
	}
}
