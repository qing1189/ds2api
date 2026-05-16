package accounts

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) queueWeights(w http.ResponseWriter, _ *http.Request) {
	status := h.Pool.WeightStatus()
	if status == nil {
		writeJSON(w, http.StatusOK, []map[string]any{})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) reenableAccount(w http.ResponseWriter, r *http.Request) {
	identifier := chi.URLParam(r, "identifier")
	if decoded, err := url.PathUnescape(identifier); err == nil {
		identifier = decoded
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "identifier 不能为空"})
		return
	}

	// Check the account exists in config.
	if _, ok := findAccountByIdentifier(h.Store, identifier); !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "账号不存在"})
		return
	}

	if h.Pool.ReenableAccount(identifier) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "账号已重新启用"})
	} else {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "启用失败"})
	}
}
