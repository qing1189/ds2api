package promptcompat

import (
	"ds2api/internal/prompt"
)

func buildOpenAIFinalPrompt(messagesRaw []any, toolsRaw any, traceID string, thinkingEnabled bool) (string, []string) {
	return BuildOpenAIPrompt(messagesRaw, toolsRaw, traceID, DefaultToolChoicePolicy(), thinkingEnabled)
}

// BuildOpenAIPrompt builds the plain-text prompt sent to DeepSeek.
//
// Tool calling has been removed: client-provided tools never modify the prompt
// (no DSML / tool-instruction injection). The toolsRaw / toolPolicy parameters
// are accepted for API compatibility but ignored. The returned tool-name slice
// is always empty.
func BuildOpenAIPrompt(messagesRaw []any, toolsRaw any, traceID string, toolPolicy ToolChoicePolicy, thinkingEnabled bool) (string, []string) {
	return buildOpenAIPrompt(messagesRaw, toolsRaw, traceID, toolPolicy, thinkingEnabled, true)
}

// BuildOpenAIPromptWithToolInstructionsOnly is retained for API compatibility.
// It behaves identically to BuildOpenAIPrompt now that tool injection is gone.
func BuildOpenAIPromptWithToolInstructionsOnly(messagesRaw []any, toolsRaw any, traceID string, toolPolicy ToolChoicePolicy, thinkingEnabled bool) (string, []string) {
	return buildOpenAIPrompt(messagesRaw, toolsRaw, traceID, toolPolicy, thinkingEnabled, false)
}

func buildOpenAIPrompt(messagesRaw []any, _ any, traceID string, _ ToolChoicePolicy, thinkingEnabled bool, _ bool) (string, []string) {
	messages := NormalizeOpenAIMessagesForPrompt(messagesRaw, traceID)
	return prompt.MessagesPrepareWithThinking(messages, thinkingEnabled), []string{}
}

// BuildOpenAIPromptForAdapter exposes the OpenAI-compatible prompt building flow so
// other protocol adapters (for example Gemini) can reuse the same history
// normalization logic and remain behavior-compatible with chat/completions.
func BuildOpenAIPromptForAdapter(messagesRaw []any, toolsRaw any, traceID string, thinkingEnabled bool) (string, []string) {
	return buildOpenAIFinalPrompt(messagesRaw, toolsRaw, traceID, thinkingEnabled)
}
