package brain

import (
	"encoding/json"
	"strings"
)

// PetAction is the structured reply for every pet_action / chat turn.
type PetAction struct {
	Speech              string     `json:"speech"`
	Mood                string     `json:"mood"`
	Animation           string     `json:"animation"`
	NewAnimationRequest *AnimReq   `json:"new_animation_request"`
	MemoryOps           []MemoryOp `json:"memory_ops"`
	Suggestion          string     `json:"suggestion"`
	// Activity starts a scripted prop activity ("" = none).
	Activity string `json:"activity"`
}

// Activities are scripted mini-scenes with props, played by the frontend.
var Activities = []string{"football", "basketball", "golf", "toilet"}

type AnimReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type MemoryOp struct {
	Op      string `json:"op"`
	ID      int64  `json:"id"`
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

type memoryOpsReply struct {
	MemoryOps []MemoryOp `json:"memory_ops"`
}

var Moods = []string{"neutral", "happy", "excited", "curious", "sleepy", "concerned", "sad", "love"}

func strEnum(vals ...string) map[string]any {
	return map[string]any{"type": "string", "enum": vals}
}

func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

func memoryOpSchema() map[string]any {
	return obj(map[string]any{
		"op":      strEnum("add", "update", "forget"),
		"id":      map[string]any{"type": "integer", "description": "existing memory id for update/forget, 0 for add"},
		"kind":    strEnum("habit", "preference", "fact", "goal"),
		"content": map[string]any{"type": "string"},
	}, "op", "id", "kind", "content")
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// PetActionSchema is strict-mode compatible (all properties required, no additional properties).
var PetActionSchema = mustJSON(obj(map[string]any{
	"speech":    map[string]any{"type": "string", "description": "what the pet says (max ~140 chars), '' to stay silent"},
	"mood":      strEnum(Moods...),
	"animation": map[string]any{"type": "string", "description": "name of a catalog animation to play, or ''"},
	"new_animation_request": map[string]any{
		"anyOf": []any{
			obj(map[string]any{
				"name":        map[string]any{"type": "string", "description": "snake_case name"},
				"description": map[string]any{"type": "string"},
			}, "name", "description"),
			map[string]any{"type": "null"},
		},
	},
	"memory_ops": map[string]any{"type": "array", "items": memoryOpSchema()},
	"suggestion": map[string]any{"type": "string", "description": "short practical suggestion, or ''"},
	"activity":   strEnum(append([]string{""}, Activities...)...),
}, "speech", "mood", "animation", "new_animation_request", "memory_ops", "suggestion", "activity"))

var MemoryOpsSchema = mustJSON(obj(map[string]any{
	"memory_ops": map[string]any{"type": "array", "items": memoryOpSchema()},
}, "memory_ops"))

func numOrVec() map[string]any {
	return map[string]any{"anyOf": []any{
		map[string]any{"type": "number"},
		map[string]any{"type": "array", "items": map[string]any{"type": "number"}},
	}}
}

// AnimationSpecSchema mirrors anim.Spec; ranges are enforced by anim.Validate afterwards.
var AnimationSpecSchema = mustJSON(obj(map[string]any{
	"name":        map[string]any{"type": "string"},
	"description": map[string]any{"type": "string"},
	"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	"target":      strEnum("generic", "blob", "cat", "chick"),
	"duration":    map[string]any{"type": "number"},
	"loop":        map[string]any{"type": "boolean"},
	"tracks": map[string]any{"type": "array", "items": obj(map[string]any{
		"slot":   strEnum("root", "body", "head", "eyeL", "eyeR", "mouth", "earL", "earR", "armL", "armR", "legL", "legR", "tail", "accessory"),
		"prop":   strEnum("position.x", "position.y", "position.z", "rotation.x", "rotation.y", "rotation.z", "scale", "scale.x", "scale.y", "scale.z"),
		"times":  map[string]any{"type": "array", "items": map[string]any{"type": "number"}},
		"values": map[string]any{"type": "array", "items": numOrVec()},
		"interp": strEnum("linear", "smooth", "step"),
	}, "slot", "prop", "times", "values", "interp")},
	"expressions": map[string]any{"type": "array", "items": obj(map[string]any{
		"t":     map[string]any{"type": "number"},
		"eyes":  strEnum("neutral", "happy", "sleepy", "surprised", "angry", "love", "closed", "pain"),
		"mouth": strEnum("neutral", "smile", "open", "frown", "o", "wavy"),
	}, "t", "eyes", "mouth")},
	"effects": map[string]any{"type": "array", "items": obj(map[string]any{
		"t":    map[string]any{"type": "number"},
		"type": strEnum("hearts", "sparkles", "sweat", "zzz", "question", "exclaim", "notes"),
	}, "t", "type")},
}, "name", "description", "tags", "target", "duration", "loop", "tracks", "expressions", "effects"))

// Sanitize trims and bounds model output before it reaches the UI.
func (a *PetAction) Sanitize() {
	a.Speech = clip(strings.TrimSpace(a.Speech), 280)
	a.Suggestion = clip(strings.TrimSpace(a.Suggestion), 200)
	ok := false
	for _, m := range Moods {
		if a.Mood == m {
			ok = true
		}
	}
	if !ok {
		a.Mood = "neutral"
	}
	a.Animation = strings.TrimSpace(a.Animation)
	if len(a.MemoryOps) > 5 {
		a.MemoryOps = a.MemoryOps[:5]
	}
	if a.MemoryOps == nil {
		a.MemoryOps = []MemoryOp{}
	}
	if a.NewAnimationRequest != nil && strings.TrimSpace(a.NewAnimationRequest.Name) == "" {
		a.NewAnimationRequest = nil
	}
	known := false
	for _, v := range Activities {
		if a.Activity == v {
			known = true
		}
	}
	if !known {
		a.Activity = ""
	}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
