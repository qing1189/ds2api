package openai

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// BuildResponseObject renders a plain-text Responses-API object. Tool calling
// has been removed; toolNames / toolsRaw are accepted for call-site
// compatibility but ignored.
func BuildResponseObject(responseID, model, finalPrompt, finalThinking, finalText string, _ []string, _ any) map[string]any {
	exposedOutputText := finalText
	content := make([]any, 0, 2)
	if finalThinking != "" {
		content = append(content, map[string]any{
			"type": "reasoning",
			"text": finalThinking,
		})
	}
	if strings.TrimSpace(finalText) != "" {
		content = append(content, map[string]any{
			"type": "output_text",
			"text": finalText,
		})
	}
	if strings.TrimSpace(finalText) == "" && strings.TrimSpace(finalThinking) != "" {
		exposedOutputText = finalThinking
	}
	output := []any{map[string]any{
		"type":    "message",
		"id":      "msg_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		"role":    "assistant",
		"content": content,
	}}
	return BuildResponseObjectFromItems(
		responseID,
		model,
		finalPrompt,
		finalThinking,
		finalText,
		output,
		exposedOutputText,
	)
}

func BuildResponseObjectFromItems(responseID, model, finalPrompt, finalThinking, finalText string, output []any, outputText string) map[string]any {
	if output == nil {
		output = []any{}
	}
	return map[string]any{
		"id":          responseID,
		"type":        "response",
		"object":      "response",
		"created_at":  time.Now().Unix(),
		"status":      "completed",
		"model":       model,
		"output":      output,
		"output_text": outputText,
		"usage":       BuildResponsesUsageForModel(model, finalPrompt, finalThinking, finalText, 0),
	}
}
