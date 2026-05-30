package claude

import (
	"ds2api/internal/assistantturn"
	"ds2api/internal/responsehistory"

	streamengine "ds2api/internal/stream"
)

func (s *claudeStreamRuntime) closeThinkingBlock() {
	if !s.thinkingBlockOpen {
		return
	}
	s.send("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": s.thinkingBlockIndex,
	})
	s.thinkingBlockOpen = false
	s.thinkingBlockIndex = -1
}

func (s *claudeStreamRuntime) closeTextBlock() {
	if !s.textBlockOpen {
		return
	}
	s.send("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": s.textBlockIndex,
	})
	s.textBlockOpen = false
	s.textBlockIndex = -1
}

func (s *claudeStreamRuntime) finalize(stopReason string, deferEmptyOutput bool) bool {
	if s.ended {
		return true
	}

	s.closeTextBlock()

	turn := assistantturn.BuildTurnFromStreamSnapshot(assistantturn.StreamSnapshot{
		RawText:           s.rawText.String(),
		VisibleText:       s.text.String(),
		RawThinking:       s.rawThinking.String(),
		VisibleThinking:   s.thinking.String(),
		DetectionThinking: s.toolDetectionThinking.String(),
		ResponseMessageID: s.responseMessageID,
	}, assistantturn.BuildOptions{
		Model:                 s.model,
		Prompt:                s.promptTokenText,
		SearchEnabled:         s.searchEnabled,
		StripReferenceMarkers: s.stripReferenceMarkers,
		ToolNames:             s.toolNames,
		ToolsRaw:              s.toolsRaw,
	})
	finalText := turn.Text
	outcome := assistantturn.FinalizeTurn(turn, assistantturn.FinalizeOptions{})
	if outcome.ShouldFail {
		if deferEmptyOutput {
			return false
		}
		s.ended = true
		s.closeThinkingBlock()
		s.closeTextBlock()
		if s.history != nil {
			s.history.Error(outcome.Error.Status, outcome.Error.Message, outcome.Error.Code, responsehistory.ThinkingForArchive(turn.RawThinking, turn.DetectionThinking, turn.Thinking), responsehistory.TextForArchive(turn.RawText, turn.Text))
		}
		s.sendErrorWithCode(outcome.Error.Status, outcome.Error.Message, outcome.Error.Code)
		return true
	}

	s.ended = true
	s.closeThinkingBlock()

	if finalText != "" && !s.textEmitted {
		idx := s.nextBlockIndex
		s.nextBlockIndex++
		s.send("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": idx,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		})
		s.send("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": idx,
			"delta": map[string]any{
				"type": "text_delta",
				"text": finalText,
			},
		})
		s.textEmitted = true
		s.send("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": idx,
		})
	}

	if s.auth != nil {
		s.auth.MarkOutcome(true)
		s.auth.MarkUsage(turn.Usage.InputTokens, turn.Usage.OutputTokens)
	}
	if s.history != nil {
		s.history.Success(
			200,
			responsehistory.ThinkingForArchive(turn.RawThinking, turn.DetectionThinking, turn.Thinking),
			responsehistory.TextForArchive(turn.RawText, turn.Text),
			stopReason,
			responsehistory.GenericUsage(turn),
		)
	}

	s.send("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": outcome.Usage.OutputTokens,
		},
	})
	s.send("message_stop", map[string]any{"type": "message_stop"})
	return true
}

func (s *claudeStreamRuntime) onFinalize(reason streamengine.StopReason, scannerErr error) {
	if string(reason) == "upstream_error" {
		if s.history != nil {
			s.history.Error(500, s.upstreamErr, "upstream_error", responsehistory.ThinkingForArchive(s.rawThinking.String(), s.toolDetectionThinking.String(), s.thinking.String()), responsehistory.TextForArchive(s.rawText.String(), s.text.String()))
		}
		s.sendError(s.upstreamErr)
		return
	}
	if scannerErr != nil {
		if s.history != nil {
			s.history.Error(500, scannerErr.Error(), "error", responsehistory.ThinkingForArchive(s.rawThinking.String(), s.toolDetectionThinking.String(), s.thinking.String()), responsehistory.TextForArchive(s.rawText.String(), s.text.String()))
		}
		s.sendError(scannerErr.Error())
		return
	}
	s.finalize("end_turn", false)
}
