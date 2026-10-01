package main

import (
	"fmt"
	"sort"
)

// strictSchemaIssues returns the problems that OpenAI's Structured Outputs
// (`strict: true`) rejects with HTTP 400:
//   - the root schema must be an object (not anyOf),
//   - every object must set additionalProperties:false,
//   - every key of `properties` must be listed in `required`.
// The same walker is used (as warnings only) for Anthropic output_config.format.
func strictSchemaIssues(schema any) []string {
	issues := []string{}
	root, ok := schema.(map[string]any)
	if !ok {
		return append(issues, "schema is not an object")
	}
	if !typeIncludes(root["type"], "object") {
		issues = append(issues, fmt.Sprintf("root schema type must be \"object\" (got %v)", root["type"]))
	}
	if _, has := root["anyOf"]; has {
		issues = append(issues, "root schema must not be anyOf")
	}
	walkSchema("#", root, &issues, 0)
	return issues
}

func typeIncludes(t any, want string) bool {
	switch v := t.(type) {
	case string:
		return v == want
	case []any:
		for _, x := range v {
			if s, _ := x.(string); s == want {
				return true
			}
		}
	}
	return false
}

func walkSchema(path string, s map[string]any, issues *[]string, depth int) {
	if depth > 40 {
		*issues = append(*issues, path+": schema nesting too deep")
		return
	}
	_, hasProps := s["properties"]
	if typeIncludes(s["type"], "object") || hasProps {
		if ap, ok := s["additionalProperties"].(bool); !ok || ap {
			*issues = append(*issues, path+": object must set additionalProperties:false")
		}
		props, _ := s["properties"].(map[string]any)
		req := map[string]bool{}
		if rr, ok := s["required"].([]any); ok {
			for _, x := range rr {
				if k, ok := x.(string); ok {
					req[k] = true
				}
			}
		}
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !req[k] {
				*issues = append(*issues, fmt.Sprintf("%s: property %q not listed in required", path, k))
			}
			if sub, ok := props[k].(map[string]any); ok {
				walkSchema(path+"/properties/"+k, sub, issues, depth+1)
			}
		}
	}
	if items, ok := s["items"].(map[string]any); ok {
		walkSchema(path+"/items", items, issues, depth+1)
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if arr, ok := s[key].([]any); ok {
			for i, x := range arr {
				if sub, ok := x.(map[string]any); ok {
					walkSchema(fmt.Sprintf("%s/%s/%d", path, key, i), sub, issues, depth+1)
				}
			}
		}
	}
	for _, key := range []string{"$defs", "definitions"} {
		if defs, ok := s[key].(map[string]any); ok {
			names := make([]string, 0, len(defs))
			for k := range defs {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				if sub, ok := defs[k].(map[string]any); ok {
					walkSchema(path+"/"+key+"/"+k, sub, issues, depth+1)
				}
			}
		}
	}
}
