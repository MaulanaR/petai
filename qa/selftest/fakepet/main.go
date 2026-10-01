// fakepet is a HARNESS SELF-TEST double, not the product. It implements only the
// black-box surface of qa/CONTRACT.md that has no window: env vars, data dir layout,
// the debug HTTP API and the AI wire contract (task tags, context, activity, images,
// animation cache, memory_ops). It lets the QA scripts be dry-run without the real app
// and without injecting input on the user's desktop.
//
// PETAI_FAKE_BUGS=comma,list injects known defects so we can prove the harness FAILS:
//
//	activity_leak  send activity even when watchActivity=false
//	no_redact      do not redact emails / long numbers in titles
//	no_blocklist   ignore the blocklist
//	regen          always regenerate animations (ignore the cache)
//	save_invalid   save AI animation specs without validation
//	key_in_log     write the API key into logs/petai.log
//	no_memory      ignore memory_ops
//	image_always   attach a screenshot to every request
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pGetForegroundWindow     = user32.NewProc("GetForegroundWindow")
	pGetWindowTextW          = user32.NewProc("GetWindowTextW")
	pGetWindowThreadProcId   = user32.NewProc("GetWindowThreadProcessId")
	pGetSystemMetrics        = user32.NewProc("GetSystemMetrics")
	pSystemParametersInfoW   = user32.NewProc("SystemParametersInfoW")
	pOpenProcess             = kernel32.NewProc("OpenProcess")
	pQueryFullProcessImageNW = kernel32.NewProc("QueryFullProcessImageNameW")
	pCloseHandle             = kernel32.NewProc("CloseHandle")
	pCreateMutexW            = kernel32.NewProc("CreateMutexW")
)

type app struct {
	mu       sync.Mutex
	dataDir  string
	cfg      map[string]any
	bugs     map[string]bool
	fast     bool
	keys     map[string]string
	baseURL  map[string]string
	pet      map[string]any
	memories []map[string]any
	nextMem  int64
	anims    map[string]map[string]any // user animations by name
	calls    []time.Time
	logf     *os.File
	monW     float64
	monH     float64
	workBot  float64
	vx, vy   float64
}

var builtins = []string{"idle", "walk", "float", "sleep", "jump", "wave", "happy_bounce", "surprised", "dangle", "fall", "land", "sit", "look_around"}

func defaultConfig() map[string]any {
	var m map[string]any
	_ = json.Unmarshal([]byte(`{
  "version": 1,
  "pet": {"character": "blob", "name": "Mochi", "color": "", "personality": "ceria", "language": "id", "scale": 1.0},
  "movement": {"mode": "ground", "speed": 1.0, "activity": 0.5, "anchorX": -1, "anchorY": -1},
  "ai": {"enabled": true, "provider": "anthropic", "anthropic": {"model": "claude-opus-5-5", "baseURL": ""}, "openai": {"model": "", "baseURL": ""}, "maxCallsPerHour": 12},
  "privacy": {"watchActivity": false, "screenshots": false, "screenshotIntervalMin": 10,
    "blocklist": ["1password","bitwarden","keepass","inprivate","incognito","bank","bca","mandiri","bri","bni"],
    "retentionDays": 14, "excludeFromCapture": true},
  "general": {"autostart": false, "fps": 30, "monitor": 0, "respectFullscreen": true, "debug": false}
}`), &m)
	return m
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

func get(m map[string]any, path string) any {
	var cur any = m
	for _, p := range strings.Split(path, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}
func getS(m map[string]any, p string) string { s, _ := get(m, p).(string); return s }
func getB(m map[string]any, p string) bool   { b, _ := get(m, p).(bool); return b }
func getF(m map[string]any, p string) float64 {
	switch v := get(m, p).(type) {
	case float64:
		return v
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return 0
}

func main() {
	data := os.Getenv("PETAI_DATA_DIR")
	if data == "" {
		log.Fatal("fakepet: PETAI_DATA_DIR is required (self-test double)")
	}
	addr := os.Getenv("PETAI_DEBUG_ADDR")
	if addr == "" {
		log.Fatal("fakepet: PETAI_DEBUG_ADDR is required")
	}
	name, _ := syscall.UTF16PtrFromString("Local\\PetAIFakeSingleInstance")
	h, _, err := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h != 0 && errors.Is(err, syscall.ERROR_ALREADY_EXISTS) {
		fmt.Println("fakepet: another instance is running, exiting")
		os.Exit(0)
	}
	a := &app{dataDir: data, bugs: map[string]bool{}, anims: map[string]map[string]any{}, nextMem: 1,
		keys:    map[string]string{"anthropic": os.Getenv("PETAI_API_KEY_ANTHROPIC"), "openai": os.Getenv("PETAI_API_KEY_OPENAI")},
		baseURL: map[string]string{"anthropic": os.Getenv("PETAI_ANTHROPIC_BASE_URL"), "openai": os.Getenv("PETAI_OPENAI_BASE_URL")},
		fast:    os.Getenv("PETAI_FAST") == "1"}
	for _, b := range strings.Split(os.Getenv("PETAI_FAKE_BUGS"), ",") {
		if b = strings.TrimSpace(b); b != "" {
			a.bugs[b] = true
		}
	}
	_ = os.MkdirAll(filepath.Join(data, "logs"), 0o755)
	_ = os.MkdirAll(filepath.Join(data, "animations"), 0o755)
	a.logf, _ = os.OpenFile(filepath.Join(data, "logs", "petai.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	a.load()
	w, _, _ := pGetSystemMetrics.Call(0)
	hh, _, _ := pGetSystemMetrics.Call(1)
	a.monW, a.monH = float64(w), float64(hh)
	var rc [4]int32
	pSystemParametersInfoW.Call(0x0030, 0, uintptr(unsafe.Pointer(&rc[0])), 0)
	a.workBot = float64(rc[3])
	a.pet = map[string]any{"x": a.monW/2 - 80, "y": a.workBot - 160, "w": 160.0, "h": 160.0, "character": getS(a.cfg, "pet.character"),
		"mode": getS(a.cfg, "movement.mode"), "state": "idle", "animation": "idle", "visible": true}
	a.logln("fakepet started; bugs=%v", a.bugs)
	if a.bugs["key_in_log"] {
		a.logln("using key %s", a.keys["anthropic"])
	}
	go a.simulate()
	go a.autoTriggers()

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/state", a.hState)
	mux.HandleFunc("/debug/config", a.hConfig)
	mux.HandleFunc("/debug/trigger", a.hTrigger)
	mux.HandleFunc("/debug/chat", a.hChat)
	mux.HandleFunc("/debug/animations", a.hAnimations)
	mux.HandleFunc("/debug/play", a.hPlay)
	mux.HandleFunc("/debug/memories", a.hMemories)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(http.Serve(ln, mux))
}

func (a *app) logln(f string, args ...any) {
	if a.logf != nil {
		fmt.Fprintf(a.logf, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(f, args...))
	}
}

// ------------------------------------------------------------------ persistence

func (a *app) load() {
	a.cfg = defaultConfig()
	if raw, err := os.ReadFile(filepath.Join(a.dataDir, "config.json")); err == nil {
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil {
			deepMerge(a.cfg, m)
		} else {
			a.logln("config.json invalid, using defaults")
		}
	}
	a.saveConfig()
	if raw, err := os.ReadFile(filepath.Join(a.dataDir, "petai.db")); err == nil {
		var db struct {
			Memories []map[string]any `json:"memories"`
			Next     int64            `json:"next"`
		}
		if json.Unmarshal(raw, &db) == nil {
			a.memories, a.nextMem = db.Memories, db.Next
		}
	}
	files, _ := filepath.Glob(filepath.Join(a.dataDir, "animations", "*", "*.json"))
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		var s map[string]any
		if json.Unmarshal(raw, &s) == nil {
			if n, _ := s["name"].(string); n != "" {
				a.anims[n] = s
			}
		}
	}
}

func (a *app) saveConfig() {
	b, _ := json.MarshalIndent(a.cfg, "", "  ")
	_ = os.WriteFile(filepath.Join(a.dataDir, "config.json"), b, 0o644)
}

func (a *app) saveDB() {
	b, _ := json.Marshal(map[string]any{"memories": a.memories, "next": a.nextMem})
	_ = os.WriteFile(filepath.Join(a.dataDir, "petai.db"), b, 0o644)
}

func (a *app) saveAnim(s map[string]any) {
	name, _ := s["name"].(string)
	target, _ := s["target"].(string)
	if target == "" {
		target = "generic"
	}
	dir := filepath.Join(a.dataDir, "animations", target)
	_ = os.MkdirAll(dir, 0o755)
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, name+".json"), b, 0o644)
	a.anims[name] = s
	idx := []map[string]any{}
	for n, sp := range a.anims {
		idx = append(idx, map[string]any{"name": n, "target": sp["target"], "description": sp["description"]})
	}
	ib, _ := json.MarshalIndent(idx, "", "  ")
	_ = os.WriteFile(filepath.Join(a.dataDir, "animations", "index.json"), ib, 0o644)
}

// ------------------------------------------------------------------ simulation

func (a *app) simulate() {
	t := time.NewTicker(100 * time.Millisecond)
	for range t.C {
		a.mu.Lock()
		mode := getS(a.cfg, "movement.mode")
		a.pet["mode"] = mode
		a.pet["character"] = getS(a.cfg, "pet.character")
		x, y := a.pet["x"].(float64), a.pet["y"].(float64)
		switch mode {
		case "free":
			if rand.Float64() < 0.05 {
				a.vx, a.vy = (rand.Float64()-0.5)*16, (rand.Float64()-0.5)*16
			}
			x, y = x+a.vx, y+a.vy
		case "ground":
			if rand.Float64() < 0.05 || a.vx == 0 {
				a.vx = (rand.Float64() - 0.5) * 14
			}
			x, y = x+a.vx, a.workBot-160
		}
		x = math.Max(0, math.Min(a.monW-160, x))
		y = math.Max(0, math.Min(a.monH-160, y))
		a.pet["x"], a.pet["y"] = x, y
		a.mu.Unlock()
	}
}

func (a *app) autoTriggers() {
	every := 20 * time.Minute
	if a.fast {
		every = 8 * time.Second
	}
	for range time.NewTicker(every).C {
		a.mu.Lock()
		enabled := getB(a.cfg, "ai.enabled")
		limit := int(getF(a.cfg, "ai.maxCallsPerHour"))
		n := a.recentCalls()
		a.mu.Unlock()
		if enabled && n < limit {
			_, _ = a.runOccasion("random_chatter", "", true)
		}
	}
}

func (a *app) recentCalls() int {
	window := time.Hour
	if a.fast {
		window = time.Minute
	}
	n := 0
	for _, t := range a.calls {
		if time.Since(t) < window {
			n++
		}
	}
	return n
}

// ------------------------------------------------------------------ watcher

func foreground() (app, title string) {
	h, _, _ := pGetForegroundWindow.Call()
	if h == 0 {
		return "", ""
	}
	buf := make([]uint16, 512)
	pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), 512)
	title = syscall.UTF16ToString(buf)
	var pid uint32
	pGetWindowThreadProcId.Call(h, uintptr(unsafe.Pointer(&pid)))
	ph, _, _ := pOpenProcess.Call(0x1000, 0, uintptr(pid))
	if ph != 0 {
		nb := make([]uint16, 1024)
		size := uint32(1024)
		pQueryFullProcessImageNW.Call(ph, 0, uintptr(unsafe.Pointer(&nb[0])), uintptr(unsafe.Pointer(&size)))
		pCloseHandle.Call(ph)
		app = filepath.Base(syscall.UTF16ToString(nb[:size]))
	}
	return app, title
}

var (
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	numRe   = regexp.MustCompile(`\d{6,}`)
)

func (a *app) activity() (map[string]any, bool) {
	appName, title := foreground()
	if !a.bugs["no_blocklist"] {
		low := strings.ToLower(appName + " " + title)
		bl, _ := get(a.cfg, "privacy.blocklist").([]any)
		for _, b := range bl {
			if s, _ := b.(string); s != "" && strings.Contains(low, strings.ToLower(s)) {
				return nil, false
			}
		}
	}
	if !a.bugs["no_redact"] {
		title = numRe.ReplaceAllString(emailRe.ReplaceAllString(title, "[email]"), "[num]")
	}
	return map[string]any{"app": appName, "title": title}, true
}

func fakeJPEG() string {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for x := 0; x < 1280; x += 4 {
		img.Set(x, (x/4)%720, color.RGBA{200, 50, 50, 255})
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70})
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// ------------------------------------------------------------------ AI client (contract wire format)

var petActionSchema = map[string]any{
	"type": "object", "additionalProperties": false,
	"required": []any{"speech", "mood", "animation", "new_animation_request", "memory_ops", "suggestion"},
	"properties": map[string]any{
		"speech": map[string]any{"type": "string"}, "mood": map[string]any{"type": "string"},
		"animation": map[string]any{"type": "string"}, "suggestion": map[string]any{"type": "string"},
		"new_animation_request": map[string]any{"anyOf": []any{map[string]any{"type": "null"},
			map[string]any{"type": "object", "additionalProperties": false, "required": []any{"name", "description"},
				"properties": map[string]any{"name": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}}}}},
		"memory_ops": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false,
			"required": []any{"op", "id", "kind", "content"},
			"properties": map[string]any{"op": map[string]any{"type": "string"}, "id": map[string]any{"type": "integer"},
				"kind": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}}},
	},
}

var looseSchema = map[string]any{"type": "object", "additionalProperties": false, "required": []any{"data"},
	"properties": map[string]any{"data": map[string]any{"type": "string"}}}

func (a *app) callAI(task string, ctxObj map[string]any, withImage bool) (map[string]any, error) {
	prov := getS(a.cfg, "ai.provider")
	key := a.keys[prov]
	base := a.baseURL[prov]
	if base == "" {
		base = getS(a.cfg, "ai."+prov+".baseURL")
	}
	ctxJSON, _ := json.Marshal(ctxObj)
	text := "[task:" + task + "]\n" + string(ctxJSON)
	img := ""
	if withImage || a.bugs["image_always"] {
		img = fakeJPEG()
	}
	schema := petActionSchema
	if task != "pet_action" {
		schema = looseSchema
	}
	var body map[string]any
	var url string
	hdr := map[string]string{}
	if prov == "anthropic" {
		content := []any{}
		if img != "" {
			content = append(content, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/jpeg", "data": img}})
		}
		content = append(content, map[string]any{"type": "text", "text": text})
		body = map[string]any{"model": getS(a.cfg, "ai.anthropic.model"), "max_tokens": 1024,
			"system":        []any{map[string]any{"type": "text", "text": "Kamu adalah pet desktop lucu.", "cache_control": map[string]any{"type": "ephemeral"}}},
			"messages":      []any{map[string]any{"role": "user", "content": content}},
			"output_config": map[string]any{"format": map[string]any{"type": "json_schema", "schema": schema}}}
		url = strings.TrimSuffix(base, "/") + "/v1/messages"
		hdr["x-api-key"], hdr["anthropic-version"] = key, "2023-06-01"
	} else {
		parts := []any{map[string]any{"type": "text", "text": text}}
		if img != "" {
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/jpeg;base64," + img}})
		}
		model := getS(a.cfg, "ai.openai.model")
		if model == "" {
			model = "gpt-qa-mini"
		}
		body = map[string]any{"model": model,
			"messages":        []any{map[string]any{"role": "system", "content": "Kamu adalah pet desktop lucu."}, map[string]any{"role": "user", "content": parts}},
			"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": task, "strict": true, "schema": schema}}}
		url = strings.TrimSuffix(base, "/") + "/v1/chat/completions"
		hdr["Authorization"] = "Bearer " + key
	}
	b, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ai http %d", resp.StatusCode)
	}
	var out string
	if prov == "anthropic" {
		var m struct {
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Type, Text string
			} `json:"content"`
		}
		if err := json.Unmarshal(rb, &m); err != nil {
			return nil, err
		}
		if m.StopReason == "refusal" {
			return nil, errors.New("refusal")
		}
		for _, c := range m.Content {
			if c.Type == "text" {
				out = c.Text
			}
		}
	} else {
		var m struct {
			Choices []struct {
				Message struct {
					Content *string `json:"content"`
					Refusal *string `json:"refusal"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(rb, &m); err != nil || len(m.Choices) == 0 {
			return nil, errors.New("bad openai response")
		}
		if m.Choices[0].Message.Refusal != nil {
			return nil, errors.New("refusal")
		}
		if m.Choices[0].Message.Content != nil {
			out = *m.Choices[0].Message.Content
		}
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("model output is not JSON: %v", err)
	}
	return res, nil
}

// ------------------------------------------------------------------ behaviour

func (a *app) runOccasion(occasion, chatText string, automatic bool) (map[string]any, error) {
	a.mu.Lock()
	if !getB(a.cfg, "ai.enabled") {
		a.mu.Unlock()
		return nil, errors.New("ai disabled")
	}
	ctx := map[string]any{"occasion": occasion, "localTime": time.Now().Format(time.RFC3339)}
	if chatText != "" {
		ctx["userMessage"] = chatText
	}
	watch := getB(a.cfg, "privacy.watchActivity")
	act, allowed := a.activity()
	if (watch || a.bugs["activity_leak"]) && allowed {
		ctx["activity"] = act
	}
	mems := []string{}
	for _, m := range a.memories {
		mems = append(mems, fmt.Sprint(m["content"]))
	}
	if len(mems) > 0 {
		ctx["memories"] = mems
	}
	withImage := occasion == "screenshot_insight" && getB(a.cfg, "privacy.screenshots") && watch && allowed
	if automatic {
		a.calls = append(a.calls, time.Now())
	}
	a.mu.Unlock()

	task := "pet_action"
	if occasion == "consolidate" {
		task = "memory_ops"
	}
	res, err := a.callAI(task, ctx, withImage)
	if err != nil {
		a.logln("ai error: %v", err)
		return nil, err
	}
	a.applyMemoryOps(res["memory_ops"])
	if task == "memory_ops" {
		return nil, nil
	}
	if anim, _ := res["animation"].(string); anim != "" {
		a.play(anim)
	}
	if nar, ok := res["new_animation_request"].(map[string]any); ok {
		name, _ := nar["name"].(string)
		a.mu.Lock()
		_, cached := a.anims[name]
		a.mu.Unlock()
		isBuiltin := false
		for _, b := range builtins {
			if b == name {
				isBuiltin = true
			}
		}
		if (cached || isBuiltin) && !a.bugs["regen"] {
			a.play(name)
		} else {
			spec, err := a.callAI("animation_spec", map[string]any{"name": name, "description": nar["description"], "slots": "root,body,head,..."}, false)
			if err == nil {
				if a.bugs["save_invalid"] || validSpec(spec) {
					clampSpec(spec)
					a.mu.Lock()
					a.saveAnim(spec)
					a.mu.Unlock()
					a.play(name)
				} else {
					a.play("surprised")
				}
			}
		}
	}
	return res, nil
}

func (a *app) applyMemoryOps(v any) {
	if a.bugs["no_memory"] {
		return
	}
	ops, _ := v.([]any)
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, o := range ops {
		op, _ := o.(map[string]any)
		switch op["op"] {
		case "add":
			a.memories = append(a.memories, map[string]any{"id": a.nextMem, "kind": op["kind"], "content": op["content"], "source": "ai", "confidence": 0.7})
			a.nextMem++
		case "forget":
			id := int64(getF(op, "id"))
			keep := []map[string]any{}
			for _, m := range a.memories {
				if memID(m) != id {
					keep = append(keep, m)
				}
			}
			a.memories = keep
		}
	}
	a.saveDB()
}

func memID(m map[string]any) int64 {
	switch v := m["id"].(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return -1
}

func (a *app) play(name string) {
	a.mu.Lock()
	a.pet["animation"] = name
	a.mu.Unlock()
	go func() {
		time.Sleep(1500 * time.Millisecond)
		a.mu.Lock()
		if a.pet["animation"] == name {
			a.pet["animation"] = "idle"
		}
		a.mu.Unlock()
	}()
}

var snake = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var okSlots = map[string]bool{"root": true, "body": true, "head": true, "eyeL": true, "eyeR": true, "mouth": true, "earL": true, "earR": true, "armL": true, "armR": true, "legL": true, "legR": true, "tail": true, "accessory": true}
var okProps = map[string]bool{"position.x": true, "position.y": true, "position.z": true, "rotation.x": true, "rotation.y": true, "rotation.z": true, "scale": true, "scale.x": true, "scale.y": true, "scale.z": true}

func validSpec(s map[string]any) bool {
	name, _ := s["name"].(string)
	d, ok := s["duration"].(float64)
	tracks, ok2 := s["tracks"].([]any)
	if !snake.MatchString(name) || !ok || d <= 0 || d > 10 || !ok2 || len(tracks) > 24 {
		return false
	}
	for _, t := range tracks {
		tr, _ := t.(map[string]any)
		if !okSlots[fmt.Sprint(tr["slot"])] || !okProps[fmt.Sprint(tr["prop"])] {
			return false
		}
		times, _ := tr["times"].([]any)
		vals, _ := tr["values"].([]any)
		if len(times) == 0 || len(times) != len(vals) || len(times) > 64 {
			return false
		}
		prev := -1.0
		for _, x := range times {
			f, ok := x.(float64)
			if !ok || f <= prev || f > d {
				return false
			}
			prev = f
		}
		for _, v := range vals {
			if tr["prop"] == "scale" {
				arr, ok := v.([]any)
				if !ok || len(arr) != 3 {
					return false
				}
			} else if _, ok := v.(float64); !ok {
				return false
			}
		}
	}
	return true
}

func clampSpec(s map[string]any) {
	tracks, _ := s["tracks"].([]any)
	for _, t := range tracks {
		tr, _ := t.(map[string]any)
		prop := fmt.Sprint(tr["prop"])
		vals, _ := tr["values"].([]any)
		cl := func(f float64) float64 {
			switch {
			case strings.HasPrefix(prop, "rotation"):
				return math.Max(-6.2832, math.Min(6.2832, f))
			case strings.HasPrefix(prop, "position"):
				return math.Max(-2, math.Min(2, f))
			default:
				return math.Max(0.3, math.Min(2, f))
			}
		}
		for i, v := range vals {
			switch x := v.(type) {
			case float64:
				vals[i] = cl(x)
			case []any:
				for j, c := range x {
					if f, ok := c.(float64); ok {
						x[j] = cl(f)
					}
				}
			}
		}
	}
}

// ------------------------------------------------------------------ debug API

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *app) hState(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	pet := map[string]any{}
	for k, v := range a.pet {
		pet[k] = v
	}
	prov := getS(a.cfg, "ai.provider")
	writeJSON(w, map[string]any{
		"window": map[string]any{"hwnd": 0, "exStyle": 0, "clickThrough": "exstyle", "interactive": false,
			"rect": map[string]any{"x": 0, "y": 0, "w": a.monW, "h": a.monH}, "scale": 1.0},
		"pet":    pet,
		"ai":     map[string]any{"provider": prov, "model": getS(a.cfg, "ai."+prov+".model"), "hasKey": a.keys[prov] != "", "callsLastHour": a.recentCalls()},
		"config": a.cfg,
	})
}

func (a *app) hConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	var m map[string]any
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	a.mu.Lock()
	deepMerge(a.cfg, m)
	a.saveConfig()
	if mv, ok := m["movement"].(map[string]any); ok {
		if _, ok := mv["mode"]; ok && getS(a.cfg, "movement.mode") == "ground" {
			a.pet["y"] = a.workBot - 160
		}
	}
	cfg := a.cfg
	a.mu.Unlock()
	writeJSON(w, cfg)
}

func (a *app) hTrigger(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Occasion string `json:"occasion"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	res, err := a.runOccasion(req.Occasion, "", false)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "action": nil, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "action": res, "error": ""})
}

func (a *app) hChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	res, err := a.runOccasion("chat", req.Text, false)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "action": nil, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "action": res})
}

func (a *app) hAnimations(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []any{}
	for _, b := range builtins {
		out = append(out, map[string]any{"name": b, "target": "generic", "description": "built-in", "tags": []any{}, "builtin": true})
	}
	names := []string{}
	for n := range a.anims {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s := a.anims[n]
		out = append(out, map[string]any{"name": n, "target": s["target"], "description": s["description"], "tags": s["tags"], "builtin": false})
	}
	writeJSON(w, out)
}

func (a *app) hPlay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	a.play(req.Name)
	writeJSON(w, map[string]any{"ok": true})
}

func (a *app) hMemories(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.memories == nil {
		writeJSON(w, []any{})
		return
	}
	writeJSON(w, a.memories)
}
