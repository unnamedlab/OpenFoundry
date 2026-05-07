package llm

import (
	"encoding/json"
	"strings"
)

// InterpolateTemplate is the prompt-template renderer. Returns
// (rendered, missing) — `missing` lists the variable names that
// weren't found in `variables`. When `strict` is false, missing
// `{{key}}` placeholders are kept verbatim in the output.
//
// Mirrors Rust src/domain/llm/provider.rs::interpolate_template.
func InterpolateTemplate(template string, variables json.RawMessage, strict bool) (string, []string) {
	var values map[string]json.RawMessage
	_ = json.Unmarshal(variables, &values) // empty/non-object → values stays nil
	missing := []string{}
	var rendered strings.Builder
	rendered.Grow(len(template))

	remaining := template
	for {
		open := strings.Index(remaining, "{{")
		if open == -1 {
			break
		}
		rendered.WriteString(remaining[:open])
		afterOpen := remaining[open+2:]
		close := strings.Index(afterOpen, "}}")
		if close == -1 {
			rendered.WriteString(remaining[open:])
			remaining = ""
			break
		}
		key := strings.TrimSpace(afterOpen[:close])
		if v, ok := values[key]; ok {
			rendered.WriteString(valueToString(v))
		} else {
			missing = append(missing, key)
			if !strict {
				rendered.WriteString("{{")
				rendered.WriteString(key)
				rendered.WriteString("}}")
			}
		}
		remaining = afterOpen[close+2:]
	}
	rendered.WriteString(remaining)
	return rendered.String(), missing
}

// valueToString matches Rust value_to_string: strings unwrap to inner
// content, null becomes empty, everything else uses default JSON encoding.
func valueToString(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}
	if len(s) > 0 && s[0] == '"' {
		var inner string
		if json.Unmarshal(raw, &inner) == nil {
			return inner
		}
	}
	return s
}
