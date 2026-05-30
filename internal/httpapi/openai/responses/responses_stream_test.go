package responses

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ds2api/internal/promptcompat"
)

func TestHandleResponsesStreamDoesNotEmitReasoningTextCompatEvents(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()

	b, _ := json.Marshal(map[string]any{
		"p": "response/thinking_content",
		"v": "thought",
	})
	streamBody := "data: " + string(b) + "\n" + "data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-v4-pro", "prompt", 0, true, false, nil, nil, promptcompat.DefaultToolChoicePolicy(), "")

	body := rec.Body.String()
	if !strings.Contains(body, "event: response.reasoning.delta") {
		t.Fatalf("expected response.reasoning.delta event, body=%s", body)
	}
	if strings.Contains(body, "event: response.reasoning_text.delta") || strings.Contains(body, "event: response.reasoning_text.done") {
		t.Fatalf("did not expect response.reasoning_text.* compatibility events, body=%s", body)
	}
}

func TestHandleResponsesStreamEmitsOutputTextDoneBeforeContentPartDone(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()

	sseLine := func(v string) string {
		b, _ := json.Marshal(map[string]any{
			"p": "response/content",
			"v": v,
		})
		return "data: " + string(b) + "\n"
	}

	streamBody := sseLine("hello") + "data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-v4-flash", "prompt", 0, false, false, nil, nil, promptcompat.DefaultToolChoicePolicy(), "")
	body := rec.Body.String()
	if !strings.Contains(body, "event: response.output_text.done") {
		t.Fatalf("expected response.output_text.done payload, body=%s", body)
	}
	textDoneIdx := strings.Index(body, "event: response.output_text.done")
	partDoneIdx := strings.Index(body, "event: response.content_part.done")
	if textDoneIdx < 0 || partDoneIdx < 0 {
		t.Fatalf("expected output_text.done + content_part.done, body=%s", body)
	}
	if textDoneIdx > partDoneIdx {
		t.Fatalf("expected output_text.done before content_part.done, body=%s", body)
	}
}

func TestHandleResponsesStreamOutputTextDeltaCarriesItemIndexes(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()

	sseLine := func(v string) string {
		b, _ := json.Marshal(map[string]any{
			"p": "response/content",
			"v": v,
		})
		return "data: " + string(b) + "\n"
	}

	streamBody := sseLine("hello") + "data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-v4-flash", "prompt", 0, false, false, nil, nil, promptcompat.DefaultToolChoicePolicy(), "")
	body := rec.Body.String()

	deltaPayload, ok := extractSSEEventPayload(body, "response.output_text.delta")
	if !ok {
		t.Fatalf("expected response.output_text.delta payload, body=%s", body)
	}
	if strings.TrimSpace(asString(deltaPayload["item_id"])) == "" {
		t.Fatalf("expected non-empty item_id in output_text.delta, payload=%#v", deltaPayload)
	}
	if _, ok := deltaPayload["output_index"]; !ok {
		t.Fatalf("expected output_index in output_text.delta, payload=%#v", deltaPayload)
	}
	if _, ok := deltaPayload["content_index"]; !ok {
		t.Fatalf("expected content_index in output_text.delta, payload=%#v", deltaPayload)
	}
}

func TestHandleResponsesStreamCoalescesSmallOutputTextDeltas(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()

	var streamBody strings.Builder
	for i := 0; i < 100; i++ {
		b, _ := json.Marshal(map[string]any{
			"p": "response/content",
			"v": "字",
		})
		streamBody.WriteString("data: ")
		streamBody.WriteString(string(b))
		streamBody.WriteString("\n")
	}
	streamBody.WriteString("data: [DONE]\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody.String())),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_coalesce", "deepseek-v4-flash", "prompt", 0, false, false, nil, nil, promptcompat.DefaultToolChoicePolicy(), "")

	payloads := extractSSEEventPayloads(rec.Body.String(), "response.output_text.delta")
	if len(payloads) == 0 {
		t.Fatalf("expected response.output_text.delta payloads, body=%s", rec.Body.String())
	}
	var content strings.Builder
	for _, payload := range payloads {
		content.WriteString(asString(payload["delta"]))
	}
	if got, want := content.String(), strings.Repeat("字", 100); got != want {
		t.Fatalf("coalesced response content mismatch: got %q want %q body=%s", got, want, rec.Body.String())
	}
	if len(payloads) >= 100 {
		t.Fatalf("expected coalescing to reduce 100 tiny text deltas, got %d body=%s", len(payloads), rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: response.completed") {
		t.Fatalf("expected completed event, body=%s", rec.Body.String())
	}
}

func TestHandleResponsesStreamFailsWhenUpstreamHasOnlyThinking(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()

	sseLine := func(path, value string) string {
		b, _ := json.Marshal(map[string]any{
			"p": path,
			"v": value,
		})
		return "data: " + string(b) + "\n"
	}

	streamBody := sseLine("response/thinking_content", "Only thinking") + "data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-v4-pro", "prompt", 0, true, false, nil, nil, promptcompat.DefaultToolChoicePolicy(), "")

	body := rec.Body.String()
	if !strings.Contains(body, "event: response.failed") {
		t.Fatalf("expected response.failed event, body=%s", body)
	}
	if strings.Contains(body, "event: response.completed") {
		t.Fatalf("did not expect response.completed, body=%s", body)
	}
	payload, ok := extractSSEEventPayload(body, "response.failed")
	if !ok {
		t.Fatalf("expected response.failed payload, body=%s", body)
	}
	errObj, _ := payload["error"].(map[string]any)
	if asString(errObj["code"]) != "upstream_empty_output" {
		t.Fatalf("expected code=upstream_empty_output, got %#v", payload)
	}
}

func TestHandleResponsesNonStreamSingleAttemptReturns503WhenUpstreamOutputEmpty(t *testing.T) {
	h := &Handler{}
	rec := httptest.NewRecorder()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`data: {"p":"response/content","v":""}` + "\n" +
				`data: [DONE]` + "\n",
		)),
	}

	h.handleResponsesNonStream(rec, resp, "owner-a", "resp_test", "deepseek-v4-flash", "prompt", 0, false, false, nil, nil, promptcompat.DefaultToolChoicePolicy(), "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for empty upstream output, got %d body=%s", rec.Code, rec.Body.String())
	}
	out := decodeJSONBody(t, rec.Body.String())
	errObj, _ := out["error"].(map[string]any)
	if asString(errObj["code"]) != "upstream_unavailable" {
		t.Fatalf("expected code=upstream_unavailable, got %#v", out)
	}
}

func TestHandleResponsesNonStreamSingleAttemptReturnsContentFilterErrorWhenUpstreamFilteredWithoutOutput(t *testing.T) {
	h := &Handler{}
	rec := httptest.NewRecorder()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`data: {"code":"content_filter"}` + "\n" +
				`data: [DONE]` + "\n",
		)),
	}

	h.handleResponsesNonStream(rec, resp, "owner-a", "resp_test", "deepseek-v4-flash", "prompt", 0, false, false, nil, nil, promptcompat.DefaultToolChoicePolicy(), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for filtered empty upstream output, got %d body=%s", rec.Code, rec.Body.String())
	}
	out := decodeJSONBody(t, rec.Body.String())
	errObj, _ := out["error"].(map[string]any)
	if asString(errObj["code"]) != "content_filter" {
		t.Fatalf("expected code=content_filter, got %#v", out)
	}
}

func TestHandleResponsesNonStreamSingleAttemptReturns429WhenUpstreamHasOnlyThinking(t *testing.T) {
	h := &Handler{}
	rec := httptest.NewRecorder()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`data: {"p":"response/thinking_content","v":"Only thinking"}` + "\n" +
				`data: [DONE]` + "\n",
		)),
	}

	h.handleResponsesNonStream(rec, resp, "owner-a", "resp_test", "deepseek-v4-pro", "prompt", 0, true, false, nil, nil, promptcompat.DefaultToolChoicePolicy(), "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for thinking-only upstream output, got %d body=%s", rec.Code, rec.Body.String())
	}
	out := decodeJSONBody(t, rec.Body.String())
	errObj, _ := out["error"].(map[string]any)
	if asString(errObj["code"]) != "upstream_empty_output" {
		t.Fatalf("expected code=upstream_empty_output, got %#v", out)
	}
}

func extractSSEEventPayload(body, targetEvent string) (map[string]any, bool) {
	scanner := bufio.NewScanner(strings.NewReader(body))
	matched := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "event: ") {
			evt := strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			matched = evt == targetEvent
			continue
		}
		if !matched || !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if raw == "" || raw == "[DONE]" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, false
		}
		return payload, true
	}
	return nil, false
}

func extractSSEEventPayloads(body, targetEvent string) []map[string]any {
	scanner := bufio.NewScanner(strings.NewReader(body))
	matched := false
	out := make([]map[string]any, 0, 4)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "event: ") {
			evt := strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			matched = evt == targetEvent
			continue
		}
		if !matched || !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if raw == "" || raw == "[DONE]" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			continue
		}
		out = append(out, payload)
	}
	return out
}
