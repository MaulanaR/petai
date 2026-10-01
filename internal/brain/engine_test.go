package brain

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"petai/internal/ai"
	"petai/internal/anim"
	"petai/internal/config"
	"petai/internal/store"
)

type fakeProv struct {
	mu       sync.Mutex
	reqs     []ai.Request
	action   string
	animSpec string
	memOps   string
}

func (f *fakeProv) Name() string  { return "fake" }
func (f *fakeProv) Model() string { return "fake-1" }
func (f *fakeProv) ListModels(context.Context) ([]string, error) {
	return []string{"fake-1"}, nil
}
func (f *fakeProv) Generate(_ context.Context, r ai.Request) (ai.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, r)
	switch {
	case strings.HasPrefix(r.User, "[task:animation_spec]"):
		return ai.Response{Text: f.animSpec}, nil
	case strings.HasPrefix(r.User, "[task:memory_ops]"):
		return ai.Response{Text: f.memOps}, nil
	}
	return ai.Response{Text: f.action}, nil
}
func (f *fakeProv) count(tag string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.reqs {
		if strings.HasPrefix(r.User, "[task:"+tag+"]") {
			n++
		}
	}
	return n
}
func (f *fakeProv) last() ai.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1]
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

func setup(t *testing.T, patch string) (*Engine, *fakeProv, *clock, *config.Manager, *anim.Library, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	cm, err := config.Load(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if patch != "" {
		if _, err := cm.Merge([]byte(patch)); err != nil {
			t.Fatal(err)
		}
	}
	lib, err := anim.Open(filepath.Join(dir, "animations"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "petai.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	fp := &fakeProv{
		action:   `{"speech":"Halo!","mood":"happy","animation":"wave","new_animation_request":null,"memory_ops":[],"suggestion":""}`,
		animSpec: `{"name":"qa_spin","description":"spin","tags":["fun"],"target":"generic","duration":1,"loop":false,"tracks":[{"slot":"root","prop":"rotation.y","times":[0,1],"values":[0,6.28],"interp":"smooth"}],"expressions":[],"effects":[]}`,
		memOps:   `{"memory_ops":[{"op":"add","id":0,"kind":"habit","content":"Sering ngoding sore"}]}`,
	}
	ck := &clock{t: time.Date(2026, 10, 1, 14, 0, 0, 0, time.Local)}
	e := New(Deps{
		Config: cm, Store: st, Lib: lib,
		Provider: func(config.Config) (ai.Provider, error) { return fp, nil },
		Capture:  func() ([]byte, error) { return []byte{0xff, 0xd8, 0xff}, nil },
		Now:      ck.now,
		Fast:     true,
		Rand:     func() float64 { return 1 }, // never random chatter
	})
	return e, fp, ck, cm, lib, st
}

func ctxOf(t *testing.T, r ai.Request) map[string]any {
	t.Helper()
	parts := strings.SplitN(r.User, "\n", 2)
	var m map[string]any
	if err := json.Unmarshal([]byte(parts[1]), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDecideGreetRateLimitAndDND(t *testing.T) {
	e, _, ck, cm, _, _ := setup(t, `{"ai":{"maxCallsPerHour":2}}`)
	if occ := e.Decide(); occ != "" {
		t.Fatalf("too early: %q", occ)
	}
	ck.add(time.Second)
	if occ := e.Decide(); occ != "greet" {
		t.Fatalf("want greet, got %q", occ)
	}
	ctx := context.Background()
	_, _ = e.RunOccasion(ctx, "greet", false, "")
	ck.add(time.Second) // < 3 s (fast) min gap
	if occ := e.Decide(); occ != "" {
		t.Fatalf("min gap violated: %q", occ)
	}
	ck.add(10 * time.Second)
	_, _ = e.RunOccasion(ctx, "random_chatter", false, "")
	ck.add(10 * time.Second)
	if e.CallsLastHour() != 2 {
		t.Fatalf("calls=%d", e.CallsLastHour())
	}
	e.OnForeground(Activity{App: "code.exe", Title: "x"})
	if occ := e.Decide(); occ != "" {
		t.Fatalf("rate limit violated: %q", occ)
	}
	// Forced (debug/chat) calls are not counted.
	_, _ = e.RunOccasion(ctx, "chat", true, "hai")
	if e.CallsLastHour() != 2 {
		t.Fatal("forced call counted")
	}
	ck.add(2 * time.Minute) // rolling window (1h/60) expired
	_, _ = cm.Merge([]byte(`{"privacy":{"watchActivity":false}}`))
	e.OnBusy(true)
	if occ := e.Decide(); occ != "" {
		t.Fatalf("DND violated: %q", occ)
	}
}

func TestActivityPrivacyInContext(t *testing.T) {
	e, fp, ck, cm, _, _ := setup(t, "")
	ctx := context.Background()
	e.OnForeground(Activity{App: "code.exe", Title: "main.go — budi@zahir.co.id"})
	ck.add(time.Minute)
	if _, err := e.RunOccasion(ctx, "random_chatter", true, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := ctxOf(t, fp.last())["activity"]; ok {
		t.Fatal("activity sent while watchActivity=false")
	}
	_, _ = cm.Merge([]byte(`{"privacy":{"watchActivity":true}}`))
	_, _ = e.RunOccasion(ctx, "random_chatter", true, "")
	act, ok := ctxOf(t, fp.last())["activity"].(map[string]any)
	if !ok || act["app"] != "code.exe" || strings.Contains(act["title"].(string), "@") {
		t.Fatalf("activity not redacted/sent: %v", act)
	}
	e.OnForeground(Activity{App: "chrome.exe", Title: "BCA - Internet Banking"})
	_, _ = e.RunOccasion(ctx, "random_chatter", true, "")
	if _, ok := ctxOf(t, fp.last())["activity"]; ok {
		t.Fatal("blocklisted activity sent")
	}
	// Screenshots: only with both toggles and non-blocked app.
	e.OnForeground(Activity{App: "code.exe", Title: "main.go"})
	_, _ = e.RunOccasion(ctx, "screenshot_insight", true, "")
	if fp.last().Image != nil {
		t.Fatal("image sent without screenshots toggle")
	}
	_, _ = cm.Merge([]byte(`{"privacy":{"screenshots":true}}`))
	_, _ = e.RunOccasion(ctx, "screenshot_insight", true, "")
	if fp.last().Image == nil {
		t.Fatal("image missing with screenshots enabled")
	}
	_, _ = e.RunOccasion(ctx, "random_chatter", true, "")
	if fp.last().Image != nil {
		t.Fatal("image attached to non-screenshot occasion")
	}
}

func TestNewAnimationGeneratedOnceAndReused(t *testing.T) {
	e, fp, _, _, lib, _ := setup(t, "")
	fp.action = `{"speech":"lihat!","mood":"excited","animation":"","new_animation_request":{"name":"qa_spin","description":"spin around"},"memory_ops":[],"suggestion":""}`
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := e.RunOccasion(ctx, "random_chatter", true, ""); err != nil {
			t.Fatal(err)
		}
	}
	if n := fp.count("animation_spec"); n != 1 {
		t.Fatalf("animation_spec calls = %d, want 1", n)
	}
	if _, ok := lib.Lookup("qa_spin", "cat"); !ok {
		t.Fatal("not saved")
	}
}

func TestInvalidAnimationRejected(t *testing.T) {
	e, fp, _, _, lib, _ := setup(t, "")
	fp.action = `{"speech":"","mood":"happy","animation":"","new_animation_request":{"name":"bad_move","description":"x"},"memory_ops":[],"suggestion":""}`
	fp.animSpec = `{"name":"bad_move","description":"x","tags":[],"target":"generic","duration":50,"loop":false,"tracks":[{"slot":"wing","prop":"rotation.z","times":[0,0],"values":[0,1],"interp":"smooth"}],"expressions":[],"effects":[]}`
	_, _ = e.RunOccasion(context.Background(), "random_chatter", true, "")
	if _, ok := lib.Lookup("bad_move", "blob"); ok {
		t.Fatal("invalid animation saved")
	}
}

func TestMemoryOpsAndConsolidate(t *testing.T) {
	e, fp, _, _, _, st := setup(t, "")
	fp.action = `{"speech":"oke","mood":"happy","animation":"","new_animation_request":null,"memory_ops":[{"op":"add","id":0,"kind":"preference","content":"Suka kopi susu"},{"op":"add","id":0,"kind":"fact","content":"email: a@b.co"}],"suggestion":""}`
	if _, err := e.Chat(context.Background(), "aku suka kopi susu"); err != nil {
		t.Fatal(err)
	}
	ms, _ := st.Memories()
	if len(ms) != 1 || ms[0].Content != "Suka kopi susu" {
		t.Fatalf("memories %+v", ms)
	}
	if err := e.consolidate(context.Background(), fp, config.Default()); err != nil {
		t.Fatal(err)
	}
	ms, _ = st.Memories()
	if len(ms) != 2 {
		t.Fatalf("consolidate did not add: %+v", ms)
	}
	if fp.count("memory_ops") != 1 {
		t.Fatal("memory_ops call missing")
	}
	if c, _ := st.RecentChat(5); len(c) != 2 {
		t.Fatalf("chat not stored: %+v", c)
	}
}

func TestAIDisabledBlocksEveryPath(t *testing.T) {
	e, fp, _, _, _, _ := setup(t, `{"ai":{"enabled":false}}`)
	ctx := context.Background()
	if _, err := e.RunOccasion(ctx, "greet", true, ""); err == nil {
		t.Fatal("forced trigger must respect ai.enabled=false")
	}
	if _, err := e.Chat(ctx, "halo"); err == nil {
		t.Fatal("chat must respect ai.enabled=false")
	}
	if len(fp.reqs) != 0 {
		t.Fatalf("%d AI requests while disabled", len(fp.reqs))
	}
}

func TestSanitizeActivity(t *testing.T) {
	a := PetAction{Activity: "golf"}
	a.Sanitize()
	if a.Activity != "golf" {
		t.Fatal("known activity dropped")
	}
	a = PetAction{Activity: "rm -rf"}
	a.Sanitize()
	if a.Activity != "" {
		t.Fatal("unknown activity kept")
	}
	if !strings.Contains(string(PetActionSchema), `"activity"`) {
		t.Fatal("schema missing activity")
	}
}

func TestSchemasAreValidJSON(t *testing.T) {
	for _, s := range []json.RawMessage{PetActionSchema, AnimationSpecSchema, MemoryOpsSchema} {
		var m map[string]any
		if err := json.Unmarshal(s, &m); err != nil || m["additionalProperties"] != false {
			t.Fatal("bad schema")
		}
	}
}
