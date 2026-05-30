package claude

import (
	"ds2api/internal/assistantturn"

	"ds2api/internal/prompt"
	"ds2api/internal/util"
)

func BuildMessageResponseFromTurn(messageID, model string, turn assistantturn.Turn, exposeThinking bool) map[string]any {
	content := make([]map[string]any, 0, 4)
	if exposeThinking && turn.Thinking != "" {
		content = append(content, map[string]any{"type": "thinking", "thinking": turn.Thinking})
	}
	text := turn.Text
	if text == "" && exposeThinking {
		text = turn.Thinking
	}
	if text == "" {
		text = "抱歉，没有生成有效的响应内容。"
	}
	content = append(content, map[string]any{"type": "text", "text": text})
	return map[string]any{
		"id":            messageID,
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       content,
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  turn.Usage.InputTokens,
			"output_tokens": turn.Usage.OutputTokens,
		},
	}
}

// BuildMessageResponse renders a plain-text Claude message. Tool calling has
// been removed; toolNames is accepted for call-site compatibility but ignored.
func BuildMessageResponse(messageID, model string, normalizedMessages []any, finalThinking, finalText string, _ []string) map[string]any {
	content := make([]map[string]any, 0, 4)
	if finalThinking != "" {
		content = append(content, map[string]any{"type": "thinking", "thinking": finalThinking})
	}
	if finalText == "" {
		finalText = "抱歉，没有生成有效的响应内容。"
	}
	content = append(content, map[string]any{"type": "text", "text": finalText})
	return map[string]any{
		"id":            messageID,
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       content,
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  util.CountPromptTokens(prompt.MessagesPrepareWithThinking(claudeMessageMaps(normalizedMessages), false), model),
			"output_tokens": util.CountOutputTokens(finalThinking, model) + util.CountOutputTokens(finalText, model),
		},
	}
}

func claudeMessageMaps(messages []any) []map[string]any {
	if len(messages) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(messages))
	for _, item := range messages {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, msg)
	}
	return out
}
