package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "petai.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 10, 1, 15, 0, 0, 0, time.Local)

	id, _ := s.StartSession("code.exe", "main.go", now.Add(-2*time.Hour))
	_ = s.TouchSession(id, now.Add(-time.Hour))
	old, _ := s.StartSession("old.exe", "x", now.AddDate(0, 0, -30))
	_ = s.TouchSession(old, now.AddDate(0, 0, -30).Add(time.Minute))
	n, err := s.PurgeActivity(now.AddDate(0, 0, -14))
	if err != nil || n != 1 {
		t.Fatalf("purge %d %v", n, err)
	}
	st, err := s.ActivityStats(now, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.TopApps) != 1 || st.TopApps[0].App != "code.exe" || st.TopApps[0].Minutes != 60 {
		t.Fatalf("stats %+v", st)
	}
	if st.TypicalStart != "13:00" {
		t.Fatalf("typical start %q", st.TypicalStart)
	}

	mid, err := s.AddMemory("habit", "Suka ngoding sore hari", "ai", 0.6, now)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := s.AddMemory("habit", "suka ngoding sore hari", "ai", 0.6, now)
	if again != mid {
		t.Fatal("duplicate memory not merged")
	}
	if err := s.UpdateMemory(mid, "preference", "Suka kopi", now); err != nil {
		t.Fatal(err)
	}
	ms, _ := s.Memories()
	if len(ms) != 1 || ms[0].Kind != "preference" {
		t.Fatalf("memories %+v", ms)
	}
	_ = s.DeleteMemory(mid)
	if ms, _ = s.Memories(); len(ms) != 0 {
		t.Fatal("delete failed")
	}

	_ = s.AppendChat("user", "halo", now)
	_ = s.AppendChat("pet", "hai!", now)
	c, _ := s.RecentChat(10)
	if len(c) != 2 || c[0].Role != "user" {
		t.Fatalf("chat %+v", c)
	}
	if err := s.WipeAll(); err != nil {
		t.Fatal(err)
	}
	if c, _ = s.RecentChat(10); len(c) != 0 {
		t.Fatal("wipe failed")
	}
}
