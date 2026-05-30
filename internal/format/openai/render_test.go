package openai

import (
	"strings"
	"testing"

	"ds2api/internal/util"
)

// Tool calling has been removed. Any tool-looking payloads in the model output
// are now rendered as plain text, never promoted to function calls.

func TestBuildResponseObjectKeepsFencedToolPayloadAsText(t *testing.T) {
	obj := BuildResponseObject(
		"resp_test",
		"gpt-4o",
		"prompt",
		"",
		"```json\n{\"tool_calls\":[{\"name\":\"search\",\"input\":{\"q\":\"golang\"}}]}\n```",
		[]string{"search"},
		nil,
	)

	outputText, _ := obj["output_text"].(string)
	if !strings.Contains(outputText, "\"tool_calls\"") {
		t.Fatalf("expected output_text to preserve fenced tool payload, got %q", outputText)
	}
	output, _ := obj["output"].([]any)
	if len(output) != 1 {
		t.Fatalf("expected one message output item, got %#v", obj["output"])
	}
	first, _ := output[0].(map[string]any)
	if first["type"] != "message" {
		t.Fatalf("expected message output type, got %#v", first["type"])
	}
}

func TestBuildResponseObjectReasoningOnlyFallsBackToOutputText(t *testing.T) {
	obj := BuildResponseObject(
		"resp_test",
		"gpt-4o",
		"prompt",
		"internal thinking content",
		"",
		nil,
		nil,
	)

	outputText, _ := obj["output_text"].(string)
	if outputText == "" {
		t.Fatalf("expected output_text fallback from reasoning when final text is empty")
	}

	output, _ := obj["output"].([]any)
	if len(output) != 1 {
		t.Fatalf("expected one output item, got %#v", obj["output"])
	}
	first, _ := output[0].(map[string]any)
	if first["type"] != "message" {
		t.Fatalf("expected output type message, got %#v", first["type"])
	}
	content, _ := first["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("expected reasoning content, got %#v", first["content"])
	}
	block0, _ := content[0].(map[string]any)
	if block0["type"] != "reasoning" {
		t.Fatalf("expected first content block reasoning, got %#v", block0["type"])
	}
}

func TestBuildChatCompletionRendersPlainText(t *testing.T) {
	obj := BuildChatCompletion(
		"chat_test",
		"gpt-4o",
		"prompt",
		"",
		`<tool_calls><invoke name="Write"><parameter name="content">x</parameter></invoke></tool_calls>`,
		[]string{"Write"},
		nil,
	)
	choices, _ := obj["choices"].([]map[string]any)
	if len(choices) != 1 || choices[0]["finish_reason"] != "stop" {
		t.Fatalf("expected single stop choice, got %#v", obj["choices"])
	}
	message, _ := choices[0]["message"].(map[string]any)
	if _, hasTool := message["tool_calls"]; hasTool {
		t.Fatalf("expected no tool_calls in message, got %#v", message)
	}
	if message["content"] == nil || message["content"] == "" {
		t.Fatalf("expected tool markup preserved as plain text content, got %#v", message["content"])
	}
}

func TestBuildChatUsageForModelUsesConservativePromptCount(t *testing.T) {
	prompt := strings.Repeat("上下文token ", 40)
	usage := BuildChatUsageForModel("deepseek-v4-flash", prompt, "", "ok", 0)
	promptTokens, _ := usage["prompt_tokens"].(int)
	if promptTokens <= util.EstimateTokens(prompt) {
		t.Fatalf("expected conservative prompt token count > rough estimate, got=%d estimate=%d", promptTokens, util.EstimateTokens(prompt))
	}
	totalTokens, _ := usage["total_tokens"].(int)
	completionTokens, _ := usage["completion_tokens"].(int)
	if totalTokens != promptTokens+completionTokens {
		t.Fatalf("expected total tokens to add up, got usage=%#v", usage)
	}
}
