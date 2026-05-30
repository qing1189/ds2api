package prompt

// asString returns v as a string when it already is one, otherwise "".
// (Previously lived in the now-removed tool_calls.go.)
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
