package protocol

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
	"sync"
)

// AccountFingerprint captures the device-like identity that we want to keep
// stable across the lifetime of a single DeepSeek account. Real Android
// clients send a consistent (UA, locale, x-app-build, x-device-id) on every
// request; rotating any of those mid-session is itself a strong risk-control
// signal, so we derive each field deterministically from the account id and
// only refresh when the operator explicitly rotates the fingerprint.
type AccountFingerprint struct {
	AccountID    string
	UserAgent    string
	Platform     string
	Version      string
	BuildNumber  string
	APILevel     string
	Locale       string
	AcceptLang   string
	DeviceID     string
	Manufacturer string
}

// uaProfile describes one realistic DeepSeek-on-Android client variant.
type uaProfile struct {
	version     string
	buildNumber string
	apiLevel    string
	deviceModel string
}

// uaProfiles is intentionally small and conservative: every entry below has
// been seen on a real DeepSeek Android client at some point. We pin the build
// number to the version so the (UA, x-app-build) tuple stays internally
// consistent. Rotation is per-account, not per-request, to mimic real clients.
var uaProfiles = []uaProfile{
	{version: "2.0.4", buildNumber: "20240817", apiLevel: "35", deviceModel: "Pixel 8"},
	{version: "2.0.5", buildNumber: "20240901", apiLevel: "34", deviceModel: "Mi 11"},
	{version: "2.0.6", buildNumber: "20240915", apiLevel: "34", deviceModel: "OnePlus 9"},
	{version: "2.1.0", buildNumber: "20241015", apiLevel: "35", deviceModel: "HUAWEI Mate 50"},
	{version: "2.1.1", buildNumber: "20241108", apiLevel: "35", deviceModel: "OPPO Find X7"},
	{version: "2.1.2", buildNumber: "20241201", apiLevel: "35", deviceModel: "vivo X100"},
	{version: "2.2.0", buildNumber: "20250112", apiLevel: "36", deviceModel: "Pixel 9"},
}

// localeMix associates a UI locale with the matching Accept-Language string.
// We bias zh_CN heavier because that's still the dominant DeepSeek mobile
// userbase, but include fallbacks so a small slice of accounts looks like
// overseas users.
var localeMix = []struct {
	locale string
	accept string
	weight int
}{
	{locale: "zh_CN", accept: "zh-CN,zh;q=0.9,en;q=0.6", weight: 70},
	{locale: "zh_TW", accept: "zh-TW,zh;q=0.9,en;q=0.6", weight: 5},
	{locale: "zh_HK", accept: "zh-HK,zh;q=0.9,en;q=0.6", weight: 5},
	{locale: "en_US", accept: "en-US,en;q=0.9", weight: 15},
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

// FingerprintForAccount returns the deterministic (UA, locale, device_id, ...)
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
// the resulting bytes as a deterministic source for picking UA / locale /
// device_id components.
func deriveFingerprint(accountID string) AccountFingerprint {
	salt := ensureFingerprintSalt()
	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(accountID))
	digest := h.Sum(nil)

	prof := uaProfiles[bigEndianUint32(digest[0:4])%uint32(len(uaProfiles))]
	loc := pickLocale(digest[4:8])
	deviceID := buildDeviceID(digest)

	ua := fmt.Sprintf("DeepSeek/%s Android/%s", prof.version, prof.apiLevel)

	return AccountFingerprint{
		AccountID:    accountID,
		UserAgent:    ua,
		Platform:     "android",
		Version:      prof.version,
		BuildNumber:  prof.buildNumber,
		APILevel:     prof.apiLevel,
		Locale:       loc.locale,
		AcceptLang:   loc.accept,
		DeviceID:     deviceID,
		Manufacturer: prof.deviceModel,
	}
}

// randomFingerprint is used when accountID is empty (e.g. direct token mode).
// It still picks one consistent profile so a single request looks coherent.
func randomFingerprint() AccountFingerprint {
	prof := uaProfiles[rand.Intn(len(uaProfiles))]
	loc := localeMix[rand.Intn(len(localeMix))]
	salt := ensureFingerprintSalt()
	digest := sha256.Sum256(append(salt, randomBytes(8)...))

	return AccountFingerprint{
		UserAgent:    fmt.Sprintf("DeepSeek/%s Android/%s", prof.version, prof.apiLevel),
		Platform:     "android",
		Version:      prof.version,
		BuildNumber:  prof.buildNumber,
		APILevel:     prof.apiLevel,
		Locale:       loc.locale,
		AcceptLang:   loc.accept,
		DeviceID:     buildDeviceID(digest[:]),
		Manufacturer: prof.deviceModel,
	}
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

// buildDeviceID builds a stable 24-char-ish device id that mimics the format
// real Android clients send (lower-case hex). It uses bytes from the digest
// so the same account always produces the same device id.
func buildDeviceID(digest []byte) string {
	if len(digest) < 12 {
		buf := make([]byte, 12)
		copy(buf, digest)
		digest = buf
	}
	return hex.EncodeToString(digest[:12])
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

func randomBytes(n int) []byte {
	out := make([]byte, n)
	rand.Read(out) //nolint:gosec
	return out
}
