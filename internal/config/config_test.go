package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsArePrivacySafe(t *testing.T) {
	c := Default()
	if c.Privacy.WatchActivity || c.Privacy.Screenshots {
		t.Fatal("watching must be opt-in")
	}
	if c.ScreenshotsEffective() {
		t.Fatal("screenshots must be off by default")
	}
}

func TestScreenshotsNeedMasterToggle(t *testing.T) {
	c := Default()
	c.Privacy.Screenshots = true
	if c.ScreenshotsEffective() {
		t.Fatal("screenshots without watchActivity must stay off")
	}
	c.Privacy.WatchActivity = true
	if !c.ScreenshotsEffective() {
		t.Fatal("expected screenshots on")
	}
}

func TestLoadPartialAndMerge(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(`{"pet":{"character":"cat"},"movement":{"mode":"bogus"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	c := m.Get()
	if c.Pet.Character != "cat" || c.Movement.Mode != "ground" || c.AI.Anthropic.Model != DefaultAnthropicModel {
		t.Fatalf("unexpected %+v", c)
	}
	c2, err := m.Merge([]byte(`{"movement":{"mode":"free"},"privacy":{"watchActivity":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c2.Movement.Mode != "free" || !c2.Privacy.WatchActivity || c2.Pet.Character != "cat" {
		t.Fatalf("merge failed %+v", c2)
	}
	m2, _ := Load(p)
	if m2.Get().Movement.Mode != "free" {
		t.Fatal("not persisted")
	}
}
