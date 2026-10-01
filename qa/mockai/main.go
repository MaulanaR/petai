// mockai is a stdlib-only mock of the Anthropic Messages API and the OpenAI
// Chat Completions API, used by the PetAI QA harness (qa/scripts).
//
// It listens on 127.0.0.1 only, logs every AI request as one JSON line
// (key FINGERPRINT only, never the raw key) and answers with structured JSON
// chosen by the current scenario and the request's [task:...] tag.
//
// Routing (base URL forms accepted):
//
//	http://127.0.0.1:47700/anthropic  -> /anthropic/v1/messages, /anthropic/v1/models
//	http://127.0.0.1:47700/openai     -> /openai/v1/chat/completions (also /openai/chat/completions), /openai/v1/models
//	http://127.0.0.1:47700            -> /v1/messages, /v1/chat/completions, /v1/models (provider sniffed from headers)
//
// Control endpoints: GET /mock/health, GET|POST /mock/scenario, GET /mock/requests,
// POST /mock/reset, GET /mock/stats, POST /mock/search, POST /mock/validate.
//
// Usage:
//
//	mockai -addr 127.0.0.1:47700 -log qa/artifacts/mockai.jsonl [-scenario normal] [-lenient]
//	mockai -validate spec1.json [spec2.json ...]   (validate AnimationSpec files, exit 1 if any invalid)
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type server struct {
	mu       sync.Mutex
	entries  []*Entry
	seq      int64
	logFile  *os.File
	logMu    sync.Mutex
	scen     Scenario
	lenient  bool
	started  time.Time
	expectFP map[string]string // provider -> expected key fingerprint (optional)
}

func main() {
	addr := flag.String("addr", "127.0.0.1:47700", "listen address (must be loopback)")
	logPath := flag.String("log", "", "append one JSON line per request to this file")
	scen := flag.String("scenario", "", "initial scenario (default $MOCKAI_SCENARIO or normal)")
	lenient := flag.Bool("lenient", false, "do not reject requests the real API would reject (400); only log them")
	anthFP := flag.String("anthropic-key-fp", "", "optional: expected sha256[:8] of the Anthropic key; mismatch -> 401")
	oaiFP := flag.String("openai-key-fp", "", "optional: expected sha256[:8] of the OpenAI key; mismatch -> 401")
	validate := flag.Bool("validate", false, "validate AnimationSpec JSON files given as arguments and exit")
	listScen := flag.Bool("list-scenarios", false, "print scenarios and exit")
	flag.Parse()

	if *listScen {
		for _, n := range scenarioNames() {
			fmt.Printf("%-22s %s\n", n, scenarioDefs[n].desc)
		}
		return
	}
	if *validate {
		os.Exit(runValidate(flag.Args()))
	}

	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		log.Fatalf("bad -addr: %v", err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		log.Fatalf("refusing to listen on non-loopback address %q", *addr)
	}

	s := &server{started: time.Now(), lenient: *lenient, expectFP: map[string]string{}}
	if *anthFP != "" {
		s.expectFP["anthropic"] = strings.ToLower(*anthFP)
	}
	if *oaiFP != "" {
		s.expectFP["openai"] = strings.ToLower(*oaiFP)
	}
	name := *scen
	if name == "" {
		name = os.Getenv("MOCKAI_SCENARIO")
	}
	if name == "" {
		name = "normal"
	}
	if _, ok := scenarioDefs[name]; !ok {
		log.Fatalf("unknown scenario %q (see -list-scenarios)", name)
	}
	s.scen = Scenario{Name: name}
	if *logPath != "" {
		f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.Fatalf("open log: %v", err)
		}
		s.logFile = f
		defer f.Close()
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("mockai listening on http://%s (scenario=%s, lenient=%v, log=%s)", ln.Addr(), name, *lenient, *logPath)
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.Serve(ln))
}

func runValidate(files []string) int {
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: mockai -validate file.json [...]")
		return 2
	}
	bad := 0
	out := map[string]SpecReport{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			out[f] = SpecReport{Errors: []string{err.Error()}, Warnings: []string{}, ClampViolations: []string{}}
			bad++
			continue
		}
		r := ValidateSpecJSON(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")))
		if !r.Valid {
			bad++
		}
		out[f] = r
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
	if bad > 0 {
		return 1
	}
	return 0
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			log.Printf("panic handling %s %s: %v", r.Method, r.URL.Path, rec)
			http.Error(w, "mock internal error", 500)
		}
	}()
	if r.URL.Path == "/mock" || strings.HasPrefix(r.URL.Path, "/mock/") {
		s.control(w, r)
		return
	}
	s.handleAI(w, r)
}

// ---------------------------------------------------------------- logging

func (s *server) writeLog(v any) {
	if s.logFile == nil {
		return
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		log.Printf("log encode: %v", err)
		return
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()
	_, _ = s.logFile.Write(buf.Bytes())
}

func (s *server) nextScenario() Scenario {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.scen
	if s.scen.Count > 0 {
		s.scen.Count--
		if s.scen.Count == 0 {
			then := s.scen.Then
			if then == "" {
				then = "normal"
			}
			s.scen = Scenario{Name: then}
		}
	}
	return cur
}

// ---------------------------------------------------------------- AI endpoints

func (s *server) handleAI(w http.ResponseWriter, r *http.Request) {
	e := &Entry{Kind: "ai", Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Time: time.Now().Format(time.RFC3339Nano)}
	classify(r, e)
	key := extractAuth(r, e)
	body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if r.Method == http.MethodPost {
		analyzeBody(e, body, key)
	} else {
		e.Images = []ImageInfo{}
	}

	s.mu.Lock()
	s.seq++
	e.Seq = s.seq
	s.mu.Unlock()

	status, kind := s.respondAI(w, r, e, key)
	e.ResponseStatus, e.ResponseKind = status, kind

	s.mu.Lock()
	s.entries = append(s.entries, e)
	s.mu.Unlock()
	s.writeLog(e)
}

func (s *server) respondAI(w http.ResponseWriter, r *http.Request, e *Entry, key string) (int, string) {
	prov := e.Provider
	// Models API (no scenario applied except auth errors).
	if e.Endpoint == "models" || e.Endpoint == "model_get" {
		if r.Method != http.MethodGet {
			return s.writeError(w, e, 405, "invalid_request_error", "method not allowed", false), "error"
		}
		if !e.AuthHeaderPresent {
			return s.writeError(w, e, 401, "authentication_error", "missing API key", false), "error"
		}
		if exp := s.expectFP[prov]; exp != "" && exp != e.KeyFingerprint {
			return s.writeError(w, e, 401, "authentication_error", "invalid API key (fingerprint mismatch)", false), "error"
		}
		e.Scenario = "models"
		writeJSON(w, 200, modelsList(prov, e.Endpoint, r.URL.Path), nil)
		return 200, "models"
	}
	if e.Endpoint == "count_tokens" {
		writeJSON(w, 200, map[string]any{"input_tokens": 1 + e.BodyBytes/4}, nil)
		return 200, "count_tokens"
	}
	if r.Method != http.MethodPost || (e.Endpoint != "messages" && e.Endpoint != "chat_completions") {
		e.warn("endpoint not part of the contract: %s %s", r.Method, r.URL.Path)
		return s.writeError(w, e, 404, "not_found_error", "mock: unknown endpoint "+r.URL.Path, false), "not_found"
	}
	if prov == "anthropic" && e.Endpoint != "messages" || prov == "openai" && e.Endpoint != "chat_completions" {
		e.invalid("endpoint %s does not belong to provider %s", e.Endpoint, prov)
		return s.writeError(w, e, 404, "not_found_error", "mock: wrong endpoint for provider "+prov, false), "not_found"
	}

	sc := s.nextScenario()
	e.Scenario = sc.Name

	// Auth first (like the real APIs).
	if !e.AuthHeaderPresent {
		return s.writeError(w, e, 401, "authentication_error", "missing API key header", false), "error_auth"
	}
	if prov == "anthropic" && e.AuthScheme != "x-api-key" {
		e.invalid("Anthropic auth must use the x-api-key header (got %s)", e.AuthScheme)
		return s.writeError(w, e, 401, "authentication_error", "x-api-key header is required", false), "error_auth"
	}
	if prov == "openai" && e.AuthScheme != "bearer" {
		e.invalid("OpenAI auth must use Authorization: Bearer (got %s)", e.AuthScheme)
		return s.writeError(w, e, 401, "invalid_request_error", "You didn't provide an API key (Authorization: Bearer).", false), "error_auth"
	}
	if exp := s.expectFP[prov]; exp != "" && exp != e.KeyFingerprint {
		return s.writeError(w, e, 401, "authentication_error", "invalid API key (fingerprint mismatch)", false), "error_auth"
	}
	if len(e.ValidationErrors) > 0 && !s.lenient {
		return s.writeError(w, e, 400, "invalid_request_error", strings.Join(e.ValidationErrors, "; "), true), "validation_error"
	}

	switch sc.Name {
	case "http400":
		return s.writeError(w, e, 400, "invalid_request_error", "mock: forced 400", sc.NoRetry), "error"
	case "http401":
		return s.writeError(w, e, 401, "authentication_error", "invalid x-api-key / Incorrect API key provided", sc.NoRetry), "error"
	case "http429":
		return s.writeError(w, e, 429, "rate_limit_error", "mock: rate limit exceeded", sc.NoRetry), "error"
	case "http500":
		return s.writeError(w, e, 500, "api_error", "mock: internal server error", sc.NoRetry), "error"
	case "http529":
		if prov == "anthropic" {
			return s.writeError(w, e, 529, "overloaded_error", "mock: overloaded", sc.NoRetry), "error"
		}
		return s.writeError(w, e, 503, "server_error", "mock: service unavailable", sc.NoRetry), "error"
	case "drop":
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
				return 0, "dropped"
			}
		}
		panic(http.ErrAbortHandler)
	case "slow":
		d := sc.DelayMs
		if d <= 0 {
			d = 8000
		}
		select {
		case <-time.After(time.Duration(d) * time.Millisecond):
		case <-r.Context().Done():
			return 0, "client_gone_during_delay"
		}
	}

	model := e.Model
	if model == "" {
		model = "mock-model"
	}
	task := e.TaskTag
	if task == "" {
		task = "pet_action"
	}
	text, stop := "", "end_turn"
	switch sc.Name {
	case "refusal":
		stop = "refusal"
	case "malformed":
		text = "Tentu! Ini JSON-nya: {\"speech\": \"halo\", \"mood\": "
	case "wrong_shape":
		text = `{"message":"halo dari mock","unexpected":true,"items":[1,2,3]}`
	default:
		payload, name := buildPayload(sc, task, e)
		e.ResponseName = name
		text = mustJSON(payload)
	}
	e.ResponseText = truncate(text, 4000)
	if e.Stream {
		if prov == "anthropic" {
			streamAnthropic(w, e, model, text, stop, sc.ThinkingBlock)
		} else {
			streamOpenAI(w, e, model, text, stop == "refusal")
		}
		return 200, "stream:" + stop
	}
	if prov == "anthropic" {
		writeJSON(w, 200, anthropicMessage(e, model, text, stop, sc.ThinkingBlock), map[string]string{"request-id": reqID(e)})
	} else {
		writeJSON(w, 200, openaiCompletion(e, model, text, stop == "refusal"), map[string]string{"x-request-id": reqID(e)})
	}
	return 200, stop
}

func reqID(e *Entry) string { return fmt.Sprintf("req_mock_%06d", e.Seq) }

func writeJSON(w http.ResponseWriter, status int, v any, headers map[string]string) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	w.Header().Set("Content-Type", "application/json")
	for k, val := range headers {
		w.Header().Set(k, val)
	}
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (s *server) writeError(w http.ResponseWriter, e *Entry, status int, typ, msg string, noRetry bool) int {
	h := map[string]string{}
	if status == 429 || status == 529 || status == 503 {
		h["retry-after"] = "1"
	}
	if noRetry {
		h["x-should-retry"] = "false"
	}
	if e.Provider == "anthropic" {
		h["request-id"] = reqID(e)
		writeJSON(w, status, map[string]any{"type": "error", "error": map[string]any{"type": typ, "message": msg}, "request_id": reqID(e)}, h)
		return status
	}
	code := any(nil)
	otype := typ
	switch status {
	case 401:
		code, otype = "invalid_api_key", "invalid_request_error"
	case 429:
		code, otype = "rate_limit_exceeded", "requests"
	case 500, 503:
		otype = "server_error"
	case 400, 404:
		otype = "invalid_request_error"
	}
	h["x-request-id"] = reqID(e)
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": msg, "type": otype, "param": nil, "code": code}}, h)
	return status
}

func usageNums(e *Entry, text string) (int64, int64) {
	return int64(1 + e.BodyBytes/4), int64(1 + len(text)/4)
}

func anthropicMessage(e *Entry, model, text, stop string, thinking bool) map[string]any {
	in, out := usageNums(e, text)
	content := []any{}
	if thinking {
		content = append(content, map[string]any{"type": "thinking", "thinking": "(mock) memikirkan jawaban...", "signature": "mock-signature-qa"})
	}
	if stop != "refusal" {
		content = append(content, map[string]any{"type": "text", "text": text, "citations": nil})
	}
	var stopDetails any
	if stop == "refusal" {
		stopDetails = map[string]any{"type": "refusal", "category": nil, "explanation": "mock refusal"}
	}
	return map[string]any{
		"id":            fmt.Sprintf("msg_mock_%06d", e.Seq),
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       content,
		"stop_reason":   stop,
		"stop_sequence": nil,
		"stop_details":  stopDetails,
		"container":     nil,
		"diagnostics":   nil,
		"usage": map[string]any{
			"input_tokens":                in,
			"output_tokens":               out,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens":     0,
			"cache_creation":              map[string]any{"ephemeral_5m_input_tokens": 0, "ephemeral_1h_input_tokens": 0},
			"server_tool_use":             nil,
			"service_tier":                "standard",
		},
	}
}

func openaiCompletion(e *Entry, model, text string, refusal bool) map[string]any {
	in, out := usageNums(e, text)
	msg := map[string]any{"role": "assistant", "content": text, "refusal": nil, "annotations": []any{}}
	if refusal {
		msg["content"] = nil
		msg["refusal"] = "I'm sorry, I can't help with that. (mock refusal)"
	}
	return map[string]any{
		"id":      fmt.Sprintf("chatcmpl-mock%06d", e.Seq),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{map[string]any{
			"index": 0, "message": msg, "logprobs": nil, "finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens": in, "completion_tokens": out, "total_tokens": in + out,
			"prompt_tokens_details":     map[string]any{"cached_tokens": 0, "audio_tokens": 0},
			"completion_tokens_details": map[string]any{"reasoning_tokens": 0, "audio_tokens": 0, "accepted_prediction_tokens": 0, "rejected_prediction_tokens": 0},
		},
		"service_tier":       "default",
		"system_fingerprint": "fp_mock_qa",
	}
}

func sseEvent(bw *bufio.Writer, event string, v any) {
	if event != "" {
		fmt.Fprintf(bw, "event: %s\n", event)
	}
	if s, ok := v.(string); ok {
		fmt.Fprintf(bw, "data: %s\n\n", s)
	} else {
		fmt.Fprintf(bw, "data: %s\n\n", mustJSON(v))
	}
}

func chunks(s string, n int) []string {
	out := []string{}
	r := []rune(s)
	for i := 0; i < len(r); i += n {
		j := i + n
		if j > len(r) {
			j = len(r)
		}
		out = append(out, string(r[i:j]))
	}
	return out
}

func streamAnthropic(w http.ResponseWriter, e *Entry, model, text, stop string, thinking bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("request-id", reqID(e))
	w.WriteHeader(200)
	bw := bufio.NewWriter(w)
	in, out := usageNums(e, text)
	start := anthropicMessage(e, model, "", stop, false)
	start["content"] = []any{}
	start["stop_reason"] = nil
	start["stop_details"] = nil
	start["usage"].(map[string]any)["output_tokens"] = 1
	sseEvent(bw, "message_start", map[string]any{"type": "message_start", "message": start})
	idx := 0
	if thinking {
		sseEvent(bw, "content_block_start", map[string]any{"type": "content_block_start", "index": idx, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}})
		sseEvent(bw, "content_block_delta", map[string]any{"type": "content_block_delta", "index": idx, "delta": map[string]any{"type": "thinking_delta", "thinking": "(mock) memikirkan jawaban..."}})
		sseEvent(bw, "content_block_delta", map[string]any{"type": "content_block_delta", "index": idx, "delta": map[string]any{"type": "signature_delta", "signature": "mock-signature-qa"}})
		sseEvent(bw, "content_block_stop", map[string]any{"type": "content_block_stop", "index": idx})
		idx++
	}
	if stop != "refusal" {
		sseEvent(bw, "content_block_start", map[string]any{"type": "content_block_start", "index": idx, "content_block": map[string]any{"type": "text", "text": ""}})
		for _, c := range chunks(text, 40) {
			sseEvent(bw, "content_block_delta", map[string]any{"type": "content_block_delta", "index": idx, "delta": map[string]any{"type": "text_delta", "text": c}})
		}
		sseEvent(bw, "content_block_stop", map[string]any{"type": "content_block_stop", "index": idx})
	}
	var sd any
	if stop == "refusal" {
		sd = map[string]any{"type": "refusal", "category": nil, "explanation": "mock refusal"}
	}
	sseEvent(bw, "message_delta", map[string]any{"type": "message_delta",
		"delta": map[string]any{"stop_reason": stop, "stop_sequence": nil, "stop_details": sd},
		"usage": map[string]any{"output_tokens": out, "input_tokens": in}})
	sseEvent(bw, "message_stop", map[string]any{"type": "message_stop"})
	_ = bw.Flush()
}

func streamOpenAI(w http.ResponseWriter, e *Entry, model, text string, refusal bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("x-request-id", reqID(e))
	w.WriteHeader(200)
	bw := bufio.NewWriter(w)
	id := fmt.Sprintf("chatcmpl-mock%06d", e.Seq)
	created := time.Now().Unix()
	chunk := func(delta map[string]any, finish any) map[string]any {
		return map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model,
			"system_fingerprint": "fp_mock_qa",
			"choices":            []any{map[string]any{"index": 0, "delta": delta, "logprobs": nil, "finish_reason": finish}}}
	}
	sseEvent(bw, "", chunk(map[string]any{"role": "assistant", "content": "", "refusal": nil}, nil))
	if refusal {
		sseEvent(bw, "", chunk(map[string]any{"refusal": "I'm sorry, I can't help with that. (mock refusal)"}, nil))
	} else {
		for _, c := range chunks(text, 40) {
			sseEvent(bw, "", chunk(map[string]any{"content": c}, nil))
		}
	}
	sseEvent(bw, "", chunk(map[string]any{}, "stop"))
	if so, ok := e.parsed["stream_options"].(map[string]any); ok {
		if iu, _ := so["include_usage"].(bool); iu {
			in, out := usageNums(e, text)
			sseEvent(bw, "", map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model,
				"choices": []any{}, "usage": map[string]any{"prompt_tokens": in, "completion_tokens": out, "total_tokens": in + out}})
		}
	}
	sseEvent(bw, "", "[DONE]")
	_ = bw.Flush()
}

func modelsList(prov, endpoint, path string) any {
	if prov == "anthropic" {
		models := []any{
			map[string]any{"type": "model", "id": "claude-opus-5-5", "display_name": "Claude Opus 5.5 (mock)", "created_at": "2026-08-01T00:00:00Z"},
			map[string]any{"type": "model", "id": "claude-sonnet-5-5", "display_name": "Claude Sonnet 5.5 (mock)", "created_at": "2026-08-01T00:00:00Z"},
			map[string]any{"type": "model", "id": "claude-haiku-4-5", "display_name": "Claude Haiku 4.5 (mock)", "created_at": "2025-10-01T00:00:00Z"},
		}
		if endpoint == "model_get" {
			id := path[strings.LastIndex(path, "/")+1:]
			return map[string]any{"type": "model", "id": id, "display_name": id + " (mock)", "created_at": "2026-08-01T00:00:00Z"}
		}
		return map[string]any{"data": models, "has_more": false, "first_id": "claude-opus-5-5", "last_id": "claude-haiku-4-5"}
	}
	mk := func(id string) any {
		return map[string]any{"id": id, "object": "model", "created": 1767225600, "owned_by": "mock-qa"}
	}
	if endpoint == "model_get" {
		return mk(path[strings.LastIndex(path, "/")+1:])
	}
	return map[string]any{"object": "list", "data": []any{mk("gpt-qa-mini"), mk("gpt-qa-vision"), mk("qa-mock-model")}}
}

// ---------------------------------------------------------------- control

func (s *server) control(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	q := r.URL.Query()
	switch {
	case path == "/mock/health" || path == "/mock":
		s.mu.Lock()
		resp := map[string]any{"ok": true, "scenario": s.scen, "requests": len(s.entries), "lastSeq": s.seq,
			"uptimeSec": int(time.Since(s.started).Seconds()), "lenient": s.lenient}
		s.mu.Unlock()
		writeJSON(w, 200, resp, nil)

	case path == "/mock/scenario" && r.Method == http.MethodGet:
		s.mu.Lock()
		cur := s.scen
		s.mu.Unlock()
		avail := map[string]string{}
		for k, v := range scenarioDefs {
			avail[k] = v.desc
		}
		writeJSON(w, 200, map[string]any{"current": cur, "available": avail}, nil)

	case path == "/mock/scenario" && r.Method == http.MethodPost:
		sc, err := parseScenario(r)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error(), "available": scenarioNames()}, nil)
			return
		}
		s.mu.Lock()
		s.scen = sc
		s.mu.Unlock()
		s.writeLog(map[string]any{"kind": "control", "time": time.Now().Format(time.RFC3339Nano), "control": "scenario", "scenario": sc})
		writeJSON(w, 200, map[string]any{"ok": true, "scenario": sc}, nil)

	case path == "/mock/reset" && r.Method == http.MethodPost:
		var sc *Scenario
		if r.ContentLength != 0 || q.Get("scenario") != "" {
			if parsed, err := parseScenario(r); err == nil {
				sc = &parsed
			} else if !errors.Is(err, errNoScenario) {
				writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()}, nil)
				return
			}
		}
		s.mu.Lock()
		n := len(s.entries)
		s.entries = nil
		if sc != nil {
			s.scen = *sc
		}
		cur, last := s.scen, s.seq
		s.mu.Unlock()
		s.writeLog(map[string]any{"kind": "control", "time": time.Now().Format(time.RFC3339Nano), "control": "reset", "cleared": n, "scenario": cur})
		writeJSON(w, 200, map[string]any{"ok": true, "cleared": n, "lastSeq": last, "scenario": cur}, nil)

	case path == "/mock/requests" && r.Method == http.MethodGet:
		writeJSON(w, 200, s.filtered(q), nil)

	case path == "/mock/stats":
		s.mu.Lock()
		st := map[string]any{"total": len(s.entries)}
		by := func(f func(*Entry) string) map[string]int {
			m := map[string]int{}
			for _, e := range s.entries {
				m[f(e)]++
			}
			return m
		}
		st["byProvider"] = by(func(e *Entry) string { return e.Provider })
		st["byEndpoint"] = by(func(e *Entry) string { return e.Endpoint })
		st["byTask"] = by(func(e *Entry) string { return e.TaskTag })
		st["byStatus"] = by(func(e *Entry) string { return strconv.Itoa(e.ResponseStatus) })
		st["byOccasion"] = by(func(e *Entry) string { return e.Occasion })
		s.mu.Unlock()
		writeJSON(w, 200, st, nil)

	case path == "/mock/search" && r.Method == http.MethodPost:
		var req struct {
			Needles  []string `json:"needles"`
			Provider string   `json:"provider"`
			Since    int64    `json:"since"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()}, nil)
			return
		}
		res := map[string][]int64{}
		s.mu.Lock()
		for _, n := range req.Needles {
			res[n] = []int64{}
			if n == "" {
				continue
			}
			for _, e := range s.entries {
				if e.Seq <= req.Since || (req.Provider != "" && e.Provider != req.Provider) {
					continue
				}
				if entryContains(e, n) {
					res[n] = append(res[n], e.Seq)
				}
			}
		}
		s.mu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true, "results": res}, nil)

	case path == "/mock/validate" && r.Method == http.MethodPost:
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		writeJSON(w, 200, ValidateSpecJSON(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))), nil)

	default:
		writeJSON(w, 404, map[string]any{"ok": false, "error": "unknown control endpoint",
			"endpoints": []string{"GET /mock/health", "GET|POST /mock/scenario", "GET /mock/requests?provider=&task=&endpoint=&since=&brief=1",
				"POST /mock/reset", "GET /mock/stats", "POST /mock/search", "POST /mock/validate"}}, nil)
	}
}

func entryContains(e *Entry, needle string) bool {
	if bytes.Contains(e.rawBody, []byte(needle)) {
		return true
	}
	for _, s := range e.strs {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

var errNoScenario = errors.New("no scenario given")

func parseScenario(r *http.Request) (Scenario, error) {
	var sc Scenario
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &sc); err != nil {
			return sc, fmt.Errorf("bad JSON: %v", err)
		}
	}
	q := r.URL.Query()
	if v := q.Get("scenario"); v != "" {
		sc.Name = v
	}
	if v := q.Get("count"); v != "" {
		sc.Count, _ = strconv.Atoi(v)
	}
	if v := q.Get("animName"); v != "" {
		sc.AnimName = v
	}
	if sc.Name == "" {
		return sc, errNoScenario
	}
	if _, ok := scenarioDefs[sc.Name]; !ok {
		return sc, fmt.Errorf("unknown scenario %q", sc.Name)
	}
	if sc.Then != "" {
		if _, ok := scenarioDefs[sc.Then]; !ok {
			return sc, fmt.Errorf("unknown 'then' scenario %q", sc.Then)
		}
	}
	return sc, nil
}

func (s *server) filtered(q map[string][]string) []any {
	get := func(k string) string {
		if v := q[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	prov, task, ep := get("provider"), get("task"), get("endpoint")
	brief := get("brief") == "1" || get("brief") == "true"
	since, _ := strconv.ParseInt(get("since"), 10, 64)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []any{}
	for _, e := range s.entries {
		if e.Seq <= since || (prov != "" && e.Provider != prov) || (task != "" && e.TaskTag != task) || (ep != "" && e.Endpoint != ep) {
			continue
		}
		if brief {
			c := *e
			c.Body, c.BodyText, c.ResponseText = nil, "", truncate(e.ResponseText, 300)
			out = append(out, &c)
		} else {
			out = append(out, e)
		}
	}
	return out
}
