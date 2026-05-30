package claude

import (
	"strings"
	"testing"
)

// ─── normalizeClaudeMessages ─────────────────────────────────────────

func TestNormalizeClaudeMessagesSimpleString(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "user", "content": "Hello"},
	}
	got := normalizeClaudeMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	m := got[0].(map[string]any)
	if m["content"] != "Hello" {
		t.Fatalf("expected 'Hello', got %v", m["content"])
	}
}

func TestNormalizeClaudeMessagesArrayContent(t *testing.T) {
	msgs := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "line1"},
				map[string]any{"type": "text", "text": "line2"},
			},
		},
	}
	got := normalizeClaudeMessages(msgs)
	m := got[0].(map[string]any)
	if m["content"] != "line1\nline2" {
		t.Fatalf("expected joined text, got %q", m["content"])
	}
}

func TestNormalizeClaudeMessagesToolResult(t *testing.T) {
	msgs := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "tool_result", "content": "tool output"},
			},
		},
	}
	got := normalizeClaudeMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected one normalized message, got %d", len(got))
	}
	m := got[0].(map[string]any)
	if m["role"] != "tool" {
		t.Fatalf("expected tool role preserved, got %#v", m["role"])
	}
	content, _ := m["content"].(string)
	if content != "tool output" {
		t.Fatalf("expected raw tool output content preserved, got %q", content)
	}
}

func TestNormalizeClaudeMessagesToolUseToAssistantPlainText(t *testing.T) {
	msgs := []any{
		map[string]any{
			"role": "assistant",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call_1",
					"name":  "search_web",
					"input": map[string]any{"query": "latest"},
				},
			},
		},
	}

	got := normalizeClaudeMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected one normalized tool-call message, got %d", len(got))
	}
	m := got[0].(map[string]any)
	if m["role"] != "assistant" {
		t.Fatalf("expected assistant role, got %#v", m["role"])
	}
	// Tool calling removed: no structured tool_calls field, no DSML markup.
	if _, ok := m["tool_calls"]; ok {
		t.Fatalf("expected no tool_calls field after tool-call removal, got %#v", m["tool_calls"])
	}
	content, _ := m["content"].(string)
	if containsStr(content, "DSML") {
		t.Fatalf("expected no DSML markup in assistant content, got %q", content)
	}
	if !containsStr(content, "search_web") || !containsStr(content, "latest") {
		t.Fatalf("expected tool call rendered as plain history text, got %q", content)
	}
}

func TestNormalizeClaudeMessagesPreservesThinkingOnToolUseHistory(t *testing.T) {
	msgs := []any{
		map[string]any{
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "thinking", "thinking": "need live search before answering"},
				map[string]any{
					"type":  "tool_use",
					"id":    "call_1",
					"name":  "search_web",
					"input": map[string]any{"query": "latest"},
				},
			},
		},
	}

	got := normalizeClaudeMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected one normalized tool-call message, got %#v", got)
	}
	m := got[0].(map[string]any)
	if m["reasoning_content"] != "need live search before answering" {
		t.Fatalf("expected thinking preserved as reasoning_content, got %#v", m)
	}
	prompt := buildClaudePromptTokenText(got, true)
	if !containsStr(prompt, "[reasoning_content]\nneed live search before answering\n[/reasoning_content]") {
		t.Fatalf("expected thinking in prompt history, got %q", prompt)
	}
	if containsStr(prompt, "DSML") {
		t.Fatalf("expected no DSML markup in prompt history, got %q", prompt)
	}
	if !containsStr(prompt, "search_web") {
		t.Fatalf("expected tool name in prompt history, got %q", prompt)
	}
}

func TestNormalizeClaudeMessagesDoesNotPromoteUserToolUse(t *testing.T) {
	msgs := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call_unsafe",
					"name":  "dangerous_tool",
					"input": map[string]any{"value": "x"},
				},
			},
		},
	}

	got := normalizeClaudeMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected one normalized message, got %d", len(got))
	}
	m := got[0].(map[string]any)
	if m["role"] != "user" {
		t.Fatalf("expected user role preserved, got %#v", m["role"])
	}
	if _, ok := m["tool_calls"]; ok {
		t.Fatalf("expected no tool_calls promotion for user message, got %#v", m["tool_calls"])
	}
	content, _ := m["content"].(string)
	if !containsStr(content, `"type":"tool_use"`) || !containsStr(content, "dangerous_tool") {
		t.Fatalf("expected raw tool_use block preserved in user content, got %q", content)
	}
}

func TestNormalizeClaudeMessagesSkipsNonMap(t *testing.T) {
	msgs := []any{"not a map", 42}
	got := normalizeClaudeMessages(msgs)
	if len(got) != 0 {
		t.Fatalf("expected 0 messages for non-map items, got %d", len(got))
	}
}

func TestNormalizeClaudeMessagesEmpty(t *testing.T) {
	got := normalizeClaudeMessages(nil)
	if len(got) != 0 {
		t.Fatalf("expected 0, got %d", len(got))
	}
}

func TestNormalizeClaudeMessagesPreservesRole(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "assistant", "content": "response"},
	}
	got := normalizeClaudeMessages(msgs)
	m := got[0].(map[string]any)
	if m["role"] != "assistant" {
		t.Fatalf("expected 'assistant', got %q", m["role"])
	}
}

func TestNormalizeClaudeMessagesMixedContentBlocks(t *testing.T) {
	msgs := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "Hello"},
				map[string]any{"type": "image", "source": map[string]any{"type": "base64", "data": strings.Repeat("A", 2048)}},
				map[string]any{"type": "text", "text": "World"},
			},
		},
	}
	got := normalizeClaudeMessages(msgs)
	m := got[0].(map[string]any)
	content, _ := m["content"].(string)
	if !containsStr(content, "Hello") || !containsStr(content, "World") || !containsStr(content, `"type":"image"`) {
		t.Fatalf("expected text plus non-text block marker preserved, got %q", content)
	}
	if !containsStr(content, omittedBinaryMarker) {
		t.Fatalf("expected binary payload omitted marker, got %q", content)
	}
	if containsStr(content, strings.Repeat("A", 100)) {
		t.Fatalf("expected raw base64 payload not to be included, got %q", content)
	}
}

func TestNormalizeClaudeMessagesToolResultNonTextPayloadStringified(t *testing.T) {
	msgs := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call_image_1",
					"name":        "vision_tool",
					"content": []any{
						map[string]any{"type": "text", "text": "image analysis"},
						map[string]any{
							"type":   "image",
							"source": map[string]any{"type": "base64", "media_type": "image/png", "data": strings.Repeat("B", 2048)},
						},
					},
				},
			},
		},
	}

	got := normalizeClaudeMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected one normalized message, got %d", len(got))
	}
	m := got[0].(map[string]any)
	if m["role"] != "tool" {
		t.Fatalf("expected tool role, got %#v", m["role"])
	}
	content, _ := m["content"].(string)
	if !containsStr(content, `"type":"tool_result"`) || !containsStr(content, `"type":"image"`) {
		t.Fatalf("expected non-text tool_result payload to be JSON stringified, got %q", content)
	}
	if !containsStr(content, omittedBinaryMarker) {
		t.Fatalf("expected binary data to be sanitized with omitted marker, got %q", content)
	}
	if containsStr(content, strings.Repeat("B", 100)) {
		t.Fatalf("expected raw base64 payload not to be included, got %q", content)
	}
}

func TestNormalizeClaudeMessagesBackfillsToolResultCallIDByName(t *testing.T) {
	msgs := []any{
		map[string]any{
			"role": "assistant",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"name":  "search_web",
					"input": map[string]any{"query": "latest"},
				},
			},
		},
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":    "tool_result",
					"name":    "search_web",
					"content": "ok",
				},
			},
		},
	}

	got := normalizeClaudeMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %#v", got)
	}
	assistant, _ := got[0].(map[string]any)
	if _, ok := assistant["tool_calls"]; ok {
		t.Fatalf("expected no tool_calls field after tool-call removal, got %#v", assistant["tool_calls"])
	}
	toolMsg, _ := got[1].(map[string]any)
	toolCallID, _ := toolMsg["tool_call_id"].(string)
	if !strings.HasPrefix(toolCallID, "call_claude_") {
		t.Fatalf("expected tool_result to reuse generated call id, got %#v", toolMsg)
	}
}

// ─── hasSystemMessage ────────────────────────────────────────────────

func TestHasSystemMessageTrue(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "system", "content": "You are a helper"},
		map[string]any{"role": "user", "content": "Hi"},
	}
	if !hasSystemMessage(msgs) {
		t.Fatal("expected true")
	}
}

func TestHasSystemMessageFalse(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "user", "content": "Hi"},
		map[string]any{"role": "assistant", "content": "Hello"},
	}
	if hasSystemMessage(msgs) {
		t.Fatal("expected false")
	}
}

func TestHasSystemMessageEmpty(t *testing.T) {
	if hasSystemMessage(nil) {
		t.Fatal("expected false for nil")
	}
}

func TestHasSystemMessageNonMap(t *testing.T) {
	msgs := []any{"not a map"}
	if hasSystemMessage(msgs) {
		t.Fatal("expected false for non-map")
	}
}

// ─── toMessageMaps ───────────────────────────────────────────────────

func TestToMessageMapsNormal(t *testing.T) {
	input := []any{
		map[string]any{"role": "user", "content": "Hello"},
	}
	got := toMessageMaps(input)
	if len(got) != 1 {
		t.Fatalf("expected 1, got %d", len(got))
	}
}

func TestToMessageMapsNonSlice(t *testing.T) {
	got := toMessageMaps("not a slice")
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestToMessageMapsSkipsNonMap(t *testing.T) {
	input := []any{"string", map[string]any{"role": "user"}, 42}
	got := toMessageMaps(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 map, got %d", len(got))
	}
}

func TestToMessageMapsNil(t *testing.T) {
	got := toMessageMaps(nil)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

// ─── extractMessageContent ──────────────────────────────────────────

func TestExtractMessageContentString(t *testing.T) {
	if got := extractMessageContent("hello"); got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestExtractMessageContentArray(t *testing.T) {
	input := []any{"part1", "part2"}
	got := extractMessageContent(input)
	if got != "part1\npart2" {
		t.Fatalf("expected joined, got %q", got)
	}
}

func TestExtractMessageContentOther(t *testing.T) {
	got := extractMessageContent(42)
	if got != "42" {
		t.Fatalf("expected '42', got %q", got)
	}
}

func TestExtractMessageContentNil(t *testing.T) {
	got := extractMessageContent(nil)
	if got != "<nil>" {
		t.Fatalf("expected '<nil>', got %q", got)
	}
}

// ─── cloneMap ────────────────────────────────────────────────────────

func TestCloneMapBasic(t *testing.T) {
	original := map[string]any{"a": 1, "b": "hello"}
	clone := cloneMap(original)
	original["a"] = 999
	if clone["a"] != 1 {
		t.Fatalf("expected 1, got %v", clone["a"])
	}
	if clone["b"] != "hello" {
		t.Fatalf("expected 'hello', got %v", clone["b"])
	}
}

func TestCloneMapEmpty(t *testing.T) {
	clone := cloneMap(map[string]any{})
	if len(clone) != 0 {
		t.Fatalf("expected empty, got %v", clone)
	}
}

func TestCloneMapNested(t *testing.T) {
	// cloneMap is shallow, so nested maps share references
	inner := map[string]any{"key": "value"}
	original := map[string]any{"nested": inner}
	clone := cloneMap(original)
	// Shallow clone means inner is shared
	inner["key"] = "modified"
	cloneNested := clone["nested"].(map[string]any)
	if cloneNested["key"] != "modified" {
		t.Fatal("expected shallow clone to share nested references")
	}
}

// helper
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && findSubstring(s, sub))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
