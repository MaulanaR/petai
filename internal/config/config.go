// Package config loads, validates and persists PetAI settings (config.json).
package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Pet struct {
	Character   string  `json:"character"`
	Name        string  `json:"name"`
	Color       string  `json:"color"`
	Personality string  `json:"personality"`
	Language    string  `json:"language"`
	Scale       float64 `json:"scale"`
}

type Movement struct {
	Mode     string  `json:"mode"`
	Speed    float64 `json:"speed"`
	Activity float64 `json:"activity"`
	AnchorX  float64 `json:"anchorX"`
	AnchorY  float64 `json:"anchorY"`
}

type ProviderCfg struct {
	Model   string `json:"model"`
	BaseURL string `json:"baseURL"`
}

type AI struct {
	Enabled         bool        `json:"enabled"`
	Provider        string      `json:"provider"`
	Anthropic       ProviderCfg `json:"anthropic"`
	OpenAI          ProviderCfg `json:"openai"`
	MaxCallsPerHour int         `json:"maxCallsPerHour"`
}

type Privacy struct {
	WatchActivity         bool     `json:"watchActivity"`
	Screenshots           bool     `json:"screenshots"`
	ScreenshotIntervalMin int      `json:"screenshotIntervalMin"`
	Blocklist             []string `json:"blocklist"`
	RetentionDays         int      `json:"retentionDays"`
	ExcludeFromCapture    bool     `json:"excludeFromCapture"`
}

type General struct {
	Autostart         bool `json:"autostart"`
	FPS               int  `json:"fps"`
	Monitor           int  `json:"monitor"`
	RespectFullscreen bool `json:"respectFullscreen"`
	Debug             bool `json:"debug"`
}

type Config struct {
	Version  int      `json:"version"`
	Pet      Pet      `json:"pet"`
	Movement Movement `json:"movement"`
	AI       AI       `json:"ai"`
	Privacy  Privacy  `json:"privacy"`
	General  General  `json:"general"`
}

const DefaultAnthropicModel = "claude-opus-5-5"

var DefaultBlocklist = []string{
	"1password", "bitwarden", "keepass", "lastpass", "dashlane", "inprivate", "incognito",
	"bank", "bca", "mandiri", "bri", "bni", "klikbca", "mybca", "livin", "brimo", "ocbc", "cimb",
	"danamon", "permata", "jenius", "seabank", "paypal", "wise.com", "gopay", "ovo", "dana",
	"windows security", "credential", "password", "consent.exe", "credentialuibroker",
	"proton pass", "nordpass", "enpass", "roboform", "keeper", "authy", "authenticator",
	"whatsapp", "telegram", "signal", "line.exe", "messenger",
}

var Characters = []string{"blob", "cat", "chick"}

func Default() Config {
	return Config{
		Version: 1,
		Pet: Pet{
			Character:   "blob",
			Name:        "Mochi",
			Color:       "", // "" = the character's own default colour
			Personality: "ceria, sedikit jahil, perhatian, suka memberi semangat",
			Language:    "id",
			Scale:       1.0,
		},
		Movement: Movement{Mode: "ground", Speed: 1.0, Activity: 0.5, AnchorX: -1, AnchorY: -1},
		AI: AI{
			Enabled:         true,
			Provider:        "anthropic",
			Anthropic:       ProviderCfg{Model: DefaultAnthropicModel},
			OpenAI:          ProviderCfg{},
			MaxCallsPerHour: 12,
		},
		Privacy: Privacy{
			WatchActivity:         false,
			Screenshots:           false,
			ScreenshotIntervalMin: 10,
			Blocklist:             append([]string(nil), DefaultBlocklist...),
			RetentionDays:         14,
			ExcludeFromCapture:    true,
		},
		General: General{Autostart: false, FPS: 30, Monitor: 0, RespectFullscreen: true, Debug: false},
	}
}

// Normalize clamps values into their valid ranges and fills blanks with defaults.
func (c *Config) Normalize() {
	d := Default()
	c.Version = 1
	if !contains(Characters, c.Pet.Character) {
		c.Pet.Character = d.Pet.Character
	}
	if strings.TrimSpace(c.Pet.Name) == "" {
		c.Pet.Name = d.Pet.Name
	}
	if len(c.Pet.Name) > 32 {
		c.Pet.Name = c.Pet.Name[:32]
	}
	if c.Pet.Color != "" && (!strings.HasPrefix(c.Pet.Color, "#") || (len(c.Pet.Color) != 7 && len(c.Pet.Color) != 4)) {
		c.Pet.Color = d.Pet.Color
	}
	if len(c.Pet.Personality) > 400 {
		c.Pet.Personality = c.Pet.Personality[:400]
	}
	if c.Pet.Language != "id" && c.Pet.Language != "en" {
		c.Pet.Language = "id"
	}
	c.Pet.Scale = clamp(c.Pet.Scale, 0.5, 2, 1)
	if c.Movement.Mode != "stay" && c.Movement.Mode != "ground" && c.Movement.Mode != "free" {
		c.Movement.Mode = d.Movement.Mode
	}
	c.Movement.Speed = clamp(c.Movement.Speed, 0.25, 3, 1)
	if c.Movement.Activity < 0 || c.Movement.Activity > 1 {
		c.Movement.Activity = 0.5
	}
	if c.AI.Provider != "anthropic" && c.AI.Provider != "openai" {
		c.AI.Provider = "anthropic"
	}
	if strings.TrimSpace(c.AI.Anthropic.Model) == "" {
		c.AI.Anthropic.Model = DefaultAnthropicModel
	}
	// 0 = no automatic comments (chat still works).
	if c.AI.MaxCallsPerHour < 0 || c.AI.MaxCallsPerHour > 120 {
		c.AI.MaxCallsPerHour = d.AI.MaxCallsPerHour
	}
	if c.Privacy.ScreenshotIntervalMin < 1 {
		c.Privacy.ScreenshotIntervalMin = d.Privacy.ScreenshotIntervalMin
	}
	if c.Privacy.Blocklist == nil {
		c.Privacy.Blocklist = append([]string(nil), DefaultBlocklist...)
	}
	clean := c.Privacy.Blocklist[:0]
	for _, b := range c.Privacy.Blocklist {
		if b = strings.ToLower(strings.TrimSpace(b)); b != "" {
			clean = append(clean, b)
		}
	}
	c.Privacy.Blocklist = clean
	if c.Privacy.RetentionDays < 1 || c.Privacy.RetentionDays > 365 {
		c.Privacy.RetentionDays = d.Privacy.RetentionDays
	}
	if c.General.FPS != 15 && c.General.FPS != 30 && c.General.FPS != 60 {
		c.General.FPS = 30
	}
	if c.General.Monitor < 0 {
		c.General.Monitor = 0
	}
}

// ScreenshotsEffective reports whether screenshots may be captured (requires the master watch toggle).
func (c Config) ScreenshotsEffective() bool {
	return c.Privacy.WatchActivity && c.Privacy.Screenshots
}

func clamp(v, lo, hi, def float64) float64 {
	if v == 0 {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Manager guards the live config and persists it.
type Manager struct {
	mu   sync.RWMutex
	path string
	cfg  Config
	subs []func(Config)
}

// Load reads path (missing or partial files fall back to defaults) and normalizes the result.
func Load(path string) (*Manager, error) {
	m := &Manager{path: path, cfg: Default()}
	b, err := os.ReadFile(path)
	if err == nil {
		b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")) // Notepad/PowerShell may add a UTF-8 BOM
		cfg := Default()
		if jerr := json.Unmarshal(b, &cfg); jerr == nil {
			m.cfg = cfg
		} else {
			// Keep the unreadable file for the user instead of silently losing it.
			_ = os.WriteFile(path+".bak", b, 0o600)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	m.cfg.Normalize()
	return m, m.save()
}

func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c := m.cfg
	c.Privacy.Blocklist = append([]string(nil), m.cfg.Privacy.Blocklist...)
	return c
}

// Set replaces the config, saves it and notifies subscribers.
func (m *Manager) Set(c Config) (Config, error) {
	c.Normalize()
	m.mu.Lock()
	m.cfg = c
	err := m.save()
	subs := append([]func(Config){}, m.subs...)
	m.mu.Unlock()
	for _, f := range subs {
		f(c)
	}
	return c, err
}

// Merge deep-merges a partial JSON document into the current config.
func (m *Manager) Merge(patch []byte) (Config, error) {
	cur := m.Get()
	base, _ := json.Marshal(cur)
	var a, b map[string]any
	_ = json.Unmarshal(base, &a)
	if err := json.Unmarshal(patch, &b); err != nil {
		return cur, err
	}
	deepMerge(a, b)
	merged, _ := json.Marshal(a)
	next := Default()
	if err := json.Unmarshal(merged, &next); err != nil {
		return cur, err
	}
	return m.Set(next)
}

func (m *Manager) Subscribe(f func(Config)) {
	m.mu.Lock()
	m.subs = append(m.subs, f)
	m.mu.Unlock()
}

func (m *Manager) save() error {
	b, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}

func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				deepMerge(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}
