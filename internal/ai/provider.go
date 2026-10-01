// Package ai wraps the BYOK providers (Anthropic, OpenAI-compatible) behind one interface
// that always returns schema-constrained JSON.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

type Request struct {
	// System holds stable prompt blocks; the last block is the cache breakpoint.
	System []string
	// User is the final user message: "[task:<name>]\n<context JSON>".
	User string
	// Image is an optional JPEG attached to the user message.
	Image      []byte
	SchemaName string
	Schema     json.RawMessage
	// Effort is "low" or "medium" (ignored by models without effort support).
	Effort    string
	MaxTokens int
}

type Response struct {
	Text         string
	Model        string
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
}

type Provider interface {
	Name() string
	Model() string
	Generate(ctx context.Context, req Request) (Response, error)
	ListModels(ctx context.Context) ([]string, error)
}

var (
	ErrNoKey     = errors.New("API key belum diatur")
	ErrAuth      = errors.New("API key ditolak (401/403)")
	ErrRateLimit = errors.New("rate limit provider (429)")
	ErrRefusal   = errors.New("model menolak permintaan")
	ErrEmpty     = errors.New("respons kosong")
)

// Config selects and configures a provider.
type Config struct {
	Provider string
	Model    string
	BaseURL  string
	APIKey   string
}

func New(c Config) (Provider, error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return nil, ErrNoKey
	}
	switch c.Provider {
	case "openai":
		return newOpenAI(c), nil
	default:
		return newAnthropic(c), nil
	}
}

var reVersionSuffix = regexp.MustCompile(`/v\d+/?$`)

// openAIBase normalizes an OpenAI-compatible base URL so it ends with /vN/.
func openAIBase(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if reVersionSuffix.MatchString(u) {
		return strings.TrimSuffix(u, "/") + "/"
	}
	return strings.TrimSuffix(u, "/") + "/v1/"
}

// anthropicBase strips a trailing /v1 (the SDK adds it).
func anthropicBase(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	u = reVersionSuffix.ReplaceAllString(u, "")
	return strings.TrimSuffix(u, "/") + "/"
}

// ExtractJSON returns the first top-level JSON object in s (tolerates code fences / prose).
func ExtractJSON(s string) string {
	s = strings.TrimSpace(s)
	if json.Valid([]byte(s)) {
		return s
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start && json.Valid([]byte(s[start:end+1])) {
		return s[start : end+1]
	}
	return s
}
