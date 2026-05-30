package promptcompat

import (
	"encoding/json"
	"strings"
)

// StringifyToolCallArguments normalizes tool-call arguments (still accepted on
// the request side for API compatibility) into a compact string, preserving
// raw concatenated payloads when they already look like model output rather
// than a single JSON object.
//
// Tool calling has been removed from the response path, so this helper only
// affects how inbound tool history is parsed; it never injects DSML.
func StringifyToolCallArguments(v any) string {
	switch x := v.(type) {
	case nil:
		return "{}"
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return "{}"
		}
		s = normalizeToolArgumentString(s)
		if s == "" {
			return "{}"
		}
		return s
	default:
		b, err := json.Marshal(x)
		if err != nil || len(b) == 0 {
			return "{}"
		}
		return string(b)
	}
}

func normalizeToolArgumentString(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if looksLikeConcatenatedJSON(trimmed) {
		// Keep the original payload to avoid silently rewriting model output.
		return raw
	}
	return trimmed
}

func looksLikeConcatenatedJSON(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "}{") || strings.Contains(trimmed, "][") {
		return true
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	var first any
	if err := dec.Decode(&first); err != nil {
		return false
	}
	var second any
	return dec.Decode(&second) == nil
}
