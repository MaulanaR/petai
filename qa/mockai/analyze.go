package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// ImageInfo describes one image found in a request.
type ImageInfo struct {
	Where     string `json:"where"`
	MediaType string `json:"mediaType"`
	Encoding  string `json:"encoding"` // base64 | data-url | url
	Bytes     int    `json:"bytes"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Format    string `json:"format"`
	JPEGMagic bool   `json:"jpegMagic"`
	Error     string `json:"error,omitempty"`
}

// Entry is one logged AI request (one JSON line in the log file).
type Entry struct {
	Seq               int64             `json:"seq"`
	Time              string            `json:"time"`
	Kind              string            `json:"kind"` // "ai" | "control"
	Provider          string            `json:"provider"`
	ProviderSource    string            `json:"providerSource"` // prefix | header | endpoint
	Method            string            `json:"method"`
	Path              string            `json:"path"`
	Query             string            `json:"query,omitempty"`
	PathHasV1         bool              `json:"pathHasV1"`
	Endpoint          string            `json:"endpoint"`
	AuthHeaderPresent bool              `json:"authHeaderPresent"`
	AuthScheme        string            `json:"authScheme"` // x-api-key | bearer | none | other
	KeyFingerprint    string            `json:"keyFingerprint"`
	Headers           map[string]string `json:"headers"`
	HeaderNames       []string          `json:"headerNames"`
	Model             string            `json:"model"`
	MaxTokens         int64             `json:"maxTokens"`
	Stream            bool              `json:"stream"`
	Effort            string            `json:"effort,omitempty"`
	Thinking          string            `json:"thinking,omitempty"`
	MessageCount      int               `json:"messageCount"`
	SystemChars       int               `json:"systemChars"`
	SystemSHA8        string            `json:"systemSha8,omitempty"`
	CacheControl      bool              `json:"cacheControl"`
	TaskTag           string            `json:"taskTag"`
	TaskTagAtStart    bool              `json:"taskTagAtStart"`
	LastUserTextHead  string            `json:"lastUserTextHead,omitempty"`
	Context           json.RawMessage   `json:"context,omitempty"`
	ContextError      string            `json:"contextError,omitempty"`
	Occasion          string            `json:"occasion"`
	HasLocalTime      bool              `json:"hasLocalTime"`
	HasActivity       bool              `json:"hasActivity"`
	Activity          json.RawMessage   `json:"activity,omitempty"`
	ImageCount        int               `json:"imageCount"`
	Images            []ImageInfo       `json:"images"`
	StructuredOutput  string            `json:"structuredOutput"`
	SchemaName        string            `json:"schemaName,omitempty"`
	SchemaIssues      []string          `json:"schemaIssues,omitempty"`
	ValidationErrors  []string          `json:"validationErrors,omitempty"`
	Warnings          []string          `json:"warnings,omitempty"`
	BodyContainsKey   bool              `json:"bodyContainsAuthKey"`
	Scenario          string            `json:"scenario"`
	ResponseStatus    int               `json:"responseStatus"`
	ResponseKind      string            `json:"responseKind"`
	ResponseName      string            `json:"responseName,omitempty"`
	ResponseText      string            `json:"responseText,omitempty"`
	BodyBytes         int               `json:"bodyBytes"`
	Body              json.RawMessage   `json:"body,omitempty"`
	BodyText          string            `json:"bodyText,omitempty"`
	Control           map[string]any    `json:"control,omitempty"`

	rawBody []byte
	parsed  map[string]any
	ctxObj  map[string]any
	strs    []string // all decoded string values of the body (for /mock/search)
}

var (
	taskTagRe      = regexp.MustCompile(`\[task:([A-Za-z0-9_]+)\]`)
	taskTagStartRe = regexp.MustCompile(`^\[task:([A-Za-z0-9_]+)\]`)
	// Non-secret headers worth logging verbatim.
	loggedHeaders = []string{"anthropic-version", "anthropic-beta", "content-type", "accept", "user-agent",
		"x-stainless-lang", "x-stainless-package-version", "x-stainless-retry-count", "x-stainless-timeout",
		"x-stainless-runtime-version", "openai-beta", "idempotency-key"}
)

func fingerprint(key string) string {
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:8]
}

// classify decides provider + endpoint from the URL (prefix routing) and headers.
func classify(r *http.Request, e *Entry) {
	p := r.URL.Path
	prov := ""
	switch {
	case p == "/anthropic" || strings.HasPrefix(p, "/anthropic/"):
		prov, p = "anthropic", strings.TrimPrefix(p, "/anthropic")
	case p == "/openai" || strings.HasPrefix(p, "/openai/"):
		prov, p = "openai", strings.TrimPrefix(p, "/openai")
	}
	p = strings.TrimSuffix(p, "/")
	if strings.HasPrefix(p, "/v1/") || p == "/v1" {
		e.PathHasV1 = true
		p = strings.TrimPrefix(p, "/v1")
	}
	switch {
	case p == "/messages":
		e.Endpoint = "messages"
	case p == "/messages/count_tokens":
		e.Endpoint = "count_tokens"
	case p == "/chat/completions":
		e.Endpoint = "chat_completions"
	case p == "/models":
		e.Endpoint = "models"
	case strings.HasPrefix(p, "/models/"):
		e.Endpoint = "model_get"
	case p == "/responses":
		e.Endpoint = "responses"
	default:
		e.Endpoint = "unknown"
	}
	if prov != "" {
		e.Provider, e.ProviderSource = prov, "prefix"
		return
	}
	switch {
	case r.Header.Get("anthropic-version") != "" || r.Header.Get("x-api-key") != "":
		e.Provider, e.ProviderSource = "anthropic", "header"
	case strings.HasPrefix(strings.ToLower(r.Header.Get("Authorization")), "bearer "):
		e.Provider, e.ProviderSource = "openai", "header"
	case e.Endpoint == "messages" || e.Endpoint == "count_tokens":
		e.Provider, e.ProviderSource = "anthropic", "endpoint"
	default:
		e.Provider, e.ProviderSource = "openai", "endpoint"
	}
}

// extractAuth records auth info. Returns the raw key (never logged).
func extractAuth(r *http.Request, e *Entry) string {
	key := ""
	if v := r.Header.Get("x-api-key"); v != "" {
		e.AuthScheme, key = "x-api-key", v
	} else if v := r.Header.Get("Authorization"); v != "" {
		if strings.HasPrefix(strings.ToLower(v), "bearer ") {
			e.AuthScheme, key = "bearer", strings.TrimSpace(v[7:])
		} else {
			e.AuthScheme, key = "other", v
		}
	} else {
		e.AuthScheme = "none"
	}
	e.AuthHeaderPresent = key != ""
	e.KeyFingerprint = fingerprint(key)
	e.Headers = map[string]string{}
	for _, h := range loggedHeaders {
		if v := r.Header.Get(h); v != "" {
			e.Headers[h] = v
		}
	}
	names := make([]string, 0, len(r.Header))
	for k := range r.Header {
		names = append(names, strings.ToLower(k))
	}
	sort.Strings(names)
	e.HeaderNames = names
	return key
}

func (e *Entry) warn(f string, a ...any)    { e.Warnings = append(e.Warnings, fmt.Sprintf(f, a...)) }
func (e *Entry) invalid(f string, a ...any) { e.ValidationErrors = append(e.ValidationErrors, fmt.Sprintf(f, a...)) }

// analyzeBody parses the JSON body and fills the analysis fields.
func analyzeBody(e *Entry, body []byte, key string) {
	e.BodyBytes = len(body)
	e.rawBody = body
	e.Images = []ImageInfo{}
	if key != "" && bytes.Contains(body, []byte(key)) {
		e.BodyContainsKey = true
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		e.BodyText = string(body)
		e.invalid("request body is not valid JSON: %v", err)
		return
	}
	if json.Valid(body) {
		e.Body = json.RawMessage(body)
	} else {
		e.BodyText = string(body)
		e.warn("request body has trailing data after the JSON value")
	}
	m, ok := v.(map[string]any)
	if !ok {
		e.invalid("request body is not a JSON object")
		return
	}
	e.parsed = m
	collectStrings(v, &e.strs)
	if key != "" && !e.BodyContainsKey {
		for _, s := range e.strs {
			if strings.Contains(s, key) {
				e.BodyContainsKey = true
				break
			}
		}
	}
	e.Model, _ = m["model"].(string)
	if n, ok := asNumber(m["max_tokens"]); ok {
		e.MaxTokens = int64(n)
	} else if n, ok := asNumber(m["max_completion_tokens"]); ok {
		e.MaxTokens = int64(n)
	}
	e.Stream, _ = m["stream"].(bool)

	switch e.Endpoint {
	case "messages", "count_tokens":
		analyzeAnthropic(e, m)
	case "chat_completions":
		analyzeOpenAI(e, m)
	}
}

func collectStrings(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, x)
	case map[string]any:
		for k, vv := range x {
			*out = append(*out, k)
			collectStrings(vv, out)
		}
	case []any:
		for _, vv := range x {
			collectStrings(vv, out)
		}
	}
}

func analyzeAnthropic(e *Entry, m map[string]any) {
	if e.Model == "" {
		e.invalid("model: Field required")
	}
	if _, ok := asNumber(m["max_tokens"]); !ok && e.Endpoint == "messages" {
		e.invalid("max_tokens: Field required")
	}
	if e.AuthScheme != "x-api-key" {
		e.warn("Anthropic request without x-api-key header (auth scheme %q)", e.AuthScheme)
	}
	if e.Headers["anthropic-version"] == "" {
		e.invalid("anthropic-version: header is required")
	}
	// system
	sysText := ""
	switch s := m["system"].(type) {
	case string:
		sysText = s
	case []any:
		for _, b := range s {
			if bm, ok := b.(map[string]any); ok {
				if t, _ := bm["text"].(string); t != "" {
					sysText += t + "\n"
				}
				if _, ok := bm["cache_control"]; ok {
					e.CacheControl = true
				}
			}
		}
	}
	if _, ok := m["cache_control"]; ok {
		e.CacheControl = true
	}
	e.SystemChars = len(sysText)
	if sysText != "" {
		sum := sha256.Sum256([]byte(sysText))
		e.SystemSHA8 = hex.EncodeToString(sum[:])[:8]
	}
	// thinking / effort / output_config
	if th, ok := m["thinking"].(map[string]any); ok {
		e.Thinking, _ = th["type"].(string)
		if e.Thinking == "disabled" && strings.Contains(e.Model, "opus-5") {
			e.invalid("thinking.type=disabled is rejected for %s (per approved plan: 400 on Opus 5.5)", e.Model)
		}
	}
	if tc, ok := m["tool_choice"].(map[string]any); ok {
		if t, _ := tc["type"].(string); (t == "tool" || t == "any") && strings.Contains(e.Model, "opus-5") {
			e.invalid("forced tool_choice %q is rejected for %s (per approved plan)", t, e.Model)
		}
	}
	if _, ok := m["temperature"]; ok {
		if _, ok2 := m["top_p"]; ok2 {
			e.warn("both temperature and top_p set (rejected by recent Claude models)")
		}
	}
	if _, ok := m["output_format"]; ok {
		e.warn("deprecated top-level output_format used; contract requires output_config.format")
	}
	if oc, ok := m["output_config"].(map[string]any); ok {
		e.Effort, _ = oc["effort"].(string)
		if e.Effort != "" && strings.Contains(e.Model, "haiku-4-5") {
			e.invalid("output_config.effort is not supported for %s (per approved plan)", e.Model)
		}
		if f, ok := oc["format"].(map[string]any); ok {
			t, _ := f["type"].(string)
			schema, hasSchema := f["schema"].(map[string]any)
			if t != "json_schema" || !hasSchema {
				e.invalid("output_config.format must be {type:json_schema, schema:{...}} (got type=%q, schema present=%v)", t, hasSchema)
			} else {
				e.StructuredOutput = "anthropic:json_schema"
				for _, iss := range strictSchemaIssues(schema) {
					e.SchemaIssues = append(e.SchemaIssues, iss)
				}
			}
		}
	}
	// messages
	msgs, ok := m["messages"].([]any)
	if !ok || len(msgs) == 0 {
		e.invalid("messages: at least one message is required")
		return
	}
	e.MessageCount = len(msgs)
	lastUser := -1
	for i, mm := range msgs {
		msg, ok := mm.(map[string]any)
		if !ok {
			e.invalid("messages[%d]: not an object", i)
			continue
		}
		role, _ := msg["role"].(string)
		if role != "user" && role != "assistant" {
			e.invalid("messages[%d].role %q must be user|assistant (system goes in top-level `system`)", i, role)
		}
		if role == "user" {
			lastUser = i
		}
		if blocks, ok := msg["content"].([]any); ok {
			for j, b := range blocks {
				bm, ok := b.(map[string]any)
				if !ok {
					continue
				}
				if _, ok := bm["cache_control"]; ok {
					e.CacheControl = true
				}
				if t, _ := bm["type"].(string); t == "image" {
					e.Images = append(e.Images, anthropicImage(fmt.Sprintf("messages[%d].content[%d]", i, j), bm, e))
				}
			}
		}
	}
	if first, ok := msgs[0].(map[string]any); ok {
		if r, _ := first["role"].(string); r != "user" {
			e.warn("first message role is %q (Anthropic expects user first)", r)
		}
	}
	e.ImageCount = len(e.Images)
	if lastUser < 0 {
		e.invalid("no user message")
		return
	}
	msg := msgs[lastUser].(map[string]any)
	analyzeTaskText(e, firstText(msg["content"]))
}

func anthropicImage(where string, b map[string]any, e *Entry) ImageInfo {
	info := ImageInfo{Where: where}
	src, _ := b["source"].(map[string]any)
	if src == nil {
		info.Error = "image block without source"
		e.invalid("%s: image block without source", where)
		return info
	}
	st, _ := src["type"].(string)
	switch st {
	case "base64":
		info.Encoding = "base64"
		info.MediaType, _ = src["media_type"].(string)
		data, _ := src["data"].(string)
		switch info.MediaType {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
		default:
			e.invalid("%s: unsupported media_type %q", where, info.MediaType)
		}
		inspectImage(&info, data, e)
	case "url":
		info.Encoding = "url"
		u, _ := src["url"].(string)
		info.Error = "url image source (not a local screenshot): " + truncate(u, 80)
	default:
		info.Error = "unknown image source type " + st
		e.invalid("%s: unknown image source type %q", where, st)
	}
	return info
}

func inspectImage(info *ImageInfo, b64 string, e *Entry) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		info.Error = "base64 decode failed: " + err.Error()
		e.invalid("%s: image data is not valid base64", info.Where)
		return
	}
	info.Bytes = len(raw)
	info.JPEGMagic = len(raw) > 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		info.Error = "image decode failed: " + err.Error()
		e.invalid("%s: image bytes could not be decoded", info.Where)
		return
	}
	info.Width, info.Height, info.Format = cfg.Width, cfg.Height, format
	if info.MediaType != "" && !strings.HasSuffix(info.MediaType, format) && !(format == "jpeg" && info.MediaType == "image/jpeg") {
		e.invalid("%s: media type %s does not match actual format %s", info.Where, info.MediaType, format)
	}
}

func analyzeOpenAI(e *Entry, m map[string]any) {
	if strings.TrimSpace(e.Model) == "" {
		e.invalid("you must provide a model parameter")
	}
	if e.AuthScheme != "bearer" {
		e.warn("OpenAI request without Authorization: Bearer (auth scheme %q)", e.AuthScheme)
	}
	if _, ok := m["max_tokens"]; ok {
		e.warn("max_tokens is deprecated for chat completions (reasoning models reject it); prefer max_completion_tokens")
	}
	if rf, ok := m["response_format"].(map[string]any); ok {
		t, _ := rf["type"].(string)
		switch t {
		case "json_schema":
			js, _ := rf["json_schema"].(map[string]any)
			if js == nil {
				e.invalid("response_format.json_schema is required when type=json_schema")
				break
			}
			e.SchemaName, _ = js["name"].(string)
			if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(e.SchemaName) {
				e.invalid("response_format.json_schema.name %q must match ^[a-zA-Z0-9_-]{1,64}$", e.SchemaName)
			}
			strict, _ := js["strict"].(bool)
			schema, ok := js["schema"].(map[string]any)
			if !ok {
				e.invalid("response_format.json_schema.schema missing")
				break
			}
			if strict {
				e.StructuredOutput = "openai:json_schema(strict)"
				issues := strictSchemaIssues(schema)
				e.SchemaIssues = append(e.SchemaIssues, issues...)
				for _, iss := range issues {
					e.invalid("Invalid schema for response_format '%s': %s", e.SchemaName, iss)
				}
			} else {
				e.StructuredOutput = "openai:json_schema(non-strict)"
				e.warn("response_format json_schema without strict:true (contract requires strict)")
			}
		case "json_object":
			e.StructuredOutput = "openai:json_object"
			e.warn("response_format json_object (contract requires json_schema strict)")
		default:
			e.warn("response_format type %q", t)
		}
	}
	msgs, ok := m["messages"].([]any)
	if !ok || len(msgs) == 0 {
		e.invalid("messages: at least one message is required")
		return
	}
	e.MessageCount = len(msgs)
	lastUser := -1
	sysText := ""
	for i, mm := range msgs {
		msg, ok := mm.(map[string]any)
		if !ok {
			e.invalid("messages[%d]: not an object", i)
			continue
		}
		role, _ := msg["role"].(string)
		switch role {
		case "system", "developer":
			sysText += firstText(msg["content"]) + "\n"
		case "user":
			lastUser = i
		case "assistant", "tool":
		default:
			e.invalid("messages[%d].role %q invalid", i, role)
		}
		if parts, ok := msg["content"].([]any); ok {
			for j, p := range parts {
				pm, ok := p.(map[string]any)
				if !ok {
					continue
				}
				if t, _ := pm["type"].(string); t == "image_url" {
					e.Images = append(e.Images, openaiImage(fmt.Sprintf("messages[%d].content[%d]", i, j), pm, e))
				}
			}
		}
	}
	e.SystemChars = len(sysText)
	if sysText != "" {
		sum := sha256.Sum256([]byte(sysText))
		e.SystemSHA8 = hex.EncodeToString(sum[:])[:8]
	}
	e.ImageCount = len(e.Images)
	if lastUser < 0 {
		e.invalid("no user message")
		return
	}
	analyzeTaskText(e, firstText(msgs[lastUser].(map[string]any)["content"]))
}

func openaiImage(where string, p map[string]any, e *Entry) ImageInfo {
	info := ImageInfo{Where: where}
	iu, _ := p["image_url"].(map[string]any)
	url, _ := iu["url"].(string)
	if !strings.HasPrefix(url, "data:") {
		info.Encoding = "url"
		info.Error = "remote image url (not a local screenshot data URL): " + truncate(url, 80)
		return info
	}
	info.Encoding = "data-url"
	comma := strings.IndexByte(url, ',')
	if comma < 0 {
		info.Error = "malformed data URL"
		e.invalid("%s: malformed data URL", where)
		return info
	}
	meta := url[5:comma] // e.g. image/jpeg;base64
	info.MediaType = strings.SplitN(meta, ";", 2)[0]
	if !strings.Contains(meta, ";base64") {
		info.Error = "data URL is not base64"
		e.invalid("%s: data URL is not base64", where)
		return info
	}
	inspectImage(&info, url[comma+1:], e)
	return info
}

// firstText returns the text of a message content: the string itself, or the
// first text block/part of an array.
func firstText(c any) string {
	switch v := c.(type) {
	case string:
		return v
	case []any:
		for _, b := range v {
			if bm, ok := b.(map[string]any); ok {
				if t, _ := bm["type"].(string); t == "text" {
					s, _ := bm["text"].(string)
					return s
				}
			}
		}
	}
	return ""
}

func analyzeTaskText(e *Entry, text string) {
	e.LastUserTextHead = truncate(text, 600)
	if mm := taskTagStartRe.FindStringSubmatch(text); mm != nil {
		e.TaskTag, e.TaskTagAtStart = mm[1], true
	} else if mm := taskTagRe.FindStringSubmatch(text); mm != nil {
		e.TaskTag = mm[1]
		e.warn("task tag found but not at the very start of the last user message text")
	} else {
		e.warn("no [task:...] tag in the last user message")
	}
	rest := text
	if idx := strings.IndexByte(text, '\n'); idx >= 0 && e.TaskTag != "" {
		rest = text[idx+1:]
	} else if e.TaskTag != "" {
		rest = taskTagRe.ReplaceAllString(text, "")
	}
	rest = strings.TrimSpace(rest)
	start := strings.IndexByte(rest, '{')
	if start < 0 {
		e.ContextError = "no JSON object after the task tag line"
		return
	}
	if start > 0 {
		e.warn("text between task tag line and JSON context object")
	}
	dec := json.NewDecoder(strings.NewReader(rest[start:]))
	dec.UseNumber()
	var ctx map[string]any
	if err := dec.Decode(&ctx); err != nil {
		e.ContextError = "context JSON parse error: " + err.Error()
		return
	}
	e.ctxObj = ctx
	if b, err := json.Marshal(ctx); err == nil {
		e.Context = b
	}
	e.Occasion, _ = ctx["occasion"].(string)
	_, e.HasLocalTime = ctx["localTime"]
	if act, ok := ctx["activity"]; ok {
		e.HasActivity = true
		b, _ := json.Marshal(act)
		e.Activity = b
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
