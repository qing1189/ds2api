package client

import (
	"context"
	dsprotocol "ds2api/internal/deepseek/protocol"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"ds2api/internal/auth"
)

// DeepSeek's web client fetches two short-lived anti-bot validation tokens
// ("hidden integration feature") and replays them as request headers on the
// chat / session / pow / completion endpoints. Reproducing them is the main
// behavioral difference between the verified reference web flow and a plain
// scripted client, so we fetch + cache them per upstream token.
const (
	hifLeimURL           = "https://hif-leim.deepseek.com/query"
	hifDliqURL           = "https://hif-dliq.deepseek.com/query"
	hifDefaultTTLSeconds = 600
)

type hifEntry struct {
	leim      string
	dliq      string
	expiresAt time.Time
}

// injectHIFHeaders adds x-hif-leim / x-hif-dliq to the outgoing header set.
//
// It is best-effort: if the HIF endpoints are unreachable, return nothing or
// are disabled via DS2API_DISABLE_HIF, the request simply proceeds without
// the headers (matching the reference project's behavior).
func (c *Client) injectHIFHeaders(ctx context.Context, a *auth.RequestAuth, headers map[string]string) {
	if c == nil || a == nil || headers == nil {
		return
	}
	if hifDisabled() {
		return
	}
	token := strings.TrimSpace(a.DeepSeekToken)
	if token == "" {
		return
	}
	leim, dliq := c.hifTokens(ctx, a, token)
	if leim != "" {
		headers["x-hif-leim"] = leim
	}
	if dliq != "" {
		headers["x-hif-dliq"] = dliq
	}
}

func hifDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DS2API_DISABLE_HIF"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// hifTokens returns the cached HIF tokens for an upstream token, refreshing
// them when missing or expired.
func (c *Client) hifTokens(ctx context.Context, a *auth.RequestAuth, token string) (string, string) {
	c.hifMu.Lock()
	if c.hifCache == nil {
		c.hifCache = map[string]hifEntry{}
	}
	if entry, ok := c.hifCache[token]; ok && time.Now().Before(entry.expiresAt) {
		c.hifMu.Unlock()
		return entry.leim, entry.dliq
	}
	c.hifMu.Unlock()

	leim, leimTTL := c.fetchHIF(ctx, a, hifLeimURL, token)
	dliq, dliqTTL := c.fetchHIF(ctx, a, hifDliqURL, token)
	if leim == "" && dliq == "" {
		return "", ""
	}

	ttl := leimTTL
	if dliqTTL < ttl {
		ttl = dliqTTL
	}
	if ttl <= 0 {
		ttl = hifDefaultTTLSeconds
	}

	c.hifMu.Lock()
	if c.hifCache == nil {
		c.hifCache = map[string]hifEntry{}
	}
	prev := c.hifCache[token]
	if leim == "" {
		leim = prev.leim
	}
	if dliq == "" {
		dliq = prev.dliq
	}
	c.hifCache[token] = hifEntry{
		leim:      leim,
		dliq:      dliq,
		expiresAt: time.Now().Add(time.Duration(ttl) * time.Second),
	}
	c.hifMu.Unlock()
	return leim, dliq
}

// fetchHIF performs a single GET against a HIF endpoint and returns the token
// value plus its TTL (seconds). Failures are swallowed and reported as an
// empty value so callers can degrade gracefully.
func (c *Client) fetchHIF(ctx context.Context, a *auth.RequestAuth, rawURL, token string) (string, int) {
	clients := c.requestClientsForAuth(ctx, a)
	fp := dsprotocol.FingerprintForAccount(a.AccountID)

	hdr := map[string]string{
		"User-Agent":      fp.UserAgent,
		"Accept":          "*/*",
		"Accept-Encoding": "gzip, deflate, br",
		"Origin":          "https://chat.deepseek.com",
		"Referer":         "https://chat.deepseek.com/",
		"authorization":   "Bearer " + token,
	}
	if fp.AcceptLang != "" {
		hdr["Accept-Language"] = fp.AcceptLang
	}

	build := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		return req, nil
	}

	req, err := build()
	if err != nil {
		return "", 0
	}
	resp, err := clients.regular.Do(req)
	if err != nil {
		req2, buildErr := build()
		if buildErr != nil {
			return "", 0
		}
		resp, err = clients.fallback.Do(req2)
		if err != nil {
			return "", 0
		}
	}
	defer func() { _ = resp.Body.Close() }()

	ttl := hifTTLFromHeader(resp.Header.Get("x-hif-ttl"))
	body, err := readResponseBody(resp)
	if err != nil {
		return "", ttl
	}
	return extractHIFValue(body), ttl
}

func extractHIFValue(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ""
	}
	data, _ := parsed["data"].(map[string]any)
	bizData, _ := data["biz_data"].(map[string]any)
	value, _ := bizData["value"].(string)
	return strings.TrimSpace(value)
}

func hifTTLFromHeader(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return hifDefaultTTLSeconds
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return hifDefaultTTLSeconds
	}
	return v
}
