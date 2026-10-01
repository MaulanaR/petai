// petai-check verifies a BYOK AI setup end-to-end without opening the pet window:
// it lists models, sends one chat message and asks for one comment about the current
// foreground window — through the exact same prompt/schema/parsing path as the app.
// The API key is read from Windows Credential Manager and never printed.
//
//	go run ./cmd/petai-check -provider openai -model gpt-x -base https://host/v1 -chat "halo!"
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"petai/internal/ai"
	"petai/internal/anim"
	"petai/internal/brain"
	"petai/internal/config"
	"petai/internal/paths"
	"petai/internal/secrets"
	"petai/internal/store"
	"petai/internal/voice"
	"petai/internal/watcher"
)

func main() {
	provider := flag.String("provider", "anthropic", "anthropic | openai")
	model := flag.String("model", "", "model id (default: app default)")
	base := flag.String("base", "", "base URL (optional)")
	chat := flag.String("chat", "Halo! Kamu bisa dengar aku?", "chat message to send")
	activity := flag.Bool("activity", true, "also ask for a comment about the current foreground window")
	lang := flag.String("lang", "id", "id | en")
	fgApp := flag.String("fg-app", "", "simulate the foreground app (exe name) instead of reading it")
	fgTitle := flag.String("fg-title", "", "simulated foreground window title (with -fg-app)")
	voiceProbe := flag.Bool("voice", false, "only test whether the model can hear audio (voice mode capability) and exit")
	save := flag.Bool("save", false, "after a successful check, write provider/model/base (and watchActivity=true) into the app's config.json")
	flag.Parse()

	key := secrets.Get(*provider)
	if key == "" {
		fail("no API key for %q in Credential Manager (set it in PetAI → Pengaturan → AI)", *provider)
	}
	fmt.Printf("key        : found (%s)\n", secrets.Mask(key))

	tmp, err := os.MkdirTemp("", "petai-check-*")
	if err != nil {
		fail("%v", err)
	}
	defer os.RemoveAll(tmp)
	cm, err := config.Load(filepath.Join(tmp, "config.json"))
	if err != nil {
		fail("%v", err)
	}
	patch := map[string]any{
		"pet":     map[string]any{"language": *lang},
		"ai":      map[string]any{"enabled": true, "provider": *provider, *provider: map[string]any{"model": *model, "baseURL": *base}},
		"privacy": map[string]any{"watchActivity": true},
	}
	pb, _ := json.Marshal(patch)
	cfg, err := cm.Merge(pb)
	if err != nil {
		fail("%v", err)
	}
	pc := cfg.AI.Anthropic
	if *provider == "openai" {
		pc = cfg.AI.OpenAI
	}
	fmt.Printf("provider   : %s  model=%q  base=%q\n", *provider, pc.Model, pc.BaseURL)

	newProv := func(c config.Config) (ai.Provider, error) {
		p := c.AI.Anthropic
		if c.AI.Provider == "openai" {
			p = c.AI.OpenAI
		}
		return ai.New(ai.Config{Provider: c.AI.Provider, Model: p.Model, BaseURL: p.BaseURL, APIKey: key})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	if *voiceProbe {
		p, err := newProv(cfg)
		if err != nil {
			fail("provider: %v", err)
		}
		r := voice.Probe(ctx, p)
		fmt.Printf("voice      : supported=%v heard=%q\n", r.Supported, r.Heard)
		if r.Error != "" {
			fmt.Printf("  error    : %s\n", r.Error)
		}
		if len(r.Suggestions) > 0 {
			fmt.Printf("  try      : %s\n", strings.Join(r.Suggestions, ", "))
		}
		return
	}

	// 1. connection
	prov, err := newProv(cfg)
	if err != nil {
		fail("provider: %v", err)
	}
	t0 := time.Now()
	models, err := prov.ListModels(ctx)
	if err != nil {
		fmt.Printf("models     : FAILED (%v) — continuing, some endpoints do not list models\n", err)
	} else {
		found := false
		for _, m := range models {
			if m == pc.Model {
				found = true
			}
		}
		fmt.Printf("models     : OK, %d listed in %v; selected model listed=%v\n", len(models), time.Since(t0).Round(time.Millisecond), found)
	}

	lib, err := anim.Open(filepath.Join(tmp, "animations"))
	if err != nil {
		fail("%v", err)
	}
	st, err := store.Open(filepath.Join(tmp, "petai.db"))
	if err != nil {
		fail("%v", err)
	}
	defer st.Close()
	eng := brain.New(brain.Deps{
		Config: cm, Store: st, Lib: lib, Provider: newProv,
		Logf: func(f string, a ...any) { fmt.Printf("  log      : "+f+"\n", a...) },
	})

	// 2. chat
	t0 = time.Now()
	act, err := eng.Chat(ctx, *chat)
	if err != nil {
		fail("chat: %v", err)
	}
	fmt.Printf("\nCHAT (%v)\n  you      : %s\n", time.Since(t0).Round(time.Millisecond), *chat)
	printAction(act)

	if *save {
		defer saveConfig(*provider, pc.Model, pc.BaseURL)
	}

	// 3. activity comment
	if !*activity {
		return
	}
	w := watcher.New(watcher.Callbacks{})
	fg, ok := w.Current()
	if *fgApp != "" {
		fg, ok = watcher.Foreground{App: *fgApp, Title: *fgTitle}, true
	}
	if !ok || fg.Desktop || fg.App == "" {
		fmt.Println("\nACTIVITY: foreground is the desktop/own window — focus an app and rerun")
		return
	}
	if watcher.Blocked(fg.App, fg.Title, cfg.Privacy.Blocklist) {
		fmt.Printf("\nACTIVITY: foreground app %q is on the privacy blocklist — nothing is sent (by design)\n", fg.App)
		return
	}
	eng.OnForeground(brain.Activity{App: fg.App, Title: fg.Title})
	t0 = time.Now()
	act, err = eng.RunOccasion(ctx, "app_switch", true, "")
	if err != nil {
		fail("activity comment: %v", err)
	}
	fmt.Printf("\nACTIVITY COMMENT (%v)\n  sent     : app=%s title=%q\n", time.Since(t0).Round(time.Millisecond), fg.App, watcher.Redact(fg.Title))
	printAction(act)
}

// saveConfig applies the verified AI settings to the real app config (keeps everything else).
func saveConfig(provider, model, base string) {
	m, err := config.Load(filepath.Join(paths.DataDir(), "config.json"))
	if err != nil {
		fmt.Printf("\nsave       : FAILED (%v)\n", err)
		return
	}
	patch, _ := json.Marshal(map[string]any{
		"ai":      map[string]any{"enabled": true, "provider": provider, provider: map[string]any{"model": model, "baseURL": base}},
		"privacy": map[string]any{"watchActivity": true},
	})
	if _, err := m.Merge(patch); err != nil {
		fmt.Printf("\nsave       : FAILED (%v)\n", err)
		return
	}
	fmt.Printf("\nsave       : settings written to %s\n", filepath.Join(paths.DataDir(), "config.json"))
}

func printAction(a *brain.PetAction) {
	fmt.Printf("  pet      : %s\n  mood     : %s   animation: %q\n", a.Speech, a.Mood, a.Animation)
	if a.Suggestion != "" {
		fmt.Printf("  saran    : %s\n", a.Suggestion)
	}
	if a.NewAnimationRequest != nil {
		fmt.Printf("  new anim : %s — %s\n", a.NewAnimationRequest.Name, a.NewAnimationRequest.Description)
	}
	for _, m := range a.MemoryOps {
		fmt.Printf("  memory   : %s %s: %s\n", m.Op, m.Kind, m.Content)
	}
}

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+f+"\n", a...)
	os.Exit(1)
}
