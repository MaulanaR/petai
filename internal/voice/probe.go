// Package voice decides whether the selected AI model can hear (audio input) and drives voice turns.
package voice

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"petai/internal/ai"
)

// probeWAV is a short Indonesian recording ("satu, dua, tiga", Microsoft Andika voice, 16 kHz mono).
// Only this sample is ever sent for capability checks — never the user's voice.
//
//go:embed probe.wav
var probeWAV []byte

// Result of a capability check.
type Result struct {
	Supported   bool      `json:"supported"`
	Heard       string    `json:"heard"`
	Error       string    `json:"error"`
	Suggestions []string  `json:"suggestions"`
	Model       string    `json:"model"`
	CheckedAt   time.Time `json:"checkedAt"`
}

var probeSchema = json.RawMessage(`{"type":"object","properties":{"heard":{"type":"string","description":"exact words spoken in the audio, or '' if there is no audio"}},"required":["heard"],"additionalProperties":false}`)

// Probe sends the embedded sample to the model and checks that it actually transcribes it.
// A request that succeeds but ignores the audio (some gateways silently drop it) counts as unsupported.
func Probe(ctx context.Context, p ai.Provider) Result {
	r := Result{Model: p.Model(), CheckedAt: time.Now()}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	res, err := p.Generate(ctx, ai.Request{
		System:     []string{"You transcribe short audio clips. Reply only with JSON matching the schema."},
		User:       "[task:transcribe]\nWrite exactly the words spoken in the attached audio. If there is no audio, use an empty string.",
		Audio:      probeWAV,
		SchemaName: "Transcript",
		Schema:     probeSchema,
		Effort:     "low",
		MaxTokens:  1024,
	})
	if err != nil {
		r.Error = err.Error()
		r.Suggestions = SuggestedModels(err.Error())
		if errors.Is(err, ai.ErrAudioUnsupported) {
			r.Error = ai.ErrAudioUnsupported.Error() // the long gateway message only feeds Suggestions
		}
		return r
	}
	var out struct {
		Heard string `json:"heard"`
	}
	if json.Unmarshal([]byte(res.Text), &out) != nil {
		out.Heard = res.Text
	}
	r.Heard = strings.TrimSpace(out.Heard)
	if heardProbe(r.Heard) {
		r.Supported = true
		return r
	}
	r.Error = "model menjawab tapi tidak mendengar audionya"
	return r
}

var wordRe = regexp.MustCompile(`[a-z0-9]+`)

// heardProbe accepts a transcript containing at least two of "satu dua tiga" (or 1 2 3 / one two three).
func heardProbe(s string) bool {
	seen := map[string]bool{}
	for _, w := range wordRe.FindAllString(strings.ToLower(s), -1) {
		switch w {
		case "satu", "1", "one":
			seen["1"] = true
		case "dua", "2", "two":
			seen["2"] = true
		case "tiga", "3", "three":
			seen["3"] = true
		}
	}
	return len(seen) >= 2
}

var reModelsThatDo = regexp.MustCompile(`(?i)models that do:\s*([^"}\]]+)`)

// SuggestedModels extracts audio-capable model names from a gateway error such as
// "model 'x' does not accept audio input; models that do: cp/a/mimo-v2.5, ocg/mimo-v2.6-flash".
func SuggestedModels(errText string) []string {
	m := reModelsThatDo.FindStringSubmatch(errText)
	if m == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(m[1], ",") {
		name := strings.TrimSpace(part)
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}
