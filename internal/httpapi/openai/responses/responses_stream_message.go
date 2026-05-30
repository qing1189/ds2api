package responses

import (
	"strings"

	openaifmt "ds2api/internal/format/openai"

	"github.com/google/uuid"
)

// This file holds the plain message (text / reasoning) streaming helpers for the
// Responses API. Tool calling has been removed, so only the assistant message
// item is ever emitted.

func (s *responsesStreamRuntime) allocateOutputIndex() int {
	idx := s.nextOutputID
	s.nextOutputID++
	return idx
}

func (s *responsesStreamRuntime) ensureMessageItemID() string {
	if strings.TrimSpace(s.messageItemID) != "" {
		return s.messageItemID
	}
	s.messageItemID = "msg_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	return s.messageItemID
}

func (s *responsesStreamRuntime) ensureMessageOutputIndex() int {
	if s.messageOutputID >= 0 {
		return s.messageOutputID
	}
	s.messageOutputID = s.allocateOutputIndex()
	return s.messageOutputID
}

func (s *responsesStreamRuntime) ensureMessageItemAdded() {
	if s.messageAdded {
		return
	}
	itemID := s.ensureMessageItemID()
	item := map[string]any{
		"id":     itemID,
		"type":   "message",
		"role":   "assistant",
		"status": "in_progress",
	}
	s.sendEvent(
		"response.output_item.added",
		openaifmt.BuildResponsesOutputItemAddedPayload(s.responseID, itemID, s.ensureMessageOutputIndex(), item),
	)
	s.messageAdded = true
}

func (s *responsesStreamRuntime) ensureMessageContentPartAdded() {
	if s.messagePartAdded {
		return
	}
	s.ensureMessageItemAdded()
	s.sendEvent(
		"response.content_part.added",
		openaifmt.BuildResponsesContentPartAddedPayload(
			s.responseID,
			s.ensureMessageItemID(),
			s.ensureMessageOutputIndex(),
			0,
			map[string]any{"type": "output_text", "text": ""},
		),
	)
	s.messagePartAdded = true
}

func (s *responsesStreamRuntime) emitTextDelta(content string) {
	if content == "" {
		return
	}
	s.ensureMessageContentPartAdded()
	s.visibleText.WriteString(content)
	s.sendEvent(
		"response.output_text.delta",
		openaifmt.BuildResponsesTextDeltaPayload(
			s.responseID,
			s.ensureMessageItemID(),
			s.ensureMessageOutputIndex(),
			0,
			content,
		),
	)
}

func (s *responsesStreamRuntime) closeMessageItem() {
	if !s.messageAdded {
		return
	}
	itemID := s.ensureMessageItemID()
	outputIndex := s.ensureMessageOutputIndex()
	text := s.visibleText.String()
	if s.messagePartAdded {
		s.sendEvent(
			"response.output_text.done",
			openaifmt.BuildResponsesTextDonePayload(s.responseID, itemID, outputIndex, 0, text),
		)
		s.sendEvent(
			"response.content_part.done",
			openaifmt.BuildResponsesContentPartDonePayload(
				s.responseID,
				itemID,
				outputIndex,
				0,
				map[string]any{"type": "output_text", "text": text},
			),
		)
		s.messagePartAdded = false
	}
	item := map[string]any{
		"id":     itemID,
		"type":   "message",
		"role":   "assistant",
		"status": "completed",
		"content": []map[string]any{
			{
				"type": "output_text",
				"text": text,
			},
		},
	}
	s.sendEvent(
		"response.output_item.done",
		openaifmt.BuildResponsesOutputItemDonePayload(s.responseID, itemID, outputIndex, item),
	)
}

func (s *responsesStreamRuntime) buildCompletedResponseObject(finalThinking, finalText string) map[string]any {
	content := make([]map[string]any, 0, 2)
	if s.messageAdded {
		content = append(content, map[string]any{
			"type": "output_text",
			"text": s.visibleText.String(),
		})
	} else {
		if finalThinking != "" {
			content = append(content, map[string]any{
				"type": "reasoning",
				"text": finalThinking,
			})
		}
		if finalText != "" {
			content = append(content, map[string]any{
				"type": "output_text",
				"text": finalText,
			})
		}
	}

	output := make([]any, 0, 1)
	if len(content) > 0 {
		output = append(output, map[string]any{
			"id":      s.ensureMessageItemID(),
			"type":    "message",
			"role":    "assistant",
			"status":  "completed",
			"content": content,
		})
	}

	outputText := s.visibleText.String()
	if outputText == "" {
		if finalText != "" {
			outputText = finalText
		} else if finalThinking != "" {
			outputText = finalThinking
		}
	}

	obj := openaifmt.BuildResponseObjectFromItems(
		s.responseID,
		s.model,
		s.finalPrompt,
		finalThinking,
		finalText,
		output,
		outputText,
	)
	if s.refFileTokens > 0 {
		addRefFileTokensToUsage(obj, s.refFileTokens)
	}
	return obj
}
