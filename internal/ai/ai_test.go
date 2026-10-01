package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var schema = json.RawMessage(`{"type":"object","properties":{"speech":{"type":"string"}},"required":["speech"],"additionalProperties":false}`)

func capture(t *testing.T, status int, reply string) (*httptest.Server, *map[string]any, *http.Header, *string) {
	t.Helper()
	body := map[string]any{}
	hdr := http.Header{}
	path := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		hdr = r.Header.Clone()
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, &body, &hdr, &path
}

func TestAnthropicWire(t *testing.T) {
	srv, body, hdr, path := capture(t, 200, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5",
		"content":[{"type":"text","text":"{\"speech\":\"halo\"}"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`)
	p, err := New(Config{Provider: "anthropic", BaseURL: srv.URL + "/v1", APIKey: "sk-test", Model: "claude-opus-5-5"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Generate(context.Background(), Request{
		System: []string{"persona", "catalog"}, User: "[task:pet_action]\n{}", Image: []byte{0xff, 0xd8},
		SchemaName: "PetAction", Schema: schema, Effort: "low", MaxTokens: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != `{"speech":"halo"}` {
		t.Fatalf("text %q", res.Text)
	}
	if *path != "/v1/messages" || hdr.Get("X-Api-Key") != "sk-test" {
		t.Fatalf("path %s key %q", *path, hdr.Get("X-Api-Key"))
	}
	b := *body
	oc := b["output_config"].(map[string]any)
	if oc["effort"] != "low" || oc["format"].(map[string]any)["type"] != "json_schema" {
		t.Fatalf("output_config %v", oc)
	}
	sys := b["system"].([]any)
	if _, ok := sys[1].(map[string]any)["cache_control"]; !ok || len(sys) != 2 {
		t.Fatalf("cache_control missing on last system block: %v", sys)
	}
	content := b["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["type"] != "image" || !strings.HasPrefix(content[1].(map[string]any)["text"].(string), "[task:pet_action]") {
		t.Fatalf("content %v", content)
	}
	if _, ok := b["fallbacks"]; ok {
		t.Fatal("fallbacks must not be sent to a non-official base URL")
	}
	if _, ok := b["thinking"]; ok {
		t.Fatal("thinking must not be sent")
	}
}

func TestAnthropicHaikuNoEffortAndErrors(t *testing.T) {
	srv, body, _, _ := capture(t, 200, `{"id":"m","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[{"type":"text","text":"{\"speech\":\"\"}"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	p, _ := New(Config{Provider: "anthropic", BaseURL: srv.URL, APIKey: "k", Model: "claude-haiku-4-5"})
	if _, err := p.Generate(context.Background(), Request{User: "[task:pet_action]", Schema: schema, Effort: "low"}); err != nil {
		t.Fatal(err)
	}
	if oc := (*body)["output_config"].(map[string]any); oc["effort"] != nil {
		t.Fatal("haiku must not receive effort")
	}

	srvR, _, _, _ := capture(t, 200, `{"id":"m","type":"message","role":"assistant","model":"x","content":[],"stop_reason":"refusal","usage":{"input_tokens":1,"output_tokens":0}}`)
	p, _ = New(Config{Provider: "anthropic", BaseURL: srvR.URL, APIKey: "k"})
	if _, err := p.Generate(context.Background(), Request{User: "x", Schema: schema}); !errors.Is(err, ErrRefusal) {
		t.Fatalf("want refusal, got %v", err)
	}
	srv401, _, _, _ := capture(t, 401, `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`)
	p, _ = New(Config{Provider: "anthropic", BaseURL: srv401.URL, APIKey: "k"})
	if _, err := p.Generate(context.Background(), Request{User: "x", Schema: schema}); !errors.Is(err, ErrAuth) {
		t.Fatalf("want auth error, got %v", err)
	}
}

func TestOpenAIWire(t *testing.T) {
	srv, body, hdr, path := capture(t, 200, `{"id":"c1","object":"chat.completion","created":1,"model":"gpt-x",
		"choices":[{"index":0,"message":{"role":"assistant","content":"{\"speech\":\"hai\"}"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
	p, err := New(Config{Provider: "openai", BaseURL: srv.URL, APIKey: "sk-oa", Model: "gpt-x"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Generate(context.Background(), Request{System: []string{"a", "b"}, User: "[task:pet_action]\n{}",
		Image: []byte{1, 2}, SchemaName: "PetAction", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != `{"speech":"hai"}` || *path != "/v1/chat/completions" || hdr.Get("Authorization") != "Bearer sk-oa" {
		t.Fatalf("res %q path %s auth %q", res.Text, *path, hdr.Get("Authorization"))
	}
	rf := (*body)["response_format"].(map[string]any)
	js := rf["json_schema"].(map[string]any)
	if rf["type"] != "json_schema" || js["name"] != "PetAction" || js["strict"] != true {
		t.Fatalf("response_format %v", rf)
	}
	msgs := (*body)["messages"].([]any)
	user := msgs[1].(map[string]any)["content"].([]any)
	img := user[0].(map[string]any)["image_url"].(map[string]any)["url"].(string)
	if !strings.HasPrefix(img, "data:image/jpeg;base64,") {
		t.Fatalf("image %v", img)
	}
}

func TestBaseNormalization(t *testing.T) {
	if got := openAIBase("http://localhost:11434"); got != "http://localhost:11434/v1/" {
		t.Error(got)
	}
	if got := openAIBase("https://openrouter.ai/api/v1"); got != "https://openrouter.ai/api/v1/" {
		t.Error(got)
	}
	if got := anthropicBase("http://127.0.0.1:47700/v1/"); got != "http://127.0.0.1:47700/" {
		t.Error(got)
	}
	if ExtractJSON("```json\n{\"a\":1}\n```") != `{"a":1}` {
		t.Error("extract")
	}
}
