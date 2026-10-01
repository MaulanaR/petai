package ai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

type anthropicProvider struct {
	client   anthropic.Client
	model    string
	official bool
}

func newAnthropic(c Config) *anthropicProvider {
	opts := []option.RequestOption{
		option.WithAPIKey(c.APIKey),
		option.WithRequestTimeout(90 * time.Second),
		option.WithMaxRetries(2),
	}
	base := anthropicBase(c.BaseURL)
	if base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	model := strings.TrimSpace(c.Model)
	if model == "" {
		model = "claude-opus-5-5"
	}
	return &anthropicProvider{
		client:   anthropic.NewClient(opts...),
		model:    model,
		official: base == "" || strings.Contains(base, "api.anthropic.com"),
	}
}

func (p *anthropicProvider) Name() string  { return "anthropic" }
func (p *anthropicProvider) Model() string { return p.model }

// supportsEffort: the effort parameter exists on Opus 4.5+, Sonnet 4.6+, Fable/Mythos; not on Haiku.
func supportsEffort(model string) bool {
	m := strings.ToLower(model)
	if strings.Contains(m, "haiku") || strings.Contains(m, "claude-3") || strings.Contains(m, "sonnet-4-5") ||
		strings.Contains(m, "sonnet-4-2") || strings.Contains(m, "opus-4-1") || strings.Contains(m, "opus-4-2") {
		return false
	}
	return true
}

// supportsDefaultFallback: server-side "default" refusal fallbacks (Claude API only).
func supportsDefaultFallback(model string) bool {
	switch strings.ToLower(model) {
	case "claude-opus-5-5", "claude-opus-5", "claude-fable-5-1", "claude-sonnet-5-5":
		return true
	}
	return false
}

func (p *anthropicProvider) Generate(ctx context.Context, req Request) (Response, error) {
	if len(req.Audio) > 0 {
		return Response{}, ErrAudioUnsupported // Claude does not accept audio input
	}
	system := make([]anthropic.BetaTextBlockParam, 0, len(req.System))
	for i, s := range req.System {
		b := anthropic.BetaTextBlockParam{Text: s}
		if i == len(req.System)-1 {
			b.CacheControl = anthropic.NewBetaCacheControlEphemeralParam()
		}
		system = append(system, b)
	}
	blocks := []anthropic.BetaContentBlockParamUnion{}
	if len(req.Image) > 0 {
		blocks = append(blocks, anthropic.NewBetaImageBlock(anthropic.BetaBase64ImageSourceParam{
			Data:      base64.StdEncoding.EncodeToString(req.Image),
			MediaType: anthropic.BetaBase64ImageSourceMediaTypeImageJPEG,
		}))
	}
	blocks = append(blocks, anthropic.NewBetaTextBlock(req.User))

	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = 4096
	}
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: int64(maxTok),
		System:    system,
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(blocks...)},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Format: anthropic.BetaJSONOutputFormatParam{Schema: req.Schema},
		},
	}
	if req.Effort != "" && supportsEffort(p.model) {
		params.OutputConfig.Effort = anthropic.BetaOutputConfigEffort(req.Effort)
	}
	if p.official && supportsDefaultFallback(p.model) {
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		params.Fallbacks = anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}
	}

	msg, err := p.client.Beta.Messages.New(ctx, params)
	if err != nil {
		return Response{}, classifyAnthropic(err)
	}
	if msg.StopReason == anthropic.BetaStopReasonRefusal {
		return Response{}, ErrRefusal
	}
	var sb strings.Builder
	for _, b := range msg.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	text := ExtractJSON(sb.String())
	if text == "" {
		if msg.StopReason == anthropic.BetaStopReasonMaxTokens {
			return Response{}, fmt.Errorf("%w (max_tokens)", ErrEmpty)
		}
		return Response{}, ErrEmpty
	}
	return Response{
		Text:         text,
		Model:        string(msg.Model),
		InputTokens:  msg.Usage.InputTokens,
		OutputTokens: msg.Usage.OutputTokens,
		CacheRead:    msg.Usage.CacheReadInputTokens,
	}, nil
}

func classifyAnthropic(err error) error {
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		switch apierr.StatusCode {
		case 401, 403:
			return ErrAuth
		case 429:
			return ErrRateLimit
		}
		return fmt.Errorf("anthropic %d: %s", apierr.StatusCode, shortErr(apierr.Error()))
	}
	return err
}

func (p *anthropicProvider) ListModels(ctx context.Context) ([]string, error) {
	it := p.client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	var out []string
	for it.Next() {
		out = append(out, it.Current().ID)
	}
	if err := it.Err(); err != nil {
		return nil, classifyAnthropic(err)
	}
	sort.Strings(out)
	return out, nil
}

func shortErr(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
