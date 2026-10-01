// Package anim defines the pet animation DSL, validates AI-generated specs and stores
// them in a local library so the same move never has to be generated twice.
package anim

import (
	"encoding/json"
	"fmt"
)

// Spec is a declarative, data-only animation. It is never executed as code.
type Spec struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Tags        []string     `json:"tags"`
	Target      string       `json:"target"`
	Duration    float64      `json:"duration"`
	Loop        bool         `json:"loop"`
	Tracks      []Track      `json:"tracks"`
	Expressions []Expression `json:"expressions"`
	Effects     []Effect     `json:"effects"`
	Builtin     bool         `json:"builtin,omitempty"`
}

type Track struct {
	Slot   string    `json:"slot"`
	Prop   string    `json:"prop"`
	Times  []float64 `json:"times"`
	Values []Value   `json:"values"`
	Interp string    `json:"interp"`
}

type Expression struct {
	T     float64 `json:"t"`
	Eyes  string  `json:"eyes"`
	Mouth string  `json:"mouth"`
}

type Effect struct {
	T    float64 `json:"t"`
	Type string  `json:"type"`
}

// Value is either a scalar or a 3-vector (for the "scale" prop).
type Value struct {
	N   float64
	Vec []float64
}

func (v Value) IsVec() bool { return v.Vec != nil }

func (v Value) MarshalJSON() ([]byte, error) {
	if v.Vec != nil {
		return json.Marshal(v.Vec)
	}
	return json.Marshal(v.N)
}

func (v *Value) UnmarshalJSON(b []byte) error {
	var n float64
	if err := json.Unmarshal(b, &n); err == nil {
		v.N, v.Vec = n, nil
		return nil
	}
	var vec []float64
	if err := json.Unmarshal(b, &vec); err != nil {
		return fmt.Errorf("value must be number or [x,y,z]: %s", string(b))
	}
	if vec == nil {
		vec = []float64{}
	}
	v.Vec = vec
	return nil
}

// Meta is the catalog entry shown to the AI and the settings UI.
type Meta struct {
	Name        string   `json:"name"`
	Target      string   `json:"target"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Builtin     bool     `json:"builtin"`
}

func (s Spec) Meta() Meta {
	tags := s.Tags
	if tags == nil {
		tags = []string{}
	}
	return Meta{Name: s.Name, Target: s.Target, Description: s.Description, Tags: tags, Builtin: s.Builtin}
}

var (
	Slots = []string{"root", "body", "head", "eyeL", "eyeR", "mouth", "earL", "earR",
		"armL", "armR", "legL", "legR", "tail", "accessory"}
	Props = []string{"position.x", "position.y", "position.z", "rotation.x", "rotation.y", "rotation.z",
		"scale", "scale.x", "scale.y", "scale.z"}
	Interps     = []string{"linear", "smooth", "step"}
	Eyes        = []string{"neutral", "happy", "sleepy", "surprised", "angry", "love", "closed"}
	Mouths      = []string{"neutral", "smile", "open", "frown", "o"}
	EffectTypes = []string{"hearts", "sparkles", "sweat", "zzz", "question", "exclaim"}
	Targets     = []string{"generic", "blob", "cat", "chick"}
)

// CharacterSlots lists which slots each character actually has (others are ignored at play time).
var CharacterSlots = map[string][]string{
	"blob":  {"root", "body", "head", "eyeL", "eyeR", "mouth", "earL", "earR", "armL", "armR", "legL", "legR", "accessory"},
	"cat":   {"root", "body", "head", "eyeL", "eyeR", "mouth", "earL", "earR", "armL", "armR", "legL", "legR", "tail", "accessory"},
	"chick": {"root", "body", "head", "eyeL", "eyeR", "mouth", "armL", "armR", "legL", "legR", "tail", "accessory"},
}

// GenericSlots are present on every character.
var GenericSlots = []string{"root", "body", "head", "eyeL", "eyeR", "mouth", "armL", "armR", "legL", "legR", "accessory"}
