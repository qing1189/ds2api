package promptcompat

import (
	"strings"
	"testing"
)

// Tool calling has been removed. These tests assert that client-provided tools
// and assistant tool_calls history never modify the prompt sent to DeepSeek
// (no DSML / tool-instruction injection), so requests look like plain
// conversations.

func TestBuildOpenAIFinalPrompt_ToolsDoNotInjectDSML(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "查北京天气"},
		map[string]any{
			"role": "assistant",
			"tool_calls": []any{
				map[string]any{
					"id": "call_1",
					"function": map[string]any{
						"name":      "get_weather",
						"arguments": "{\"city\":\"beijing\"}",
					},
				},
			},
		},
		map[string]any{
			"role":         "tool",
			"tool_call_id": "call_1",
			"name":         "get_weather",
			"content":      map[string]any{"temp": 18, "condition": "sunny"},
		},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "get_weather",
				"description": "Get weather",
				"parameters":  map[string]any{"type": "object"},
			},
		},
	}

	finalPrompt, toolNames := buildOpenAIFinalPrompt(messages, tools, "", false)
	if len(toolNames) != 0 {
		t.Fatalf("expected no tool names after tool-call removal, got: %#v", toolNames)
	}
	if strings.Contains(finalPrompt, "DSML") || strings.Contains(finalPrompt, "TOOL CALL FORMAT") {
		t.Fatalf("prompt must not contain any tool-call / DSML markup, got: %q", finalPrompt)
	}
}

func TestBuildOpenAIPromptIgnoresToolsParameter(t *testing.T) {
	messages := []any{
		map[string]any{"role": "system", "content": "You are helpful"},
		map[string]any{"role": "user", "content": "请调用工具"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "search",
				"description": "search docs",
				"parameters":  map[string]any{"type": "object"},
			},
		},
	}

	withTools, _ := BuildOpenAIPrompt(messages, tools, "", DefaultToolChoicePolicy(), false)
	withoutTools, _ := BuildOpenAIPrompt(messages, nil, "", DefaultToolChoicePolicy(), false)
	if withTools != withoutTools {
		t.Fatalf("tools parameter must not change the prompt; with=%q without=%q", withTools, withoutTools)
	}
}

func TestBuildOpenAIFinalPromptWithThinkingKeepsPromptUnchanged(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "继续回答上一个问题"},
	}

	finalPromptThinking, _ := buildOpenAIFinalPrompt(messages, nil, "", true)
	finalPromptPlain, _ := buildOpenAIFinalPrompt(messages, nil, "", false)
	if finalPromptThinking != finalPromptPlain {
		t.Fatalf("expected thinking flag not to prepend continuation contract, thinking=%q plain=%q", finalPromptThinking, finalPromptPlain)
	}
}
