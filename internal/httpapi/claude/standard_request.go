package claude

import (
	"fmt"
	"strings"

	"ds2api/internal/config"
	"ds2api/internal/prompt"
	"ds2api/internal/promptcompat"
	"ds2api/internal/util"
)

type claudeNormalizedRequest struct {
	Standard           promptcompat.StandardRequest
	NormalizedMessages []any
}

func normalizeClaudeRequest(store ConfigReader, req map[string]any) (claudeNormalizedRequest, error) {
	model, _ := req["model"].(string)
	messagesRaw, _ := req["messages"].([]any)
	if strings.TrimSpace(model) == "" || len(messagesRaw) == 0 {
		return claudeNormalizedRequest{}, fmt.Errorf("request must include 'model' and 'messages'")
	}
	if _, ok := req["max_tokens"]; !ok {
		req["max_tokens"] = 8192
	}
	normalizedMessages := normalizeClaudeMessages(messagesRaw)
	payload := cloneMap(req)
	payload["messages"] = normalizedMessages
	toolsRequested, _ := req["tools"].([]any)

	dsPayload := convertClaudeToDeepSeek(payload, store)
	dsModel, _ := dsPayload["model"].(string)
	defaultThinkingEnabled, searchEnabled, ok := config.GetModelConfig(dsModel)
	if !ok {
		searchEnabled = false
	}
	thinkingEnabled := util.ResolveThinkingEnabled(req, defaultThinkingEnabled)
	if config.IsNoThinkingModel(dsModel) {
		thinkingEnabled = false
	}
	finalPrompt := prompt.MessagesPrepareWithThinking(toMessageMaps(dsPayload["messages"]), thinkingEnabled)

	return claudeNormalizedRequest{
		Standard: promptcompat.StandardRequest{
			Surface:         "anthropic_messages",
			RequestedModel:  strings.TrimSpace(model),
			ResolvedModel:   dsModel,
			ResponseModel:   strings.TrimSpace(model),
			Messages:        normalizedMessages,
			PromptTokenText: finalPrompt,
			ToolsRaw:        toolsRequested,
			FinalPrompt:     finalPrompt,
			ToolNames:       nil,
			Stream:          util.ToBool(req["stream"]),
			Thinking:        thinkingEnabled,
			Search:          searchEnabled,
		},
		NormalizedMessages: normalizedMessages,
	}, nil
}
