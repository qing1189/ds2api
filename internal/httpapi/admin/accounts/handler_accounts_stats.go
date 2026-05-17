package accounts

import (
	"net/http"
	"strings"

	"ds2api/internal/config"
)

// statsAccount returns per-account request statistics (total / success / failure
// requests + input / output token counts) joined with config metadata such as
// account name and remark.
func (h *Handler) statsAccounts(w http.ResponseWriter, _ *http.Request) {
	rows := h.Pool.AccountStatsSnapshot()
	if rows == nil {
		writeJSON(w, http.StatusOK, []map[string]any{})
		return
	}

	// Build a quick lookup from identifier -> account so we can attach name/remark.
	accounts := h.Store.Snapshot().Accounts
	byID := make(map[string]config.Account, len(accounts))
	for _, acc := range accounts {
		id := acc.Identifier()
		if id != "" {
			byID[id] = acc
		}
	}

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		id, _ := row["account_id"].(string)
		if acc, ok := byID[id]; ok {
			row["name"] = strings.TrimSpace(acc.Name)
			row["remark"] = strings.TrimSpace(acc.Remark)
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

// statsKeys returns per-API-key request statistics joined with key metadata
// (name, remark) and a masked preview so the raw key value never leaves the
// server in plain text.
func (h *Handler) statsKeys(w http.ResponseWriter, _ *http.Request) {
	rows := h.Pool.APIKeyStatsSnapshot()
	if rows == nil {
		writeJSON(w, http.StatusOK, []map[string]any{})
		return
	}

	cfg := h.Store.Snapshot()
	byKey := make(map[string]config.APIKey, len(cfg.APIKeys))
	for _, item := range cfg.APIKeys {
		if item.Key != "" {
			byKey[item.Key] = item
		}
	}

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		raw, _ := row["api_key"].(string)
		row["key_preview"] = maskSecretPreview(raw)
		// Never leak the raw key; replace api_key with the preview as the row id.
		delete(row, "api_key")
		row["api_key"] = maskSecretPreview(raw)
		if item, ok := byKey[raw]; ok {
			row["name"] = strings.TrimSpace(item.Name)
			row["remark"] = strings.TrimSpace(item.Remark)
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

// statsReset wipes all in-memory request statistics.
func (h *Handler) statsReset(w http.ResponseWriter, _ *http.Request) {
	h.Pool.ResetStats()
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
