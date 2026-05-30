package assistantturn

import (
	"net/http"
	"strings"

	"ds2api/internal/httpapi/openai/shared"
	"ds2api/internal/promptcompat"
	"ds2api/internal/sse"
	"ds2api/internal/util"
)

type StopReason string

const (
	StopReasonStop          StopReason = "stop"
	StopReasonContentFilter StopReason = "content_filter"
	StopReasonError         StopReason = "error"
)

type Usage struct {
	InputTokens     int
	OutputTokens    int
	ReasoningTokens int
	TotalTokens     int
}

type OutputError struct {
	Status  int
	Message string
	Code    string
}

// Turn models a single assistant response. Tool calling has been removed, so a
// turn now only ever carries plain text + reasoning.
type Turn struct {
	Model             string
	Prompt            string
	RawText           string
	RawThinking       string
	DetectionThinking string
	Text              string
	Thinking          string
	CitationLinks     map[int]string
	ContentFilter     bool
	ResponseMessageID int
	StopReason        StopReason
	Usage             Usage
	Error             *OutputError
}

type FinalizeOptions struct {
	// AlreadyEmittedToolCalls is retained for call-site compatibility but is
	// always false now that tool calling is removed.
	AlreadyEmittedToolCalls bool
}

type FinalOutcome struct {
	FinishReason     string
	Error            *OutputError
	Usage            Usage
	HasToolCalls     bool
	HasVisibleText   bool
	HasVisibleOutput bool
	ShouldFail       bool
}

type BuildOptions struct {
	Model                 string
	Prompt                string
	RefFileTokens         int
	SearchEnabled         bool
	StripReferenceMarkers bool
	// ToolNames / ToolsRaw / ToolChoice are accepted for API compatibility but
	// no longer influence output (tool calling removed).
	ToolNames  []string
	ToolsRaw   any
	ToolChoice promptcompat.ToolChoicePolicy
}

type StreamSnapshot struct {
	RawText           string
	VisibleText       string
	RawThinking       string
	VisibleThinking   string
	DetectionThinking string
	ContentFilter     bool
	CitationLinks     map[int]string
	ResponseMessageID int
}

func BuildTurnFromCollected(result sse.CollectResult, opts BuildOptions) Turn {
	thinking := shared.CleanVisibleOutput(result.Thinking, opts.StripReferenceMarkers)
	text := shared.CleanVisibleOutput(result.Text, opts.StripReferenceMarkers)
	if opts.SearchEnabled {
		text = shared.ReplaceCitationMarkersWithLinks(text, result.CitationLinks)
	}

	stopReason := StopReasonStop
	if result.ContentFilter {
		stopReason = StopReasonContentFilter
	}

	turn := Turn{
		Model:             opts.Model,
		Prompt:            opts.Prompt,
		RawText:           result.Text,
		RawThinking:       result.Thinking,
		DetectionThinking: result.ToolDetectionThinking,
		Text:              text,
		Thinking:          thinking,
		CitationLinks:     result.CitationLinks,
		ContentFilter:     result.ContentFilter,
		ResponseMessageID: result.ResponseMessageID,
		StopReason:        stopReason,
	}
	turn.Usage = BuildUsage(opts.Model, opts.Prompt, thinking, text, opts.RefFileTokens)
	turn.Error = ValidateTurn(turn, opts.ToolChoice)
	if turn.Error != nil {
		turn.StopReason = StopReasonError
	}
	return turn
}

func BuildTurnFromStreamSnapshot(snapshot StreamSnapshot, opts BuildOptions) Turn {
	thinking := shared.CleanVisibleOutput(snapshot.VisibleThinking, opts.StripReferenceMarkers)
	text := shared.CleanVisibleOutput(snapshot.VisibleText, opts.StripReferenceMarkers)
	if opts.SearchEnabled {
		text = shared.ReplaceCitationMarkersWithLinks(text, snapshot.CitationLinks)
	}

	stopReason := StopReasonStop
	if snapshot.ContentFilter {
		stopReason = StopReasonContentFilter
	}

	turn := Turn{
		Model:             opts.Model,
		Prompt:            opts.Prompt,
		RawText:           snapshot.RawText,
		RawThinking:       snapshot.RawThinking,
		DetectionThinking: snapshot.DetectionThinking,
		Text:              text,
		Thinking:          thinking,
		CitationLinks:     snapshot.CitationLinks,
		ContentFilter:     snapshot.ContentFilter,
		ResponseMessageID: snapshot.ResponseMessageID,
		StopReason:        stopReason,
	}
	turn.Usage = BuildUsage(opts.Model, opts.Prompt, thinking, text, opts.RefFileTokens)
	turn.Error = ValidateTurn(turn, opts.ToolChoice)
	if turn.Error != nil {
		turn.StopReason = StopReasonError
	}
	return turn
}

func BuildUsage(model, prompt, thinking, text string, refFileTokens int) Usage {
	inputTokens := util.CountPromptTokens(prompt, model) + refFileTokens
	reasoningTokens := util.CountOutputTokens(thinking, model)
	outputTokens := reasoningTokens + util.CountOutputTokens(text, model)
	return Usage{
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		ReasoningTokens: reasoningTokens,
		TotalTokens:     inputTokens + outputTokens,
	}
}

func ValidateTurn(turn Turn, _ promptcompat.ToolChoicePolicy) *OutputError {
	if strings.TrimSpace(turn.Text) != "" {
		return nil
	}
	status, message, code := UpstreamEmptyOutputDetail(turn.ContentFilter, turn.Text, turn.Thinking)
	return &OutputError{Status: status, Message: message, Code: code}
}

func UpstreamEmptyOutputDetail(contentFilter bool, text, thinking string) (int, string, string) {
	_ = text
	if contentFilter {
		return http.StatusBadRequest, "Upstream content filtered the response and returned no output.", "content_filter"
	}
	if strings.TrimSpace(thinking) != "" {
		return http.StatusTooManyRequests, "Upstream account hit a rate limit and returned reasoning without visible output.", "upstream_empty_output"
	}
	return http.StatusServiceUnavailable, "Upstream service is unavailable and returned no output.", "upstream_unavailable"
}

// ShouldRetryEmptyOutput returns true when the turn produced no visible text
// and no content filter. This includes thinking-only responses, where the model
// returned reasoning but no answer — a retry may yield text.
func ShouldRetryEmptyOutput(turn Turn, attempts, maxAttempts int) bool {
	return attempts < maxAttempts &&
		!turn.ContentFilter &&
		strings.TrimSpace(turn.Text) == ""
}

func FinalizeTurn(turn Turn, _ FinalizeOptions) FinalOutcome {
	hasVisibleText := strings.TrimSpace(turn.Text) != ""
	hasVisibleThinking := strings.TrimSpace(turn.Thinking) != ""
	return FinalOutcome{
		FinishReason:     FinishReason(turn),
		Error:            turn.Error,
		Usage:            turn.Usage,
		HasToolCalls:     false,
		HasVisibleText:   hasVisibleText,
		HasVisibleOutput: hasVisibleText || hasVisibleThinking,
		ShouldFail:       turn.Error != nil,
	}
}

func OpenAIChatUsage(turn Turn) map[string]any {
	return map[string]any{
		"prompt_tokens":     turn.Usage.InputTokens,
		"completion_tokens": turn.Usage.OutputTokens,
		"total_tokens":      turn.Usage.TotalTokens,
		"completion_tokens_details": map[string]any{
			"reasoning_tokens": turn.Usage.ReasoningTokens,
		},
	}
}

func OpenAIResponsesUsage(turn Turn) map[string]any {
	return map[string]any{
		"input_tokens":  turn.Usage.InputTokens,
		"output_tokens": turn.Usage.OutputTokens,
		"total_tokens":  turn.Usage.TotalTokens,
	}
}

func FinishReason(turn Turn) string {
	switch turn.StopReason {
	case StopReasonContentFilter:
		return "content_filter"
	default:
		return "stop"
	}
}
