package ai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

type openAIProvider struct {
	client openai.Client
	model  string
}

func newOpenAI(c Config) *openAIProvider {
	opts := []option.RequestOption{
		option.WithAPIKey(c.APIKey),
		option.WithRequestTimeout(90 * time.Second),
		option.WithMaxRetries(2),
	}
	if base := openAIBase(c.BaseURL); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	return &openAIProvider{client: openai.NewClient(opts...), model: strings.TrimSpace(c.Model)}
}

func (p *openAIProvider) Name() string  { return "openai" }
func (p *openAIProvider) Model() string { return p.model }

func (p *openAIProvider) Generate(ctx context.Context, req Request) (Response, error) {
	if p.model == "" {
		return Response{}, errors.New("model OpenAI belum dipilih")
	}
	msgs := []openai.ChatCompletionMessageParamUnion{}
	if len(req.System) > 0 {
		msgs = append(msgs, openai.SystemMessage(strings.Join(req.System, "\n\n")))
	}
	if len(req.Image) > 0 || len(req.Audio) > 0 {
		parts := []openai.ChatCompletionContentPartUnionParam{}
		if len(req.Image) > 0 {
			parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
				URL:    "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(req.Image),
				Detail: "low",
			}))
		}
		if len(req.Audio) > 0 {
			parts = append(parts, openai.InputAudioContentPart(openai.ChatCompletionContentPartInputAudioInputAudioParam{
				Data:   base64.StdEncoding.EncodeToString(req.Audio),
				Format: "wav",
			}))
		}
		parts = append(parts, openai.TextContentPart(req.User))
		msgs = append(msgs, openai.UserMessage(parts))
	} else {
		msgs = append(msgs, openai.UserMessage(req.User))
	}
	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = 4096
	}
	params := openai.ChatCompletionNewParams{
		Model:               shared.ChatModel(p.model),
		Messages:            msgs,
		MaxCompletionTokens: openai.Int(int64(maxTok)),
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   req.SchemaName,
					Strict: openai.Bool(true),
					Schema: req.Schema,
				},
			},
		},
	}
	res, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return Response{}, classifyOpenAI(err)
	}
	if len(res.Choices) == 0 {
		return Response{}, ErrEmpty
	}
	ch := res.Choices[0]
	if ch.Message.Refusal != "" {
		return Response{}, ErrRefusal
	}
	text := ExtractJSON(ch.Message.Content)
	if text == "" {
		return Response{}, ErrEmpty
	}
	return Response{
		Text:         text,
		Model:        res.Model,
		InputTokens:  res.Usage.PromptTokens,
		OutputTokens: res.Usage.CompletionTokens,
		CacheRead:    res.Usage.PromptTokensDetails.CachedTokens,
	}, nil
}

func classifyOpenAI(err error) error {
	var apierr *openai.Error
	if errors.As(err, &apierr) {
		switch apierr.StatusCode {
		case 401, 403:
			return ErrAuth
		case 429:
			return ErrRateLimit
		}
		if apierr.StatusCode == 400 && strings.Contains(strings.ToLower(apierr.Error()), "audio") {
			return fmt.Errorf("%w: %s", ErrAudioUnsupported, apierr.Error())
		}
		return fmt.Errorf("openai %d: %s", apierr.StatusCode, shortErr(apierr.Error()))
	}
	return err
}

func (p *openAIProvider) ListModels(ctx context.Context) ([]string, error) {
	it := p.client.Models.ListAutoPaging(ctx)
	var out []string
	for it.Next() {
		out = append(out, it.Current().ID)
	}
	if err := it.Err(); err != nil {
		return nil, classifyOpenAI(err)
	}
	sort.Strings(out)
	return out, nil
}
