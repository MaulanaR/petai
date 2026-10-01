package main

// Independent re-implementation of the AnimationSpec validation rules from
// qa/CONTRACT.md ("AnimationSpec (DSL)"). Used to:
//   - prove the mock's "valid" fixtures are valid and each "invalid_*" fixture
//     breaks exactly the rule it claims to break (see mock_test.go),
//   - validate animation files the app saved / its built-ins (POST /mock/validate,
//     `mockai -validate file...`).

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
)

var (
	dslSlots = setOf("root", "body", "head", "eyeL", "eyeR", "mouth", "earL", "earR",
		"armL", "armR", "legL", "legR", "tail", "accessory")
	dslProps = setOf("position.x", "position.y", "position.z", "rotation.x", "rotation.y",
		"rotation.z", "scale", "scale.x", "scale.y", "scale.z")
	dslInterps  = setOf("linear", "smooth", "step")
	dslTargets  = setOf("generic", "blob", "cat", "chick")
	dslEyes     = setOf("neutral", "happy", "sleepy", "surprised", "angry", "love", "closed")
	dslMouths   = setOf("neutral", "smile", "open", "frown", "o")
	dslEffects  = setOf("hearts", "sparkles", "sweat", "zzz", "question", "exclaim")
	dslTopKeys  = setOf("name", "description", "tags", "target", "duration", "loop", "tracks", "expressions", "effects")
	snakeCaseRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

const (
	dslMaxDuration = 10.0
	dslMaxTracks   = 24
	dslMaxKeys     = 64
	dslRotLimit    = 6.2832
	dslPosLimit    = 2.0
	dslScaleMin    = 0.3
	dslScaleMax    = 2.0
	dslEps         = 1e-6
)

// SpecReport is the result of validating one AnimationSpec.
type SpecReport struct {
	Name     string   `json:"name"`
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
	// Values outside the clamp ranges. A spec the app SAVED must have none
	// (the app is supposed to clamp before saving).
	ClampViolations []string `json:"clampViolations"`
}

func setOf(v ...string) map[string]bool {
	m := make(map[string]bool, len(v))
	for _, s := range v {
		m[s] = true
	}
	return m
}

func asNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// ValidateSpecJSON parses raw JSON and validates it.
func ValidateSpecJSON(raw []byte) SpecReport {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return SpecReport{Errors: []string{"not valid JSON: " + err.Error()}, Warnings: []string{}, ClampViolations: []string{}}
	}
	return ValidateSpec(v)
}

// ValidateSpec validates a decoded AnimationSpec against the contract rules.
func ValidateSpec(v any) SpecReport {
	r := SpecReport{Errors: []string{}, Warnings: []string{}, ClampViolations: []string{}}
	errf := func(f string, a ...any) { r.Errors = append(r.Errors, fmt.Sprintf(f, a...)) }
	warnf := func(f string, a ...any) { r.Warnings = append(r.Warnings, fmt.Sprintf(f, a...)) }
	clampf := func(f string, a ...any) { r.ClampViolations = append(r.ClampViolations, fmt.Sprintf(f, a...)) }

	obj, ok := v.(map[string]any)
	if !ok {
		errf("spec is not a JSON object")
		return r
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !dslTopKeys[k] {
			warnf("unknown top-level key %q", k)
		}
	}

	name, _ := obj["name"].(string)
	r.Name = name
	if name == "" {
		errf("name: missing or not a string")
	} else if !snakeCaseRe.MatchString(name) {
		errf("name %q is not snake_case [a-z][a-z0-9_]{0,63} (also unsafe as a file name)", name)
	}
	if d, present := obj["description"]; present {
		if _, ok := d.(string); !ok {
			errf("description: not a string")
		}
	}
	if t, present := obj["tags"]; present {
		arr, ok := t.([]any)
		if !ok {
			errf("tags: not an array")
		} else {
			for i, x := range arr {
				if _, ok := x.(string); !ok {
					errf("tags[%d]: not a string", i)
				}
			}
		}
	}
	target, _ := obj["target"].(string)
	if !dslTargets[target] {
		errf("target %q not in generic|blob|cat|chick", target)
	}
	if l, present := obj["loop"]; present {
		if _, ok := l.(bool); !ok {
			errf("loop: not a boolean")
		}
	}

	duration, ok := asNumber(obj["duration"])
	durOK := ok
	if !ok {
		errf("duration: missing or not a number")
	} else if !(duration > 0 && duration <= dslMaxDuration+dslEps) {
		errf("duration %v outside (0, %v]", duration, dslMaxDuration)
		durOK = false
	}

	tracksRaw, present := obj["tracks"]
	tracks, ok := tracksRaw.([]any)
	if !present || !ok {
		errf("tracks: missing or not an array")
	} else {
		if len(tracks) == 0 {
			warnf("tracks: empty")
		}
		if len(tracks) > dslMaxTracks {
			errf("tracks: %d > %d", len(tracks), dslMaxTracks)
		}
		for i, tr := range tracks {
			validateTrack(i, tr, duration, durOK, errf, warnf, clampf)
		}
	}

	if e, present := obj["expressions"]; present {
		arr, ok := e.([]any)
		if !ok {
			errf("expressions: not an array")
		} else {
			for i, x := range arr {
				m, ok := x.(map[string]any)
				if !ok {
					errf("expressions[%d]: not an object", i)
					continue
				}
				checkTime(fmt.Sprintf("expressions[%d].t", i), m["t"], duration, durOK, errf)
				if s, present := m["eyes"]; present {
					if str, _ := s.(string); !dslEyes[str] {
						errf("expressions[%d].eyes %v not allowed", i, s)
					}
				}
				if s, present := m["mouth"]; present {
					if str, _ := s.(string); !dslMouths[str] {
						errf("expressions[%d].mouth %v not allowed", i, s)
					}
				}
			}
		}
	}
	if e, present := obj["effects"]; present {
		arr, ok := e.([]any)
		if !ok {
			errf("effects: not an array")
		} else {
			for i, x := range arr {
				m, ok := x.(map[string]any)
				if !ok {
					errf("effects[%d]: not an object", i)
					continue
				}
				checkTime(fmt.Sprintf("effects[%d].t", i), m["t"], duration, durOK, errf)
				if str, _ := m["type"].(string); !dslEffects[str] {
					errf("effects[%d].type %v not allowed", i, m["type"])
				}
			}
		}
	}
	r.Valid = len(r.Errors) == 0
	return r
}

func checkTime(label string, v any, duration float64, durOK bool, errf func(string, ...any)) {
	t, ok := asNumber(v)
	if !ok {
		errf("%s: missing or not a number", label)
		return
	}
	if t < -dslEps || (durOK && t > duration+dslEps) {
		errf("%s=%v outside [0,duration]", label, t)
	}
}

func validateTrack(i int, tr any, duration float64, durOK bool, errf, warnf, clampf func(string, ...any)) {
	m, ok := tr.(map[string]any)
	if !ok {
		errf("tracks[%d]: not an object", i)
		return
	}
	slot, _ := m["slot"].(string)
	if !dslSlots[slot] {
		errf("tracks[%d].slot %v unknown", i, m["slot"])
	}
	prop, _ := m["prop"].(string)
	if !dslProps[prop] {
		errf("tracks[%d].prop %v unknown", i, m["prop"])
	}
	if ip, present := m["interp"]; present {
		if s, _ := ip.(string); !dslInterps[s] {
			errf("tracks[%d].interp %v unknown", i, ip)
		}
	} else {
		warnf("tracks[%d].interp missing", i)
	}
	times, ok1 := m["times"].([]any)
	values, ok2 := m["values"].([]any)
	if !ok1 {
		errf("tracks[%d].times: missing or not an array", i)
	}
	if !ok2 {
		errf("tracks[%d].values: missing or not an array", i)
	}
	if !ok1 || !ok2 {
		return
	}
	if len(times) == 0 {
		errf("tracks[%d].times: empty", i)
	}
	if len(times) > dslMaxKeys {
		errf("tracks[%d]: %d keys > %d", i, len(times), dslMaxKeys)
	}
	if len(times) != len(values) {
		errf("tracks[%d]: times(%d) and values(%d) length differ", i, len(times), len(values))
	}
	prev := math.Inf(-1)
	for k, tv := range times {
		t, ok := asNumber(tv)
		if !ok {
			errf("tracks[%d].times[%d]: not a number", i, k)
			continue
		}
		if t <= prev {
			errf("tracks[%d].times not strictly increasing at index %d (%v after %v)", i, k, t, prev)
		}
		if t < -dslEps || (durOK && t > duration+dslEps) {
			errf("tracks[%d].times[%d]=%v outside [0,duration]", i, k, t)
		}
		prev = t
	}
	isScaleVec := prop == "scale"
	for k, vv := range values {
		if isScaleVec {
			arr, ok := vv.([]any)
			if !ok || len(arr) != 3 {
				errf("tracks[%d].values[%d]: prop scale needs [x,y,z] array", i, k)
				continue
			}
			for c, x := range arr {
				n, ok := asNumber(x)
				if !ok {
					errf("tracks[%d].values[%d][%d]: not a number", i, k, c)
					continue
				}
				checkClamp(prop, n, fmt.Sprintf("tracks[%d].values[%d][%d]", i, k, c), clampf)
			}
			continue
		}
		n, ok := asNumber(vv)
		if !ok {
			errf("tracks[%d].values[%d]: %v is not a number", i, k, vv)
			continue
		}
		if math.IsNaN(n) || math.IsInf(n, 0) {
			errf("tracks[%d].values[%d]: not finite", i, k)
			continue
		}
		checkClamp(prop, n, fmt.Sprintf("tracks[%d].values[%d]", i, k), clampf)
	}
}

func checkClamp(prop string, n float64, label string, clampf func(string, ...any)) {
	switch {
	case len(prop) >= 8 && prop[:8] == "rotation":
		if math.Abs(n) > dslRotLimit+1e-4 {
			clampf("%s=%v exceeds rotation limit ±%v", label, n, dslRotLimit)
		}
	case len(prop) >= 8 && prop[:8] == "position":
		if math.Abs(n) > dslPosLimit+dslEps {
			clampf("%s=%v exceeds position limit ±%v", label, n, dslPosLimit)
		}
	case len(prop) >= 5 && prop[:5] == "scale":
		if n < dslScaleMin-dslEps || n > dslScaleMax+dslEps {
			clampf("%s=%v outside scale range [%v,%v]", label, n, dslScaleMin, dslScaleMax)
		}
	}
}
