package protocol

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// webClientVersion is the value reported in x-client-version / x-app-version.
// The reference web client (verified to avoid DeepSeek risk control) reports
// "2.0.0" for both, so we keep them aligned.
const webClientVersion = "2.0.0"

// AccountFingerprint captures the browser-like identity that we keep stable
// across the lifetime of a single DeepSeek account. The real DeepSeek web
// client sends a consistent (UA, sec-ch-ua, cookie jar) on every request;
// rotating any of those mid-session is itself a strong risk-control signal,
// so we derive each field deterministically from the account id and only
// refresh when the operator explicitly rotates the fingerprint.
//
// This mirrors the conversation scheme of the reference project ds2026530:
// a Chrome desktop browser session with a per-account cookie jar (smidV2,
// HWWAFSESTIME, HWWAFSESID, ds_session_id, .thumbcache) plus a base64 device
// id used at login time.
type AccountFingerprint struct {
	AccountID       string
	UserAgent       string
	Platform        string // always "web"
	Version         string // x-client-version / x-app-version, e.g. "2.0.0"
	Locale          string
	AcceptLang      string
	SecChUa         string // e.g. "Not_A Brand";v="8", "Chromium";v="120", ...
	SecChUaMobile   string // "?0" for desktop
	SecChUaPlatform string // e.g. "Windows"
	Cookie          string // full Cookie header value (per-account, stable)
	DeviceID        string // base64 device id reused for login + thumbcache
}

// webProfile describes one realistic Chrome desktop client variant. We pin
// sec-ch-ua to the major version so the (UA, sec-ch-ua) tuple stays internally
// consistent. Rotation is per-account, not per-request, to mimic real clients.
type webProfile struct {
	chromeMajor string
	chromeFull  string
	osUA        string
	secPlatform string
}

// webProfiles is intentionally small and conservative: every entry is a
// plausible recent Chrome desktop build. We bias toward Windows because that
// is the dominant DeepSeek web userbase.
var webProfiles = []webProfile{
	{chromeMajor: "120", chromeFull: "120.0.0.0", osUA: "Windows NT 10.0; Win64; x64", secPlatform: `"Windows"`},
	{chromeMajor: "121", chromeFull: "121.0.0.0", osUA: "Windows NT 10.0; Win64; x64", secPlatform: `"Windows"`},
	{chromeMajor: "122", chromeFull: "122.0.0.0", osUA: "Windows NT 10.0; Win64; x64", secPlatform: `"Windows"`},
	{chromeMajor: "123", chromeFull: "123.0.0.0", osUA: "Macintosh; Intel Mac OS X 10_15_7", secPlatform: `"macOS"`},
	{chromeMajor: "119", chromeFull: "119.0.0.0", osUA: "X11; Linux x86_64", secPlatform: `"Linux"`},
}

// localeMix associates a UI locale with the matching Accept-Language string.
// We bias zh_CN heavier because that's still the dominant DeepSeek userbase,
// but include fallbacks so a small slice of accounts looks like overseas
// users.
var localeMix = []struct {
	locale string
	accept string
	weight int
}{
	{locale: "zh_CN", accept: "zh-CN,zh;q=0.9", weight: 75},
	{locale: "zh_TW", accept: "zh-TW,zh;q=0.9,en;q=0.6", weight: 5},
	{locale: "zh_HK", accept: "zh-HK,zh;q=0.9,en;q=0.6", weight: 5},
	{locale: "en_US", accept: "en-US,en;q=0.9", weight: 10},
	{locale: "en_GB", accept: "en-GB,en;q=0.9", weight: 5},
}

var (
	fingerprintMu    sync.RWMutex
	fingerprintCache = map[string]AccountFingerprint{}

	// fingerprintSalt is mixed into the deterministic hash so two ds2api
	// deployments don't end up with identical (account → fingerprint)
	// mappings even when they share the same account list. It is randomized
	// on process start unless an explicit salt is supplied via
	// SetFingerprintSalt or the DS2API_FINGERPRINT_SALT env variable.
	fingerprintSalt []byte
)

// SetFingerprintSalt overrides the auto-generated salt. Useful when the
// operator wants reproducible fingerprints across restarts; leave unset for
// per-process random salt (default behavior). Calling this also flushes the
// fingerprint cache so the next request from each account picks up the new
// device identity.
func SetFingerprintSalt(s string) {
	fingerprintMu.Lock()
	defer fingerprintMu.Unlock()
	fingerprintSalt = []byte(s)
	fingerprintCache = map[string]AccountFingerprint{}
}

func ensureFingerprintSalt() []byte {
	fingerprintMu.RLock()
	if len(fingerprintSalt) > 0 {
		out := make([]byte, len(fingerprintSalt))
		copy(out, fingerprintSalt)
		fingerprintMu.RUnlock()
		return out
	}
	fingerprintMu.RUnlock()

	fingerprintMu.Lock()
	defer fingerprintMu.Unlock()
	if len(fingerprintSalt) == 0 {
		buf := make([]byte, 16)
		// math/rand is fine here: salt only needs to be unpredictable to
		// outside observers, not cryptographically secure.
		rand.Read(buf) //nolint:gosec
		fingerprintSalt = buf
	}
	out := make([]byte, len(fingerprintSalt))
	copy(out, fingerprintSalt)
	return out
}

// FingerprintForAccount returns the deterministic (UA, locale, cookie, ...)
// tuple for an account. The first call for any given accountID computes and
// caches the value; later calls return the cached fingerprint so a single
// account always presents the same identity to upstream until the cache is
// reset.
func FingerprintForAccount(accountID string) AccountFingerprint {
	id := strings.TrimSpace(accountID)
	if id == "" {
		// Fallback for unknown / direct-token usage: still fake a consistent
		// random fingerprint per process.
		return randomFingerprint()
	}

	fingerprintMu.RLock()
	cached, ok := fingerprintCache[id]
	fingerprintMu.RUnlock()
	if ok {
		return cached
	}

	fp := deriveFingerprint(id)

	fingerprintMu.Lock()
	defer fingerprintMu.Unlock()
	if existing, ok := fingerprintCache[id]; ok {
		return existing
	}
	fingerprintCache[id] = fp
	return fp
}

// ResetFingerprintCache clears all cached fingerprints. Operators can call
// this when they want every account to "rotate" devices on the next request,
// e.g. after a suspected ban wave.
func ResetFingerprintCache() {
	fingerprintMu.Lock()
	defer fingerprintMu.Unlock()
	fingerprintCache = map[string]AccountFingerprint{}
}

// deriveFingerprint hashes the account id with the per-process salt and uses
// the resulting bytes as a deterministic source for picking the Chrome
// profile / locale and for building a stable cookie jar + device id.
func deriveFingerprint(accountID string) AccountFingerprint {
	salt := ensureFingerprintSalt()
	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(accountID))
	digest := h.Sum(nil)

	prof := webProfiles[bigEndianUint32(digest[0:4])%uint32(len(webProfiles))]
	loc := pickLocale(digest[4:8])
	cookie, deviceID := buildWebIdentity(salt, accountID)

	return AccountFingerprint{
		AccountID:       accountID,
		UserAgent:       buildChromeUA(prof),
		Platform:        "web",
		Version:         webClientVersion,
		Locale:          loc.locale,
		AcceptLang:      loc.accept,
		SecChUa:         buildSecChUa(prof.chromeMajor),
		SecChUaMobile:   "?0",
		SecChUaPlatform: prof.secPlatform,
		Cookie:          cookie,
		DeviceID:        deviceID,
	}
}

// randomFingerprint is used when accountID is empty (e.g. direct token mode).
// It still picks one consistent profile so a single request looks coherent.
func randomFingerprint() AccountFingerprint {
	prof := webProfiles[rand.Intn(len(webProfiles))]
	loc := localeMix[rand.Intn(len(localeMix))]
	seed := fmt.Sprintf("anon-%d-%d", rand.Int63(), rand.Int63()) //nolint:gosec
	salt := ensureFingerprintSalt()
	cookie, deviceID := buildWebIdentity(salt, seed)

	return AccountFingerprint{
		UserAgent:       buildChromeUA(prof),
		Platform:        "web",
		Version:         webClientVersion,
		Locale:          loc.locale,
		AcceptLang:      loc.accept,
		SecChUa:         buildSecChUa(prof.chromeMajor),
		SecChUaMobile:   "?0",
		SecChUaPlatform: prof.secPlatform,
		Cookie:          cookie,
		DeviceID:        deviceID,
	}
}

func buildChromeUA(prof webProfile) string {
	return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", prof.osUA, prof.chromeFull)
}

func buildSecChUa(major string) string {
	return fmt.Sprintf(`"Not_A Brand";v="8", "Chromium";v="%s", "Google Chrome";v="%s"`, major, major)
}

// buildWebIdentity derives a stable-per-account cookie jar and device id,
// mirroring the cookie shape the reference web client generates:
//
//	smidV2=<datePrefix><alnum10><hex24>;
//	HWWAFSESTIME=<epoch_ms>;
//	HWWAFSESID=<alnum4><hex12>;
//	ds_session_id=<hex32>;
//	.thumbcache_<hex32>=<url-encoded base64 device id>
//
// All components are deterministic so the same account always presents the
// same browser session until the fingerprint salt changes.
func buildWebIdentity(salt []byte, accountID string) (cookie string, deviceID string) {
	smid := "20260520" + alphaNumFromBytes(derive(salt, accountID, "smid-an", 10), 10) +
		hexFromBytes(derive(salt, accountID, "smid-hex", 12), 24)
	hwTime := webTimestamp(derive(salt, accountID, "hwtime", 8))
	hwSesID := alphaNumFromBytes(derive(salt, accountID, "hwsesid-an", 4), 4) +
		hexFromBytes(derive(salt, accountID, "hwsesid-hex", 6), 12)
	dsSessionID := hexFromBytes(derive(salt, accountID, "ds-session", 16), 32)
	thumbKey := hexFromBytes(derive(salt, accountID, "thumb-key", 16), 32)
	deviceID = webDeviceID(derive(salt, accountID, "device-id", 48))

	cookie = fmt.Sprintf("smidV2=%s; HWWAFSESTIME=%s; HWWAFSESID=%s; ds_session_id=%s; .thumbcache_%s=%s",
		smid, hwTime, hwSesID, dsSessionID, thumbKey, url.QueryEscape(deviceID))
	return cookie, deviceID
}

// derive returns n deterministic bytes from (salt, accountID, label) using a
// counter-extended SHA-256 keystream.
func derive(salt []byte, accountID, label string, n int) []byte {
	out := make([]byte, 0, ((n/sha256.Size)+1)*sha256.Size)
	var counter uint32
	for len(out) < n {
		h := sha256.New()
		h.Write(salt)
		h.Write([]byte(accountID))
		h.Write([]byte(label))
		var cb [4]byte
		binary.BigEndian.PutUint32(cb[:], counter)
		h.Write(cb[:])
		out = append(out, h.Sum(nil)...)
		counter++
	}
	return out[:n]
}

// hexFromBytes returns the first n lowercase hex characters derived from b.
func hexFromBytes(b []byte, n int) string {
	s := hex.EncodeToString(b)
	if len(s) < n {
		s += strings.Repeat("0", n-len(s))
	}
	return s[:n]
}

// alphaNumFromBytes maps bytes to a lowercase alphanumeric string of length n.
func alphaNumFromBytes(b []byte, n int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	if len(b) == 0 {
		b = []byte{0}
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = charset[int(b[i%len(b)])%len(charset)]
	}
	return string(out)
}

// webDeviceID builds a base64 device id matching the reference format:
// base64(48 bytes) with padding stripped, then a literal "==" suffix.
func webDeviceID(b []byte) string {
	return base64.RawStdEncoding.EncodeToString(b) + "=="
}

// webTimestamp returns a plausible, stable 13-digit epoch-millisecond string
// (the HWWAFSESTIME cookie). It is anchored at 2025-01-01 plus a deterministic
// offset within roughly a year so the value looks like a recent session time.
func webTimestamp(b []byte) string {
	const baseMs = int64(1735689600000) // 2025-01-01T00:00:00Z in ms
	const windowMs = int64(300 * 24 * 60 * 60 * 1000)
	var v uint64
	if len(b) >= 8 {
		v = binary.BigEndian.Uint64(b[:8])
	} else {
		buf := make([]byte, 8)
		copy(buf, b)
		v = binary.BigEndian.Uint64(buf)
	}
	offset := int64(v % uint64(windowMs))
	return strconv.FormatInt(baseMs+offset, 10)
}

// pickLocale walks the localeMix in declared order using the digest as a
// stable random seed, weighted by the entries' weight field.
func pickLocale(digest []byte) struct {
	locale string
	accept string
} {
	total := 0
	for _, l := range localeMix {
		total += l.weight
	}
	if total <= 0 {
		first := localeMix[0]
		return struct {
			locale string
			accept string
		}{first.locale, first.accept}
	}
	r := int(bigEndianUint32(digest)) % total
	cumulative := 0
	for _, l := range localeMix {
		cumulative += l.weight
		if r < cumulative {
			return struct {
				locale string
				accept string
			}{l.locale, l.accept}
		}
	}
	first := localeMix[0]
	return struct {
		locale string
		accept string
	}{first.locale, first.accept}
}

// bigEndianUint32 converts the first 4 bytes of b into an unsigned int.
func bigEndianUint32(b []byte) uint32 {
	if len(b) < 4 {
		buf := make([]byte, 4)
		copy(buf, b)
		b = buf
	}
	return binary.BigEndian.Uint32(b[:4])
}
