package voice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"petai/internal/ai"
)

func gateway(t *testing.T, status int, content string, sawAudio *bool) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if sawAudio != nil {
			*sawAudio = strings.Contains(string(b), `"input_audio"`)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status != 200 {
			_, _ = w.Write([]byte(content))
			return
		}
		reply, _ := json.Marshal(content)
		_, _ = w.Write([]byte(`{"id":"c","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":` + string(reply) + `},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func provider(t *testing.T, base string) ai.Provider {
	p, err := ai.New(ai.Config{Provider: "openai", Model: "m", BaseURL: base, APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProbeSupported(t *testing.T) {
	saw := false
	r := Probe(context.Background(), provider(t, gateway(t, 200, `{"heard":"Satu, dua, tiga."}`, &saw)))
	if !r.Supported || !saw {
		t.Fatalf("want supported with audio sent, got %+v saw=%v", r, saw)
	}
}

func TestProbeSilentlyDroppedAudio(t *testing.T) {
	r := Probe(context.Background(), provider(t, gateway(t, 200, `{"heard":""}`, nil)))
	if r.Supported {
		t.Fatal("empty transcript must be unsupported")
	}
	r = Probe(context.Background(), provider(t, gateway(t, 200, `NOAUDIO`, nil)))
	if r.Supported {
		t.Fatal("NOAUDIO must be unsupported")
	}
}

func TestProbeGatewayRejectsWithSuggestions(t *testing.T) {
	// Same shape as the real gateway response.
	body := `{"error":{"message":"model 'qwen' does not accept audio input; models that do: cp/cline-pass/mimo-v2.5, cp/cline-pass/mimo-v2.6-flash, ocg/mimo-v2.5","type":"unsupported_modality"}}`
	r := Probe(context.Background(), provider(t, gateway(t, 400, body, nil)))
	if r.Supported {
		t.Fatal("400 must be unsupported")
	}
	if strings.Join(r.Suggestions, ",") != "mimo-v2.5,mimo-v2.6-flash" {
		t.Fatalf("suggestions %v err=%q", r.Suggestions, r.Error)
	}
}

func TestProbeAnthropicUnsupported(t *testing.T) {
	p, _ := ai.New(ai.Config{Provider: "anthropic", APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	if r := Probe(context.Background(), p); r.Supported {
		t.Fatal("Claude cannot hear")
	}
}

func TestHeardProbe(t *testing.T) {
	for s, want := range map[string]bool{"Satu, dua, tiga": true, "1 2 3": true, "one, two": true, "halo": false, "": false, "satu": false} {
		if heardProbe(s) != want {
			t.Errorf("heardProbe(%q) != %v", s, want)
		}
	}
}
