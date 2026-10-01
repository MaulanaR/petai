package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Scenario state, set with -scenario / MOCKAI_SCENARIO / POST /mock/scenario.
type Scenario struct {
	Name          string `json:"scenario"`
	Count         int    `json:"count"`         // >0: apply to the next N AI requests, then switch to Then
	Then          string `json:"then"`          // scenario after Count is used up (default "normal")
	AnimName      string `json:"animName"`      // name used by new_anim / clamp_anim (default qa_spin / qa_clamp)
	AnimTarget    string `json:"animTarget"`    // target used for generated specs (default generic)
	MemoryContent string `json:"memoryContent"` // content used by memory scenario
	ForgetID      int64  `json:"forgetId"`      // id used by memory_forget
	DelayMs       int    `json:"delayMs"`       // slow: delay before answering (default 8000)
	ThinkingBlock bool   `json:"thinkingBlock"` // Anthropic: prepend a thinking block before the text block
	NoRetry       bool   `json:"noRetry"`       // send x-should-retry:false on errors
	Speech        string `json:"speech"`        // override speech text for pet_action
}

type scenarioDef struct {
	desc string
}

var scenarioDefs = map[string]scenarioDef{
	"normal":                {"valid PetAction (animation 'wave'), valid AnimationSpec, empty memory_ops"},
	"new_anim":              {"PetAction asks new_animation_request {name: animName|qa_spin}; animation_spec returns a VALID spec of that name"},
	"new_anim_builtin":      {"PetAction asks new_animation_request {name:'wave'} (a built-in) -> app must NOT call animation_spec"},
	"invalid_anim":          {"new_animation_request qa_bad_combo; spec breaks several rules (unknown slot 'wing', non-increasing times, duration 50)"},
	"invalid_anim_slot":     {"new_animation_request qa_bad_slot; spec has slot '__proto__' only"},
	"invalid_anim_prop":     {"new_animation_request qa_bad_prop; spec has prop 'constructor' only"},
	"invalid_anim_times":    {"new_animation_request qa_bad_times; times [0,0.8,0.4] only"},
	"invalid_anim_duration": {"new_animation_request qa_bad_duration; duration 50 only"},
	"invalid_anim_len":      {"new_animation_request qa_bad_len; times/values length mismatch only"},
	"invalid_anim_scale":    {"new_animation_request qa_bad_scale; 'scale' values are numbers instead of [x,y,z]"},
	"invalid_anim_tracks":   {"new_animation_request qa_bad_tracks; 25 tracks (> 24)"},
	"invalid_anim_keys":     {"new_animation_request qa_bad_keys; 65 keys in one track (> 64)"},
	"invalid_anim_code":     {"new_animation_request qa_bad_code; values are JS code strings ('alert(1)')"},
	"invalid_anim_name":     {"new_animation_request qa_bad_name; spec name is a path traversal '../../qa_evil'"},
	"clamp_anim":            {"new_animation_request animName|qa_clamp; structurally valid spec with out-of-range values (app must clamp)"},
	"memory":                {"PetAction memory_ops add habit (memoryContent); memory_ops task adds 'QA-CONSOLIDATED ...'"},
	"memory_forget":         {"memory_ops forget id=forgetId (both pet_action and memory_ops tasks)"},
	"refusal":               {"Anthropic stop_reason 'refusal' (empty content); OpenAI message.refusal set, content null"},
	"malformed":             {"200 OK but the model text is not JSON"},
	"wrong_shape":           {"200 OK, valid JSON but not the requested schema"},
	"http400":               {"400 invalid_request_error"},
	"http401":               {"401 authentication error (bad key)"},
	"http429":               {"429 rate limit (retry-after: 1)"},
	"http500":               {"500 server error"},
	"http529":               {"529 overloaded (Anthropic) / 503 (OpenAI)"},
	"drop":                  {"connection closed without a response"},
	"slow":                  {"normal answer after delayMs (default 8000 ms)"},
	"xss":                   {"PetAction speech/suggestion containing HTML/script; must be displayed as text, never executed"},
	"long_speech":           {"PetAction with ~1500 character speech (bubble overflow check)"},
}

func scenarioNames() []string {
	names := make([]string, 0, len(scenarioDefs))
	for k := range scenarioDefs {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func badAnimName(scenario string) string {
	switch scenario {
	case "invalid_anim":
		return "qa_bad_combo"
	case "invalid_anim_name":
		return "qa_bad_name"
	}
	return "qa_bad_" + strings.TrimPrefix(scenario, "invalid_anim_")
}

// requestedAnimName returns the animation name a scenario makes the pet request.
func (sc Scenario) requestedAnimName() string {
	switch {
	case sc.Name == "new_anim":
		if sc.AnimName != "" {
			return sc.AnimName
		}
		return "qa_spin"
	case sc.Name == "clamp_anim":
		if sc.AnimName != "" {
			return sc.AnimName
		}
		return "qa_clamp"
	case sc.Name == "new_anim_builtin":
		return "wave"
	case strings.HasPrefix(sc.Name, "invalid_anim"):
		return badAnimName(sc.Name)
	}
	return ""
}

func (sc Scenario) target() string {
	if sc.AnimTarget != "" {
		return sc.AnimTarget
	}
	return "generic"
}

func petAction(speech, mood, anim string) map[string]any {
	return map[string]any{
		"speech":                speech,
		"mood":                  mood,
		"animation":             anim,
		"new_animation_request": nil,
		"memory_ops":            []any{},
		"suggestion":            "",
	}
}

// ValidSpec returns a valid AnimationSpec (the qa_spin shape) with the given name/target.
func ValidSpec(name, target string) map[string]any {
	return map[string]any{
		"name":        name,
		"description": "QA mock animation: spin once while squashing",
		"tags":        []any{"qa", "happy"},
		"target":      target,
		"duration":    1.2,
		"loop":        false,
		"tracks": []any{
			map[string]any{"slot": "root", "prop": "rotation.y", "times": []any{0.0, 0.6, 1.2}, "values": []any{0.0, 3.1416, 6.2832}, "interp": "smooth"},
			map[string]any{"slot": "body", "prop": "scale", "times": []any{0.0, 0.3, 0.6},
				"values": []any{[]any{1.0, 1.0, 1.0}, []any{1.2, 0.8, 1.2}, []any{1.0, 1.0, 1.0}}, "interp": "linear"},
			map[string]any{"slot": "root", "prop": "position.y", "times": []any{0.0, 0.6, 1.2}, "values": []any{0.0, 0.4, 0.0}, "interp": "smooth"},
		},
		"expressions": []any{map[string]any{"t": 0.0, "eyes": "happy", "mouth": "open"}},
		"effects":     []any{map[string]any{"t": 0.6, "type": "sparkles"}},
	}
}

func track(slot, prop string, times, values []any) map[string]any {
	return map[string]any{"slot": slot, "prop": prop, "times": times, "values": values, "interp": "linear"}
}

// InvalidSpec returns the spec for an invalid_anim* scenario (each breaks one rule,
// except invalid_anim which breaks several).
func InvalidSpec(scenario string) map[string]any {
	name := badAnimName(scenario)
	s := ValidSpec(name, "generic")
	tracks := s["tracks"].([]any)
	t0 := tracks[0].(map[string]any)
	switch scenario {
	case "invalid_anim":
		s["duration"] = 50.0
		t0["slot"] = "wing"
		t0["times"] = []any{0.0, 0.8, 0.4}
	case "invalid_anim_slot":
		t0["slot"] = "__proto__"
	case "invalid_anim_prop":
		t0["prop"] = "constructor"
	case "invalid_anim_times":
		t0["times"] = []any{0.0, 0.8, 0.4}
	case "invalid_anim_duration":
		s["duration"] = 50.0
	case "invalid_anim_len":
		t0["values"] = []any{0.0, 3.14}
	case "invalid_anim_scale":
		tracks[1].(map[string]any)["values"] = []any{1.0, 1.2, 1.0}
	case "invalid_anim_tracks":
		many := []any{}
		for i := 0; i < 25; i++ {
			many = append(many, track("root", "rotation.z", []any{0.0, 1.0}, []any{0.0, 0.1}))
		}
		s["tracks"] = many
		s["duration"] = 1.2
	case "invalid_anim_keys":
		times, values := []any{}, []any{}
		for i := 0; i < 65; i++ {
			times = append(times, float64(i)*0.015)
			values = append(values, 0.01*float64(i%10))
		}
		tracks[0] = track("root", "rotation.y", times, values)
	case "invalid_anim_code":
		t0["values"] = []any{"0", "alert(document.cookie)", "(()=>{window.__qa_pwned=1;return 6.28})()"}
	case "invalid_anim_name":
		s["name"] = "../../qa_evil"
	}
	return s
}

// ClampSpec returns a structurally valid spec whose values exceed the clamp ranges.
func ClampSpec(name, target string) map[string]any {
	s := ValidSpec(name, target)
	s["tracks"] = []any{
		track("root", "rotation.y", []any{0.0, 0.6, 1.2}, []any{0.0, 50.0, 100.0}),
		track("root", "position.y", []any{0.0, 0.6}, []any{0.0, 30.0}),
		track("body", "scale", []any{0.0, 0.6}, []any{[]any{5.0, 5.0, 5.0}, []any{0.01, 0.01, 0.01}}),
	}
	return s
}

// requestedNameFromContext finds the animation name the app asked the model to create.
func requestedNameFromContext(ctx map[string]any) string {
	if ctx == nil {
		return ""
	}
	if s, ok := ctx["name"].(string); ok && s != "" {
		return s
	}
	for _, k := range []string{"request", "new_animation_request", "animation", "newAnimationRequest", "spec"} {
		if m, ok := ctx[k].(map[string]any); ok {
			if s, ok := m["name"].(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// buildPayload returns the JSON object the "model" answers with, for a task.
func buildPayload(sc Scenario, task string, e *Entry) (any, string) {
	occ := e.Occasion
	speech := sc.Speech
	if speech == "" {
		speech = fmt.Sprintf("Halo! Aku pet QA (mock %s/%s, occasion=%s).", e.Provider, sc.Name, occ)
	}
	switch task {
	case "animation_spec":
		switch {
		case strings.HasPrefix(sc.Name, "invalid_anim"):
			s := InvalidSpec(sc.Name)
			return s, fmt.Sprint(s["name"])
		case sc.Name == "clamp_anim":
			n := sc.requestedAnimName()
			return ClampSpec(n, sc.target()), n
		}
		name := ""
		if sc.Name == "new_anim" {
			name = sc.requestedAnimName()
		}
		if name == "" {
			name = requestedNameFromContext(e.ctxObj)
		}
		if name == "" || !snakeCaseRe.MatchString(name) {
			name = "qa_generic_anim"
		}
		return ValidSpec(name, sc.target()), name
	case "memory_ops":
		switch sc.Name {
		case "memory":
			c := sc.MemoryContent
			if c == "" {
				c = "QA-HABIT: user suka ngoding sampai larut malam"
			}
			return map[string]any{"memory_ops": []any{map[string]any{"op": "add", "id": 0, "kind": "habit", "content": "QA-CONSOLIDATED " + c}}}, ""
		case "memory_forget":
			return map[string]any{"memory_ops": []any{map[string]any{"op": "forget", "id": sc.ForgetID, "kind": "habit", "content": ""}}}, ""
		}
		return map[string]any{"memory_ops": []any{}}, ""
	}
	// pet_action (also the fallback for a missing/unknown tag)
	pa := petAction(speech, "happy", "wave")
	if occ == "long_focus" {
		pa["suggestion"] = "Sudah lama fokus, istirahat 5 menit yuk (mock suggestion)."
		pa["mood"] = "concerned"
	}
	if n := sc.requestedAnimName(); n != "" {
		pa["animation"] = ""
		pa["new_animation_request"] = map[string]any{"name": n, "description": "QA mock: " + n}
		return pa, n
	}
	switch sc.Name {
	case "memory":
		c := sc.MemoryContent
		if c == "" {
			c = "QA-HABIT: user suka ngoding sampai larut malam"
		}
		pa["memory_ops"] = []any{map[string]any{"op": "add", "id": 0, "kind": "habit", "content": c}}
	case "memory_forget":
		pa["memory_ops"] = []any{map[string]any{"op": "forget", "id": sc.ForgetID, "kind": "habit", "content": ""}}
	case "xss":
		pa["speech"] = `<img src=x onerror="window.__qa_xss=1"><script>window.__qa_xss=2</script><b>QA-XSS bold?</b>`
		pa["suggestion"] = `<a href="javascript:alert(1)">QA-XSS link</a>`
	case "long_speech":
		pa["speech"] = strings.Repeat("Ini kalimat panjang dari mock QA untuk menguji bubble. ", 28)
	}
	return pa, ""
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
