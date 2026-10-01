package anim

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

const (
	MaxDuration = 10.0
	MaxTracks   = 24
	MaxKeys     = 64
	maxRot      = 6.2832
	maxPos      = 2.0
	minScale    = 0.3
	maxScale    = 2.0
)

var nonSlug = regexp.MustCompile(`[^a-z0-9_]+`)

// Slug normalizes an animation name to snake_case [a-z0-9_], max 40 chars.
func Slug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	s = nonSlug.ReplaceAllString(s, "")
	s = strings.Trim(s, "_")
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

func in(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Validate checks structure, rejects unknown slots/props and clamps numeric ranges in place.
func Validate(s *Spec) error {
	s.Name = Slug(s.Name)
	if s.Name == "" {
		return errors.New("name required")
	}
	if s.Target == "" {
		s.Target = "generic"
	}
	if !in(Targets, s.Target) {
		return fmt.Errorf("unknown target %q", s.Target)
	}
	if len(s.Description) > 300 {
		s.Description = s.Description[:300]
	}
	if len(s.Tags) > 8 {
		s.Tags = s.Tags[:8]
	}
	if !(s.Duration > 0) || s.Duration > MaxDuration || math.IsNaN(s.Duration) {
		return fmt.Errorf("duration must be in (0,%v]", MaxDuration)
	}
	if len(s.Tracks) == 0 {
		return errors.New("at least one track required")
	}
	if len(s.Tracks) > MaxTracks {
		return fmt.Errorf("too many tracks (%d > %d)", len(s.Tracks), MaxTracks)
	}
	allowed := Slots
	if s.Target != "generic" {
		allowed = CharacterSlots[s.Target]
	} else {
		allowed = GenericSlots
		// generic animations may still touch optional slots; they are ignored where absent
		allowed = append(append([]string{}, allowed...), "earL", "earR", "tail")
	}
	for i := range s.Tracks {
		t := &s.Tracks[i]
		if !in(Slots, t.Slot) || !in(allowed, t.Slot) {
			return fmt.Errorf("track %d: unknown slot %q", i, t.Slot)
		}
		if !in(Props, t.Prop) {
			return fmt.Errorf("track %d: unknown prop %q", i, t.Prop)
		}
		if t.Interp == "" {
			t.Interp = "smooth"
		}
		if !in(Interps, t.Interp) {
			return fmt.Errorf("track %d: unknown interp %q", i, t.Interp)
		}
		n := len(t.Times)
		if n == 0 || n > MaxKeys {
			return fmt.Errorf("track %d: need 1..%d keys", i, MaxKeys)
		}
		if len(t.Values) != n {
			return fmt.Errorf("track %d: times/values length mismatch", i)
		}
		for k, tm := range t.Times {
			if math.IsNaN(tm) || tm < 0 || tm > s.Duration+1e-6 {
				return fmt.Errorf("track %d: time %v outside [0,duration]", i, tm)
			}
			if k > 0 && tm <= t.Times[k-1] {
				return fmt.Errorf("track %d: times must be strictly increasing", i)
			}
		}
		vec := t.Prop == "scale"
		for k := range t.Values {
			v := &t.Values[k]
			if vec {
				if !v.IsVec() || len(v.Vec) != 3 {
					return fmt.Errorf("track %d: scale values must be [x,y,z]", i)
				}
				for j := range v.Vec {
					v.Vec[j] = clampF(v.Vec[j], minScale, maxScale)
				}
				continue
			}
			if v.IsVec() {
				return fmt.Errorf("track %d: %s values must be numbers", i, t.Prop)
			}
			switch {
			case strings.HasPrefix(t.Prop, "rotation"):
				v.N = clampF(v.N, -maxRot, maxRot)
			case strings.HasPrefix(t.Prop, "position"):
				v.N = clampF(v.N, -maxPos, maxPos)
			default:
				v.N = clampF(v.N, minScale, maxScale)
			}
		}
	}
	if len(s.Expressions) > MaxKeys {
		return errors.New("too many expressions")
	}
	for i, e := range s.Expressions {
		if e.T < 0 || e.T > s.Duration+1e-6 {
			return fmt.Errorf("expression %d: time out of range", i)
		}
		if e.Eyes != "" && !in(Eyes, e.Eyes) {
			return fmt.Errorf("expression %d: unknown eyes %q", i, e.Eyes)
		}
		if e.Mouth != "" && !in(Mouths, e.Mouth) {
			return fmt.Errorf("expression %d: unknown mouth %q", i, e.Mouth)
		}
	}
	if len(s.Effects) > 16 {
		return errors.New("too many effects")
	}
	for i, e := range s.Effects {
		if e.T < 0 || e.T > s.Duration+1e-6 {
			return fmt.Errorf("effect %d: time out of range", i)
		}
		if !in(EffectTypes, e.Type) {
			return fmt.Errorf("effect %d: unknown type %q", i, e.Type)
		}
	}
	if s.Tags == nil {
		s.Tags = []string{}
	}
	if s.Expressions == nil {
		s.Expressions = []Expression{}
	}
	if s.Effects == nil {
		s.Effects = []Effect{}
	}
	return nil
}

func clampF(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return lo
	}
	return math.Max(lo, math.Min(hi, v))
}
