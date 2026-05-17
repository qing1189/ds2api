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

// defaultStaticBaseHeaders are the headers that real DeepSeek Android clients
// (OkHttp-based) consistently send. We deliberately omit "accept-charset"
// because modern Android OkHttp does not send it, and including it makes the
// request look like a Python/curl client.
var defaultStaticBaseHeaders = map[string]string{
	"Host":            "chat.deepseek.com",
	"Accept":          "application/json",
	"Content-Type":    "application/json",
	"Accept-Encoding": "gzip, deflate, br",
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
		in.Platform = "android"
	}
	if in.AndroidAPILevel == "" {
		in.AndroidAPILevel = "35"
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
	if client.Name != "" && client.Version != "" {
		userAgent := client.Name + "/" + client.Version
		if client.Platform == "android" && client.AndroidAPILevel != "" {
			userAgent += " Android/" + client.AndroidAPILevel
		}
		out["User-Agent"] = userAgent
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

// uaVariants is kept for backwards compatibility with any callers that need a
// quick rotation pool independent from a specific account. New code should
// prefer BaseHeadersForFingerprint, which gives each account a stable identity.
var uaVariants = func() []string {
	platforms := []string{"android"}
	locales := []string{"zh_CN", "en_US", "zh_TW"}
	uaCombos := []struct {
		name  string
		ver   string
		apiLv string
	}{
		{"DeepSeek", "2.0.4", "35"}, // 基线版本
		{"DeepSeek", "2.0.3", "34"},
		{"DeepSeek", "2.1.0", "35"},
		{"DeepSeek", "2.1.1", "35"},
		{"DeepSeek", "2.0.5", "34"},
		{"DeepSeek", "2.2.0", "36"},
	}
	seen := map[string]bool{}
	var result []string
	for _, combo := range uaCombos {
		for _, plat := range platforms {
			for _, loc := range locales {
				ua := combo.name + "/" + combo.ver + " Android/" + combo.apiLv
				key := ua + "|" + plat + "|" + loc
				if seen[key] {
					continue
				}
				seen[key] = true
				result = append(result, ua+"|"+plat+"|"+loc+"|"+combo.ver)
			}
		}
	}
	return result
}()

// RandomBaseHeaders returns headers with a random User-Agent picked from the
// shared pool. This is kept for backwards compatibility; new call sites
// should use BaseHeadersForFingerprint(accountID) so each account presents a
// stable identity rather than rotating per request (which itself is a
// suspicious signal).
func RandomBaseHeaders() map[string]string {
	out := cloneStringMap(defaultStaticBaseHeaders)
	variant := uaVariants[rand.Intn(len(uaVariants))]
	parts := strings.Split(variant, "|")
	if len(parts) == 4 {
		out["User-Agent"] = parts[0]
		out["x-client-platform"] = parts[1]
		out["x-client-locale"] = parts[2]
		out["x-client-version"] = parts[3]
	}
	return out
}

// BaseHeadersForFingerprint builds a fresh header map keyed to the given
// account fingerprint. The same accountID always produces the same UA,
// locale, x-app-build and x-device-id so DeepSeek-side risk control sees a
// consistent device per account, instead of values shuffling on every request.
//
// Callers must still set Authorization themselves; this function only fills
// in the device-identity surface (User-Agent, locale, build, device id, etc).
func BaseHeadersForFingerprint(fp AccountFingerprint) map[string]string {
	out := cloneStringMap(defaultStaticBaseHeaders)
	if fp.UserAgent != "" {
		out["User-Agent"] = fp.UserAgent
	}
	if fp.Platform != "" {
		out["x-client-platform"] = fp.Platform
	}
	if fp.Version != "" {
		out["x-client-version"] = fp.Version
	}
	if fp.Locale != "" {
		out["x-client-locale"] = fp.Locale
	}
	if fp.AcceptLang != "" {
		out["Accept-Language"] = fp.AcceptLang
	}
	if fp.BuildNumber != "" {
		out["x-app-build"] = fp.BuildNumber
	}
	if fp.APILevel != "" {
		out["x-os-version"] = fp.APILevel
	}
	if fp.DeviceID != "" {
		out["x-device-id"] = fp.DeviceID
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
