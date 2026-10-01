package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testAnthKey = "sk-test-qa-anthropic-FAKE-unit"
	testOAIKey  = "sk-test-qa-openai-FAKE-unit"
)

func newTestServer(t *testing.T) (*server, *httptest.Server, string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "mock.jsonl")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	s := &server{started: time.Now(), scen: Scenario{Name: "normal"}, logFile: f, expectFP: map[string]string{}}
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return s, ts, logPath
}

func petActionSchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []any{"speech", "mood", "animation", "new_animation_request", "memory_ops", "suggestion"},
		"properties": map[string]any{
			"speech": map[string]any{"type": "string"}, "mood": map[string]any{"type": "string"},
			"animation": map[string]any{"type": "string"}, "suggestion": map[string]any{"type": "string"},
			"new_animation_request": map[string]any{"anyOf": []any{
				map[string]any{"type": "null"},
				map[string]any{"type": "object", "additionalProperties": false, "required": []any{"name", "description"},
					"properties": map[string]any{"name": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}}},
			}},
			"memory_ops": map[string]any{"type": "array", "items": map[string]any{
				"type": "object", "additionalProperties": false, "required": []any{"op", "id", "kind", "content"},
				"properties": map[string]any{"op": map[string]any{"type": "string"}, "id": map[string]any{"type": "integer"},
					"kind": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}}},
		},
	}
}

func tinyJPEG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, x%h, color.RGBA{255, 0, 0, 255})
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func anthropicBody(task string, ctx map[string]any, imgB64 string, stream bool) []byte {
	content := []any{}
	if imgB64 != "" {
		content = append(content, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/jpeg", "data": imgB64}})
	}
	content = append(content, map[string]any{"type": "text", "text": "[task:" + task + "]\n" + mustJSON(ctx)})
	body := map[string]any{
		"model": "claude-opus-5-5", "max_tokens": 1024, "stream": stream,
		"system":        []any{map[string]any{"type": "text", "text": "persona", "cache_control": map[string]any{"type": "ephemeral"}}},
		"messages":      []any{map[string]any{"role": "user", "content": content}},
		"output_config": map[string]any{"effort": "low", "format": map[string]any{"type": "json_schema", "schema": petActionSchema()}},
	}
	return []byte(mustJSON(body))
}

func openaiBody(task string, ctx map[string]any, imgB64 string, strict bool, schema map[string]any) []byte {
	parts := []any{map[string]any{"type": "text", "text": "[task:" + task + "]\n" + mustJSON(ctx)}}
	if imgB64 != "" {
		parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/jpeg;base64," + imgB64}})
	}
	if schema == nil {
		schema = petActionSchema()
	}
	return []byte(mustJSON(map[string]any{
		"model":    "gpt-qa-mini",
		"messages": []any{map[string]any{"role": "system", "content": "persona"}, map[string]any{"role": "user", "content": parts}},
		"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{
			"name": "pet_action", "strict": strict, "schema": schema}},
	}))
}

func post(t *testing.T, url string, body []byte, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func anthHdr() map[string]string {
	return map[string]string{"x-api-key": testAnthKey, "anthropic-version": "2023-06-01"}
}
func oaiHdr() map[string]string { return map[string]string{"Authorization": "Bearer " + testOAIKey} }

func setScenario(t *testing.T, base string, sc Scenario) {
	t.Helper()
	resp, b := post(t, base+"/mock/scenario", []byte(mustJSON(sc)), nil)
	if resp.StatusCode != 200 {
		t.Fatalf("set scenario: %d %s", resp.StatusCode, b)
	}
}

func lastEntry(t *testing.T, s *server) *Entry {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) == 0 {
		t.Fatal("no entries")
	}
	return s.entries[len(s.entries)-1]
}

func anthropicText(t *testing.T, b []byte) (map[string]any, string) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("response not JSON: %v %s", err, b)
	}
	for _, k := range []string{"id", "type", "role", "model", "content", "stop_reason", "usage"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("anthropic response missing %q: %s", k, b)
		}
	}
	text := ""
	for _, c := range m["content"].([]any) {
		cm := c.(map[string]any)
		if cm["type"] == "text" {
			text = cm["text"].(string)
		}
	}
	return m, text
}

// ------------------------------------------------------------------ fixtures

func TestFixturesValidity(t *testing.T) {
	r := ValidateSpec(roundTrip(ValidSpec("qa_spin", "generic")))
	if !r.Valid || len(r.ClampViolations) > 0 {
		t.Fatalf("ValidSpec must be valid without clamp violations: %+v", r)
	}
	want := map[string]string{
		"invalid_anim":          "slot",
		"invalid_anim_slot":     "slot",
		"invalid_anim_prop":     "prop",
		"invalid_anim_times":    "strictly increasing",
		"invalid_anim_duration": "duration",
		"invalid_anim_len":      "length differ",
		"invalid_anim_scale":    "[x,y,z]",
		"invalid_anim_tracks":   "tracks: 25",
		"invalid_anim_keys":     "65 keys",
		"invalid_anim_code":     "not a number",
		"invalid_anim_name":     "snake_case",
	}
	for sc, frag := range want {
		r := ValidateSpec(roundTrip(InvalidSpec(sc)))
		if r.Valid {
			t.Errorf("%s: fixture unexpectedly valid", sc)
			continue
		}
		joined := strings.Join(r.Errors, " | ")
		if !strings.Contains(joined, frag) {
			t.Errorf("%s: expected error containing %q, got %s", sc, frag, joined)
		}
		if sc != "invalid_anim" {
			for _, e := range r.Errors {
				if !strings.Contains(e, frag) {
					t.Errorf("%s: single-rule fixture must break only the %q rule, also got: %s", sc, frag, e)
				}
			}
		}
	}
	if sc := "invalid_anim"; len(ValidateSpec(roundTrip(InvalidSpec(sc))).Errors) < 3 {
		t.Errorf("combined fixture should break >= 3 rules")
	}
	c := ValidateSpec(roundTrip(ClampSpec("qa_clamp", "generic")))
	if !c.Valid || len(c.ClampViolations) < 4 {
		t.Fatalf("ClampSpec must be structurally valid with clamp violations: %+v", c)
	}
	for n := range scenarioDefs {
		if strings.HasPrefix(n, "invalid_anim") && InvalidSpec(n) == nil {
			t.Errorf("missing fixture for %s", n)
		}
	}
}

func roundTrip(v any) any {
	var out any
	_ = json.Unmarshal([]byte(mustJSON(v)), &out)
	return out
}

func TestStrictSchemaChecker(t *testing.T) {
	if iss := strictSchemaIssues(petActionSchema()); len(iss) != 0 {
		t.Fatalf("good schema flagged: %v", iss)
	}
	bad := petActionSchema()
	delete(bad, "additionalProperties")
	bad["required"] = []any{"speech"}
	iss := strictSchemaIssues(bad)
	if len(iss) < 2 {
		t.Fatalf("expected issues, got %v", iss)
	}
}

// ------------------------------------------------------------------ anthropic

func TestAnthropicPetActionWithActivityAndImage(t *testing.T) {
	s, ts, logPath := newTestServer(t)
	ctx := map[string]any{"occasion": "screenshot_insight", "localTime": "2026-10-01T10:00:00+07:00",
		"activity": map[string]any{"app": "code.exe", "title": "main.go - [email]"}}
	resp, b := post(t, ts.URL+"/anthropic/v1/messages", anthropicBody("pet_action", ctx, tinyJPEG(t, 64, 32), false), anthHdr())
	if resp.StatusCode != 200 {
		t.Fatalf("status %d %s", resp.StatusCode, b)
	}
	m, text := anthropicText(t, b)
	if m["type"] != "message" || m["role"] != "assistant" || m["stop_reason"] != "end_turn" {
		t.Fatalf("bad message envelope: %s", b)
	}
	var pa map[string]any
	if err := json.Unmarshal([]byte(text), &pa); err != nil {
		t.Fatalf("text is not JSON: %v", err)
	}
	for _, k := range []string{"speech", "mood", "animation", "new_animation_request", "memory_ops", "suggestion"} {
		if _, ok := pa[k]; !ok {
			t.Fatalf("PetAction missing %q", k)
		}
	}
	e := lastEntry(t, s)
	if e.Provider != "anthropic" || e.Endpoint != "messages" || !e.PathHasV1 || e.AuthScheme != "x-api-key" {
		t.Fatalf("classification wrong: %+v", e)
	}
	if e.KeyFingerprint != fingerprint(testAnthKey) || len(e.KeyFingerprint) != 8 {
		t.Fatalf("fingerprint wrong: %q", e.KeyFingerprint)
	}
	if e.TaskTag != "pet_action" || !e.TaskTagAtStart || e.Occasion != "screenshot_insight" || !e.HasLocalTime || !e.HasActivity {
		t.Fatalf("task/context analysis wrong: %+v", e)
	}
	if e.ImageCount != 1 || e.Images[0].Width != 64 || e.Images[0].Height != 32 || !e.Images[0].JPEGMagic || e.Images[0].MediaType != "image/jpeg" {
		t.Fatalf("image analysis wrong: %+v", e.Images)
	}
	if e.StructuredOutput != "anthropic:json_schema" || !e.CacheControl || e.Effort != "low" {
		t.Fatalf("structured/caching analysis wrong: %+v", e)
	}
	raw, _ := os.ReadFile(logPath)
	if bytes.Contains(raw, []byte(testAnthKey)) {
		t.Fatal("raw key leaked into the mock log")
	}
	if !bytes.Contains(raw, []byte(fingerprint(testAnthKey))) || !bytes.Contains(raw, []byte(`"taskTag":"pet_action"`)) {
		t.Fatal("log line missing fingerprint/task")
	}
	if strings.Count(strings.TrimSpace(string(raw)), "\n") != 0 {
		t.Fatal("expected exactly one JSON line")
	}
}

func TestAnthropicValidationErrors(t *testing.T) {
	_, ts, _ := newTestServer(t)
	hdr := map[string]string{"x-api-key": testAnthKey} // no anthropic-version
	resp, b := post(t, ts.URL+"/anthropic/v1/messages", anthropicBody("pet_action", map[string]any{"occasion": "greet"}, "", false), hdr)
	if resp.StatusCode != 400 || !strings.Contains(string(b), "anthropic-version") || !strings.Contains(string(b), `"type":"error"`) {
		t.Fatalf("want 400 anthropic-version error, got %d %s", resp.StatusCode, b)
	}
	resp, b = post(t, ts.URL+"/anthropic/v1/messages", anthropicBody("pet_action", map[string]any{}, "", false), map[string]string{"anthropic-version": "2023-06-01"})
	if resp.StatusCode != 401 {
		t.Fatalf("want 401 without key, got %d %s", resp.StatusCode, b)
	}
}

func TestScenariosAnthropicAndOpenAI(t *testing.T) {
	s, ts, _ := newTestServer(t)
	// new_anim -> pet_action requests qa_spin, animation_spec returns qa_spin valid
	setScenario(t, ts.URL, Scenario{Name: "new_anim"})
	_, b := post(t, ts.URL+"/anthropic/v1/messages", anthropicBody("pet_action", map[string]any{"occasion": "user_click"}, "", false), anthHdr())
	_, text := anthropicText(t, b)
	if !strings.Contains(text, `"new_animation_request":{"description":"QA mock: qa_spin","name":"qa_spin"}`) {
		t.Fatalf("new_anim pet_action wrong: %s", text)
	}
	_, b = post(t, ts.URL+"/anthropic/v1/messages", anthropicBody("animation_spec", map[string]any{"name": "qa_spin"}, "", false), anthHdr())
	_, text = anthropicText(t, b)
	if r := ValidateSpecJSON([]byte(text)); !r.Valid || r.Name != "qa_spin" {
		t.Fatalf("new_anim spec invalid: %+v %s", r, text)
	}
	// invalid_anim -> invalid spec via OpenAI
	setScenario(t, ts.URL, Scenario{Name: "invalid_anim"})
	resp, b := post(t, ts.URL+"/openai/v1/chat/completions", openaiBody("animation_spec", map[string]any{"name": "qa_bad_combo"}, "", true, nil), oaiHdr())
	if resp.StatusCode != 200 {
		t.Fatalf("openai status %d %s", resp.StatusCode, b)
	}
	var oc map[string]any
	_ = json.Unmarshal(b, &oc)
	if oc["object"] != "chat.completion" {
		t.Fatalf("bad openai envelope %s", b)
	}
	ch := oc["choices"].([]any)[0].(map[string]any)
	content := ch["message"].(map[string]any)["content"].(string)
	if ch["finish_reason"] != "stop" || ValidateSpecJSON([]byte(content)).Valid {
		t.Fatalf("invalid_anim should return invalid spec: %s", content)
	}
	if e := lastEntry(t, s); e.Provider != "openai" || e.AuthScheme != "bearer" || e.StructuredOutput != "openai:json_schema(strict)" || e.KeyFingerprint != fingerprint(testOAIKey) {
		t.Fatalf("openai analysis wrong: %+v", e)
	}
	// memory
	setScenario(t, ts.URL, Scenario{Name: "memory", MemoryContent: "QA-HABIT-X"})
	_, b = post(t, ts.URL+"/openai/chat/completions", openaiBody("pet_action", map[string]any{"occasion": "random_chatter"}, "", true, nil), oaiHdr())
	if !strings.Contains(string(b), `QA-HABIT-X`) {
		t.Fatalf("memory scenario missing content: %s", b)
	}
	if e := lastEntry(t, s); e.PathHasV1 {
		t.Fatalf("expected pathHasV1=false for /openai/chat/completions")
	}
	_, b = post(t, ts.URL+"/anthropic/v1/messages", anthropicBody("memory_ops", map[string]any{"occasion": "consolidate"}, "", false), anthHdr())
	_, text = anthropicText(t, b)
	if !strings.Contains(text, `"memory_ops":[{"content":"QA-CONSOLIDATED QA-HABIT-X"`) {
		t.Fatalf("memory_ops wrong: %s", text)
	}
	// refusal
	setScenario(t, ts.URL, Scenario{Name: "refusal"})
	_, b = post(t, ts.URL+"/anthropic/v1/messages", anthropicBody("pet_action", map[string]any{}, "", false), anthHdr())
	m, _ := anthropicText(t, b)
	if m["stop_reason"] != "refusal" {
		t.Fatalf("refusal: %s", b)
	}
	_, b = post(t, ts.URL+"/openai/v1/chat/completions", openaiBody("pet_action", map[string]any{}, "", true, nil), oaiHdr())
	if !strings.Contains(string(b), `"refusal":"I'm sorry`) || !strings.Contains(string(b), `"content":null`) {
		t.Fatalf("openai refusal: %s", b)
	}
	// http429 once, then normal
	setScenario(t, ts.URL, Scenario{Name: "http429", Count: 1, Then: "normal"})
	resp, b = post(t, ts.URL+"/anthropic/v1/messages", anthropicBody("pet_action", map[string]any{}, "", false), anthHdr())
	if resp.StatusCode != 429 || resp.Header.Get("retry-after") != "1" || !strings.Contains(string(b), "rate_limit_error") {
		t.Fatalf("429: %d %v %s", resp.StatusCode, resp.Header, b)
	}
	resp, _ = post(t, ts.URL+"/anthropic/v1/messages", anthropicBody("pet_action", map[string]any{}, "", false), anthHdr())
	if resp.StatusCode != 200 {
		t.Fatalf("count/then did not revert to normal: %d", resp.StatusCode)
	}
	// http401 openai shape
	setScenario(t, ts.URL, Scenario{Name: "http401"})
	resp, b = post(t, ts.URL+"/openai/v1/chat/completions", openaiBody("pet_action", map[string]any{}, "", true, nil), oaiHdr())
	if resp.StatusCode != 401 || !strings.Contains(string(b), `"code":"invalid_api_key"`) || strings.Contains(string(b), testOAIKey) {
		t.Fatalf("401: %d %s", resp.StatusCode, b)
	}
}

func TestOpenAIStrictSchemaRejected(t *testing.T) {
	_, ts, _ := newTestServer(t)
	bad := petActionSchema()
	bad["required"] = []any{"speech"}
	resp, b := post(t, ts.URL+"/openai/v1/chat/completions", openaiBody("pet_action", map[string]any{}, "", true, bad), oaiHdr())
	if resp.StatusCode != 400 || !strings.Contains(string(b), "not listed in required") {
		t.Fatalf("want strict schema 400, got %d %s", resp.StatusCode, b)
	}
	// missing model
	body := []byte(`{"messages":[{"role":"user","content":"[task:pet_action]\n{}"}]}`)
	resp, b = post(t, ts.URL+"/openai/v1/chat/completions", body, oaiHdr())
	if resp.StatusCode != 400 || !strings.Contains(string(b), "model") {
		t.Fatalf("want missing-model 400, got %d %s", resp.StatusCode, b)
	}
}

func TestStreaming(t *testing.T) {
	_, ts, _ := newTestServer(t)
	setScenario(t, ts.URL, Scenario{Name: "normal", ThinkingBlock: true})
	resp, b := post(t, ts.URL+"/anthropic/v1/messages", anthropicBody("pet_action", map[string]any{"occasion": "greet"}, "", true), anthHdr())
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("anthropic stream: %d", resp.StatusCode)
	}
	text := ""
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev map[string]any
		_ = json.Unmarshal([]byte(line[6:]), &ev)
		if ev["type"] == "content_block_delta" {
			d := ev["delta"].(map[string]any)
			if d["type"] == "text_delta" {
				text += d["text"].(string)
			}
		}
	}
	var pa map[string]any
	if err := json.Unmarshal([]byte(text), &pa); err != nil || pa["mood"] == nil {
		t.Fatalf("reassembled anthropic stream text is not a PetAction: %q", text)
	}
	body := openaiBody("pet_action", map[string]any{"occasion": "greet"}, "", true, nil)
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	m["stream"] = true
	resp, b = post(t, ts.URL+"/openai/v1/chat/completions", []byte(mustJSON(m)), oaiHdr())
	if resp.StatusCode != 200 || !strings.Contains(string(b), "data: [DONE]") || !strings.Contains(string(b), "chat.completion.chunk") {
		t.Fatalf("openai stream: %d %s", resp.StatusCode, b)
	}
}

func TestControlEndpoints(t *testing.T) {
	s, ts, _ := newTestServer(t)
	post(t, ts.URL+"/anthropic/v1/messages", anthropicBody("pet_action", map[string]any{"occasion": "greet", "note": "NEEDLE-123 a@b.co"}, "", false), anthHdr())
	post(t, ts.URL+"/openai/v1/chat/completions", openaiBody("pet_action", map[string]any{"occasion": "chat"}, "", true, nil), oaiHdr())
	resp, b := post(t, ts.URL+"/mock/search", []byte(`{"needles":["NEEDLE-123","a@b.co","absent-xyz"]}`), nil)
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"NEEDLE-123":[1]`) || !strings.Contains(string(b), `"absent-xyz":[]`) {
		t.Fatalf("search: %s", b)
	}
	r, _ := http.Get(ts.URL + "/mock/requests?provider=openai&brief=1")
	rb, _ := io.ReadAll(r.Body)
	var arr []map[string]any
	_ = json.Unmarshal(rb, &arr)
	if len(arr) != 1 || arr[0]["provider"] != "openai" || arr[0]["body"] != nil {
		t.Fatalf("requests filter/brief: %s", rb)
	}
	r, _ = http.Get(ts.URL + "/mock/requests")
	rb, _ = io.ReadAll(r.Body)
	if !strings.Contains(string(rb), `"body":{`) {
		t.Fatalf("full requests should contain body")
	}
	resp, b = post(t, ts.URL+"/mock/reset", []byte(`{"scenario":"new_anim","animName":"qa_spin_oai"}`), nil)
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"cleared":2`) {
		t.Fatalf("reset: %s", b)
	}
	if len(s.entries) != 0 || s.scen.Name != "new_anim" || s.scen.AnimName != "qa_spin_oai" {
		t.Fatalf("reset did not clear / set scenario")
	}
	resp, _ = post(t, ts.URL+"/mock/scenario", []byte(`{"scenario":"nope"}`), nil)
	if resp.StatusCode != 400 {
		t.Fatalf("unknown scenario should be 400")
	}
	resp, b = post(t, ts.URL+"/mock/validate", []byte(mustJSON(InvalidSpec("invalid_anim_slot"))), nil)
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"valid":false`) {
		t.Fatalf("validate: %s", b)
	}
	r, _ = http.Get(ts.URL + "/anthropic/v1/models")
	if r.StatusCode != 401 {
		t.Fatalf("models without key should be 401")
	}
	req, _ := http.NewRequest("GET", ts.URL+"/anthropic/v1/models", nil)
	req.Header.Set("x-api-key", testAnthKey)
	r, _ = http.DefaultClient.Do(req)
	rb, _ = io.ReadAll(r.Body)
	if r.StatusCode != 200 || !strings.Contains(string(rb), `"claude-opus-5-5"`) || !strings.Contains(string(rb), `"has_more":false`) {
		t.Fatalf("models: %s", rb)
	}
}

func TestNoTaskTagAndDrop(t *testing.T) {
	s, ts, _ := newTestServer(t)
	body := []byte(mustJSON(map[string]any{"model": "claude-opus-5-5", "max_tokens": 10,
		"messages": []any{map[string]any{"role": "user", "content": "hello"}}}))
	resp, _ := post(t, ts.URL+"/anthropic/v1/messages", body, anthHdr())
	if resp.StatusCode != 200 || lastEntry(t, s).TaskTag != "" || len(lastEntry(t, s).Warnings) == 0 {
		t.Fatalf("no-tag request should still answer and warn")
	}
	setScenario(t, ts.URL, Scenario{Name: "drop"})
	req, _ := http.NewRequest("POST", ts.URL+"/anthropic/v1/messages", bytes.NewReader(body))
	for k, v := range anthHdr() {
		req.Header.Set(k, v)
	}
	if _, err := http.DefaultClient.Do(req); err == nil {
		t.Fatalf("drop scenario should produce a transport error")
	}
}
