package protocol

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
)

const (
	DeepSeekHost                 = "chat.deepseek.com"
	DeepSeekLoginURL             = "https://chat.deepseek.com/api/v0/users/login"
	DeepSeekCreateSessionURL     = "https://chat.deepseek.com/api/v0/chat_session/create"
	DeepSeekCreatePowURL         = "https://chat.deepseek.com/api/v0/chat/create_pow_challenge"
	DeepSeekCompletionURL        = "https://chat.deepseek.com/api/v0/chat/completion"
	DeepSeekContinueURL          = "https://chat.deepseek.com/api/v0/chat/continue"
	DeepSeekUploadFileURL        = "https://chat.deepseek.com/api/v0/file/upload_file"
	DeepSeekFetchFilesURL        = "https://chat.deepseek.com/api/v0/file/fetch_files"
	DeepSeekFetchSessionURL      = "https://chat.deepseek.com/api/v0/chat_session/fetch_page"
	DeepSeekDeleteSessionURL     = "https://chat.deepseek.com/api/v0/chat_session/delete"
	DeepSeekDeleteAllSessionsURL = "https://chat.deepseek.com/api/v0/chat_session/delete_all"
	DeepSeekCompletionTargetPath = "/api/v0/chat/completion"
	DeepSeekUploadTargetPath     = "/api/v0/file/upload_file"
)

// DefaultWebUserAgent is a realistic recent Chrome-on-Windows UA. It is only
// used for the global BaseHeaders (proxy connectivity test); real per-account
// request UAs come from BaseHeadersForFingerprint.
const DefaultWebUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// defaultStaticBaseHeaders are the headers a real DeepSeek web client (Chrome)
// consistently sends. We model the browser session of the reference project
// ds2026530, which is verified to avoid DeepSeek risk control. Per-account
// fields (User-Agent, sec-ch-ua, Accept-Language, Cookie) are layered on top
// by BaseHeadersForFingerprint.
var defaultStaticBaseHeaders = map[string]string{
	"Host":                     "chat.deepseek.com",
	"Accept":                   "*/*",
	"Content-Type":             "application/json",
	"Accept-Encoding":          "gzip, deflate, br",
	"Accept-Language":          "zh-CN,zh;q=0.9",
	"Origin":                   "https://chat.deepseek.com",
	"Referer":                  "https://chat.deepseek.com/",
	"sec-ch-ua-mobile":         "?0",
	"sec-fetch-dest":           "empty",
	"sec-fetch-mode":           "cors",
	"sec-fetch-site":           "same-origin",
	"x-client-timezone-offset": "28800",
}

var defaultSkipContainsPatterns = []string{
	"quasi_status",
	"elapsed_secs",
	"token_usage",
	"pending_fragment",
	"conversation_mode",
	"fragments/-1/status",
	"fragments/-2/status",
	"fragments/-3/status",
}

var defaultSkipExactPaths = []string{
	"response/search_status",
}

var ClientVersion string
var BaseHeaders = map[string]string{}
var SkipContainsPatterns = cloneStringSlice(defaultSkipContainsPatterns)
var SkipExactPathSet = toStringSet(defaultSkipExactPaths)

type clientConstants struct {
	Name            string `json:"name"`
	Platform        string `json:"platform"`
	Version         string `json:"version"`
	AndroidAPILevel string `json:"android_api_level"`
	Locale          string `json:"locale"`
}

type sharedConstants struct {
	Client              clientConstants   `json:"client"`
	BaseHeaders         map[string]string `json:"base_headers"`
	SkipContainsPattern []string          `json:"skip_contains_patterns"`
	SkipExactPaths      []string          `json:"skip_exact_paths"`
}

//go:embed constants_shared.json
var sharedConstantsJSON []byte

func init() {
	cfg := sharedConstants{}
	if err := json.Unmarshal(sharedConstantsJSON, &cfg); err != nil {
		panic(fmt.Errorf("load DeepSeek shared constants: %w", err))
	}
	applySharedConstants(cfg)
}

func applySharedConstants(cfg sharedConstants) {
	client := normalizeClientConstants(cfg.Client)
	ClientVersion = client.Version
	BaseHeaders = buildBaseHeaders(client, cfg.BaseHeaders)
	SkipContainsPatterns = cloneStringSlice(defaultSkipContainsPatterns)
	if len(cfg.SkipContainsPattern) > 0 {
		SkipContainsPatterns = cloneStringSlice(cfg.SkipContainsPattern)
	}
	SkipExactPathSet = toStringSet(defaultSkipExactPaths)
	if len(cfg.SkipExactPaths) > 0 {
		SkipExactPathSet = toStringSet(cfg.SkipExactPaths)
	}
}

func normalizeClientConstants(in clientConstants) clientConstants {
	if in.Name == "" {
		in.Name = "DeepSeek"
	}
	if in.Platform == "" {
		in.Platform = "web"
	}
	if in.AndroidAPILevel == "" {
		in.AndroidAPILevel = "35"
	}
	if in.Version == "" {
		in.Version = "2.0.0"
	}
	if in.Locale == "" {
		in.Locale = "zh_CN"
	}
	return in
}

func buildBaseHeaders(client clientConstants, overrides map[string]string) map[string]string {
	out := cloneStringMap(defaultStaticBaseHeaders)
	for k, v := range overrides {
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	if client.Platform == "android" {
		// Legacy Android UA path, retained for completeness / tests.
		if client.Name != "" && client.Version != "" {
			userAgent := client.Name + "/" + client.Version
			if client.AndroidAPILevel != "" {
				userAgent += " Android/" + client.AndroidAPILevel
			}
			out["User-Agent"] = userAgent
		}
	} else {
		// Web client: present a real browser UA. Honor an explicit override if
		// supplied, otherwise fall back to a recent Chrome desktop UA.
		if ua := strings.TrimSpace(out["User-Agent"]); ua == "" {
			out["User-Agent"] = DefaultWebUserAgent
		}
		if client.Version != "" {
			out["x-app-version"] = client.Version
		}
	}
	if client.Platform != "" {
		out["x-client-platform"] = client.Platform
	}
	if client.Version != "" {
		out["x-client-version"] = client.Version
	}
	if client.Locale != "" {
		out["x-client-locale"] = client.Locale
	}
	return out
}

// webUAVariants is a small pool of realistic Chrome desktop User-Agents kept
// for the legacy RandomBaseHeaders helper. New code should prefer
// BaseHeadersForFingerprint, which gives each account a stable browser
// identity instead of rotating per request (which is itself a signal).
var webUAVariants = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
}

// RandomBaseHeaders returns headers with a random Chrome User-Agent picked
// from the shared pool. This is kept for backwards compatibility; new call
// sites should use BaseHeadersForFingerprint(accountID) so each account
// presents a stable identity rather than rotating per request (which itself
// is a suspicious signal).
func RandomBaseHeaders() map[string]string {
	out := cloneStringMap(defaultStaticBaseHeaders)
	out["User-Agent"] = webUAVariants[rand.Intn(len(webUAVariants))]
	out["x-client-platform"] = "web"
	out["x-client-version"] = webClientVersion
	out["x-app-version"] = webClientVersion
	out["x-client-locale"] = "zh_CN"
	return out
}

// BaseHeadersForFingerprint builds a fresh header map keyed to the given
// account fingerprint. The same accountID always produces the same UA,
// sec-ch-ua, locale, cookie jar and device id so DeepSeek-side risk control
// sees a consistent browser per account, instead of values shuffling on every
// request.
//
// Callers must still set Authorization themselves; this function only fills
// in the browser-identity surface (User-Agent, sec-ch-ua, locale, cookie, ...).
func BaseHeadersForFingerprint(fp AccountFingerprint) map[string]string {
	out := cloneStringMap(defaultStaticBaseHeaders)
	if fp.UserAgent != "" {
		out["User-Agent"] = fp.UserAgent
	}
	platform := fp.Platform
	if platform == "" {
		platform = "web"
	}
	out["x-client-platform"] = platform
	version := fp.Version
	if version == "" {
		version = ClientVersion
	}
	if version != "" {
		out["x-client-version"] = version
		out["x-app-version"] = version
	}
	if fp.Locale != "" {
		out["x-client-locale"] = fp.Locale
	}
	if fp.AcceptLang != "" {
		out["Accept-Language"] = fp.AcceptLang
	}
	if fp.SecChUa != "" {
		out["sec-ch-ua"] = fp.SecChUa
	}
	if fp.SecChUaMobile != "" {
		out["sec-ch-ua-mobile"] = fp.SecChUaMobile
	}
	if fp.SecChUaPlatform != "" {
		out["sec-ch-ua-platform"] = fp.SecChUaPlatform
	}
	if fp.Cookie != "" {
		out["Cookie"] = fp.Cookie
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringSlice(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func toStringSet(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		out[v] = struct{}{}
	}
	return out
}

const (
	KeepAliveTimeout  = 5
	StreamIdleTimeout = 300
	MaxKeepaliveCount = 40
)
