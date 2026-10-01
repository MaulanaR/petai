package anim

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

//go:embed builtin/*.json
var builtinFS embed.FS

// Library holds built-in and locally saved (AI-generated or imported) animations.
type Library struct {
	mu      sync.RWMutex
	dir     string
	builtin map[string]Spec
	user    map[string]Spec // key: target/name
}

func key(target, name string) string { return target + "/" + name }

// Open loads built-ins and every valid spec under dir/<target>/*.json.
func Open(dir string) (*Library, error) {
	l := &Library{dir: dir, builtin: map[string]Spec{}, user: map[string]Spec{}}
	ents, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return nil, err
	}
	for _, e := range ents {
		b, err := builtinFS.ReadFile("builtin/" + e.Name())
		if err != nil {
			return nil, err
		}
		var s Spec
		if err := json.Unmarshal(b, &s); err != nil {
			return nil, fmt.Errorf("builtin %s: %w", e.Name(), err)
		}
		if err := Validate(&s); err != nil {
			return nil, fmt.Errorf("builtin %s: %w", e.Name(), err)
		}
		s.Builtin = true
		l.builtin[s.Name] = s
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	for _, t := range Targets {
		files, _ := filepath.Glob(filepath.Join(dir, t, "*.json"))
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			var s Spec
			if json.Unmarshal(b, &s) != nil {
				continue
			}
			s.Target = t
			if Validate(&s) != nil {
				continue
			}
			s.Builtin = false
			l.user[key(t, s.Name)] = s
		}
	}
	return l, l.writeIndex()
}

// Lookup finds name for a character: character-specific, then generic, then built-in.
func (l *Library) Lookup(name, character string) (Spec, bool) {
	name = Slug(name)
	l.mu.RLock()
	defer l.mu.RUnlock()
	if s, ok := l.user[key(character, name)]; ok {
		return s, true
	}
	if s, ok := l.user[key("generic", name)]; ok {
		return s, true
	}
	s, ok := l.builtin[name]
	return s, ok
}

func (l *Library) IsBuiltin(name string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	_, ok := l.builtin[Slug(name)]
	return ok
}

// Save validates and persists a spec. Built-in names cannot be overwritten.
func (l *Library) Save(s Spec) (Spec, error) {
	if err := Validate(&s); err != nil {
		return s, err
	}
	if l.IsBuiltin(s.Name) {
		return s, fmt.Errorf("%q is a built-in animation", s.Name)
	}
	s.Builtin = false
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return s, err
	}
	p := filepath.Join(l.dir, s.Target, s.Name+".json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return s, err
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		return s, err
	}
	l.mu.Lock()
	l.user[key(s.Target, s.Name)] = s
	l.mu.Unlock()
	return s, l.writeIndex()
}

func (l *Library) Delete(target, name string) error {
	name = Slug(name)
	if !in(Targets, target) {
		return errors.New("unknown target")
	}
	l.mu.Lock()
	_, ok := l.user[key(target, name)]
	delete(l.user, key(target, name))
	l.mu.Unlock()
	if !ok {
		return errors.New("animation not found (built-ins cannot be deleted)")
	}
	_ = os.Remove(filepath.Join(l.dir, target, name+".json"))
	return l.writeIndex()
}

// Specs returns every animation playable by character (user overrides shadow generic).
func (l *Library) Specs(character string) []Spec {
	l.mu.RLock()
	defer l.mu.RUnlock()
	byName := map[string]Spec{}
	for n, s := range l.builtin {
		byName[n] = s
	}
	for _, s := range l.user {
		if s.Target == "generic" {
			byName[s.Name] = s
		}
	}
	for _, s := range l.user {
		if s.Target == character {
			byName[s.Name] = s
		}
	}
	out := make([]Spec, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Catalog is the compact list sent to the AI.
func (l *Library) Catalog(character string) []Meta {
	specs := l.Specs(character)
	out := make([]Meta, len(specs))
	for i, s := range specs {
		out[i] = s.Meta()
	}
	return out
}

// All lists every animation (all targets) for the settings UI and debug API.
func (l *Library) All() []Meta {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Meta, 0, len(l.builtin)+len(l.user))
	for _, s := range l.builtin {
		out = append(out, s.Meta())
	}
	for _, s := range l.user {
		out = append(out, s.Meta())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Builtin != out[j].Builtin {
			return out[i].Builtin
		}
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// CatalogText renders the catalog compactly for prompts (stable order).
func CatalogText(metas []Meta) string {
	var b strings.Builder
	for _, m := range metas {
		fmt.Fprintf(&b, "- %s: %s", m.Name, m.Description)
		if len(m.Tags) > 0 {
			fmt.Fprintf(&b, " [%s]", strings.Join(m.Tags, ", "))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func (l *Library) writeIndex() error {
	b, err := json.MarshalIndent(l.All(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(l.dir, "index.json"), b, 0o600)
}
