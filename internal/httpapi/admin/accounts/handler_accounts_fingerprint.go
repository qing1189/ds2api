package accounts

import (
	"net/http"

	dsprotocol "ds2api/internal/deepseek/protocol"
)

// rotateFingerprints clears every cached account fingerprint so the next
// request from each account starts fresh. The new fingerprint is still
// derived deterministically from the account ID, but combined with the
// per-process salt that was set at startup. Use this when the operator
// suspects DeepSeek-side risk control fingerprinting and wants to look like
// a fleet of brand-new devices.
func (h *Handler) rotateFingerprints(w http.ResponseWriter, _ *http.Request) {
	dsprotocol.ResetFingerprintCache()
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "已清空所有账号指纹缓存，下次请求会重新生成",
	})
}
