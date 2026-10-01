package anim

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func spec(t *testing.T, js string) Spec {
	t.Helper()
	var s Spec
	if err := json.Unmarshal([]byte(js), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

const good = `{"name":"Happy Spin","description":"spin","tags":["happy"],"target":"generic","duration":1.2,"loop":false,
 "tracks":[{"slot":"root","prop":"rotation.y","times":[0,0.6,1.2],"values":[0,3.14,99],"interp":"smooth"},
           {"slot":"body","prop":"scale","times":[0,0.6],"values":[[1,1,1],[5,0.1,1]]}],
 "expressions":[{"t":0,"eyes":"happy","mouth":"open"}],"effects":[{"t":0.5,"type":"hearts"}]}`

func TestValidateClampsAndSlugs(t *testing.T) {
	s := spec(t, good)
	if err := Validate(&s); err != nil {
		t.Fatal(err)
	}
	if s.Name != "happy_spin" {
		t.Fatalf("slug %q", s.Name)
	}
	if v := s.Tracks[0].Values[2].N; v != maxRot {
		t.Fatalf("rotation not clamped: %v", v)
	}
	if v := s.Tracks[1].Values[1].Vec; v[0] != maxScale || v[1] != minScale {
		t.Fatalf("scale not clamped: %v", v)
	}
	if s.Tracks[1].Interp != "smooth" {
		t.Fatal("default interp")
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]string{
		"unknown slot":    `{"name":"x","duration":1,"tracks":[{"slot":"wing","prop":"rotation.z","times":[0],"values":[0]}]}`,
		"unknown prop":    `{"name":"x","duration":1,"tracks":[{"slot":"body","prop":"color","times":[0],"values":[0]}]}`,
		"non increasing":  `{"name":"x","duration":1,"tracks":[{"slot":"body","prop":"rotation.z","times":[0,0.5,0.5],"values":[0,1,2]}]}`,
		"too long":        `{"name":"x","duration":50,"tracks":[{"slot":"body","prop":"rotation.z","times":[0],"values":[0]}]}`,
		"len mismatch":    `{"name":"x","duration":1,"tracks":[{"slot":"body","prop":"rotation.z","times":[0,1],"values":[0]}]}`,
		"scale scalar":    `{"name":"x","duration":1,"tracks":[{"slot":"body","prop":"scale","times":[0],"values":[1]}]}`,
		"no tracks":       `{"name":"x","duration":1,"tracks":[]}`,
		"bad effect":      `{"name":"x","duration":1,"tracks":[{"slot":"body","prop":"rotation.z","times":[0],"values":[0]}],"effects":[{"t":0,"type":"explode"}]}`,
		"time > duration": `{"name":"x","duration":1,"tracks":[{"slot":"body","prop":"rotation.z","times":[0,2],"values":[0,1]}]}`,
		"empty name":      `{"name":"!!","duration":1,"tracks":[{"slot":"body","prop":"rotation.z","times":[0],"values":[0]}]}`,
		"tail on blob":    `{"name":"x","target":"blob","duration":1,"tracks":[{"slot":"tail","prop":"rotation.z","times":[0],"values":[0]}]}`,
	}
	for name, js := range cases {
		s := spec(t, js)
		if err := Validate(&s); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestLibraryBuiltinsSaveLookupReuse(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"idle", "walk", "float", "sleep", "jump", "wave", "happy_bounce", "surprised", "dangle", "fall", "land", "sit", "look_around",
		"kick", "dribble", "throw", "golf_swing", "mulas", "relieved"} {
		if s, ok := l.Lookup(n, "cat"); !ok || !s.Builtin {
			t.Errorf("missing builtin %s", n)
		}
	}
	s := spec(t, good)
	if _, err := l.Save(s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "generic", "happy_spin.json")); err != nil {
		t.Fatal("not written to disk")
	}
	bad := spec(t, `{"name":"wave","duration":1,"tracks":[{"slot":"body","prop":"rotation.z","times":[0],"values":[0]}]}`)
	if _, err := l.Save(bad); err == nil {
		t.Fatal("builtin overwrite must fail")
	}
	// Reopen: persisted spec is found again without regeneration.
	l2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := l2.Lookup("happy spin", "chick"); !ok || got.Builtin {
		t.Fatal("saved animation not reusable after restart")
	}
	if _, err := os.Stat(filepath.Join(dir, "index.json")); err != nil {
		t.Fatal("index missing")
	}
	if err := l2.Delete("generic", "happy_spin"); err != nil {
		t.Fatal(err)
	}
	if _, ok := l2.Lookup("happy_spin", "chick"); ok {
		t.Fatal("delete failed")
	}
}
