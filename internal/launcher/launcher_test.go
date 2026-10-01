package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"petai/internal/config"
)

type fake struct {
	target, params, clip string
}

func newFake(dir string) (*Launcher, *fake) {
	f := &fake{}
	return &Launcher{
		Open:         func(t, p string) error { f.target, f.params = t, p; return nil },
		SetClipboard: func(s string) error { f.clip = s; return nil },
		DocsFolder:   func() string { return dir },
		Now:          func() time.Time { return time.Date(2026, 10, 1, 9, 30, 0, 0, time.Local) },
	}, f
}

var apps = []config.App{
	{ID: "word", Name: "Microsoft Word", Kind: "program", Target: `C:\Office\WINWORD.EXE`, Accepts: "docx"},
	{ID: "notepad", Name: "Notepad", Kind: "program", Target: "notepad.exe", Accepts: "txt"},
	{ID: "google_docs", Name: "Google Docs", Kind: "website", Target: "https://docs.new", Clipboard: true},
	{ID: "google_search", Name: "Google Search", Kind: "website", Target: "https://www.google.com/search?q={query}"},
	{ID: "evil", Name: "Evil", Kind: "website", Target: "file:///C:/Windows/System32/cmd.exe"},
}

func TestUnknownAppRejected(t *testing.T) {
	l, f := newFake(t.TempDir())
	if _, err := l.Launch(apps, "", Request{AppID: "cmd"}); !errors.Is(err, ErrUnknownApp) {
		t.Fatalf("want ErrUnknownApp, got %v", err)
	}
	if f.target != "" {
		t.Fatal("must not open anything")
	}
}

func TestWordWithNotulensiTemplate(t *testing.T) {
	dir := filepath.Clean(t.TempDir())
	l, f := newFake(dir)
	res, err := l.Launch(apps, "", Request{AppID: "WORD", Doc: &Doc{Title: `Notulensi Meeting — Rabu, 1/10: "tim"`, Content: "## Peserta\n- ..."}})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(res.File) != dir || !strings.HasSuffix(res.File, ".docx") {
		t.Fatalf("file %q not in %q", res.File, dir)
	}
	if strings.ContainsAny(filepath.Base(res.File), `<>:"/\|?*`) {
		t.Fatalf("unsafe name %q", res.File)
	}
	if f.target != `C:\Office\WINWORD.EXE` || !strings.Contains(f.params, res.File) {
		t.Fatalf("opened %q %q", f.target, f.params)
	}
	if _, err := os.Stat(res.File); err != nil {
		t.Fatal(err)
	}
	// second document with the same title doesn't overwrite the first
	res2, _ := l.Launch(apps, "", Request{AppID: "word", Doc: &Doc{Title: `Notulensi Meeting — Rabu, 1/10: "tim"`}})
	if res2.File == res.File {
		t.Fatal("overwrote existing document")
	}
}

func TestNotepadTxtAndPathTraversalTitle(t *testing.T) {
	dir := filepath.Clean(t.TempDir())
	l, _ := newFake(dir)
	res, err := l.Launch(apps, "", Request{AppID: "notepad", Doc: &Doc{Title: `..\..\Windows\evil`, Content: "- a"}})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(res.File) != dir || !strings.HasSuffix(res.File, ".txt") {
		t.Fatalf("escaped docs folder: %q", res.File)
	}
}

func TestWebsiteQueryAndClipboard(t *testing.T) {
	l, f := newFake(t.TempDir())
	res, err := l.Launch(apps, "", Request{AppID: "google_search", Query: "resep rendang & sambal"})
	if err != nil || res.URL != "https://www.google.com/search?q=resep+rendang+%26+sambal" || f.target != res.URL {
		t.Fatalf("url %q err %v", res.URL, err)
	}
	res, err = l.Launch(apps, "", Request{AppID: "google_docs", Doc: &Doc{Title: "Notulensi", Content: "# Notulensi\n## Agenda\n- satu"}})
	if err != nil || !res.Clipboard || !strings.Contains(f.clip, "• satu") || strings.Contains(f.clip, "#") {
		t.Fatalf("clipboard %q res %+v err %v", f.clip, res, err)
	}
	if _, err := l.Launch(apps, "", Request{AppID: "evil"}); err == nil {
		t.Fatal("non-http website must be rejected")
	}
}

func TestSafeName(t *testing.T) {
	if got := SafeName(`  a/b\c:d*e?  `); got != "a b c d e" {
		t.Fatalf("%q", got)
	}
	if SafeName("...") != "Dokumen PetAI" {
		t.Fatal("empty fallback")
	}
}
