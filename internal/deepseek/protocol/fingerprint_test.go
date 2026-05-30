package protocol

import (
	"strings"
	"testing"
)

func TestFingerprintForAccountIsStable(t *testing.T) {
	ResetFingerprintCache()
	SetFingerprintSalt("test-salt")
	a := FingerprintForAccount("u@example.com")
	b := FingerprintForAccount("u@example.com")
	if a != b {
		t.Fatalf("expected stable fingerprint per account, got\n  a=%+v\n  b=%+v", a, b)
	}
	if a.UserAgent == "" || a.DeviceID == "" || a.Version == "" || a.Cookie == "" {
		t.Fatalf("fingerprint missing required fields: %+v", a)
	}
	if !strings.HasPrefix(a.UserAgent, "Mozilla/") {
		t.Fatalf("unexpected UA: %q", a.UserAgent)
	}
	if a.Platform != "web" {
		t.Fatalf("unexpected platform: %q", a.Platform)
	}
	if !strings.Contains(a.Cookie, "smidV2=") || !strings.Contains(a.Cookie, "ds_session_id=") {
		t.Fatalf("cookie jar missing expected components: %q", a.Cookie)
	}
}

func TestFingerprintForAccountDiffersByID(t *testing.T) {
	ResetFingerprintCache()
	SetFingerprintSalt("test-salt")
	a := FingerprintForAccount("a@example.com")
	b := FingerprintForAccount("b@example.com")
	// At least one component must differ. We don't assert all because two
	// accounts could reasonably share a UA pool slot but have different
	// device IDs.
	if a.DeviceID == b.DeviceID {
		t.Fatalf("expected different device id for different accounts, got %q", a.DeviceID)
	}
}

func TestBaseHeadersForFingerprintFillsExpectedFields(t *testing.T) {
	ResetFingerprintCache()
	SetFingerprintSalt("test-salt")
	fp := FingerprintForAccount("u@example.com")
	h := BaseHeadersForFingerprint(fp)
	required := []string{
		"User-Agent",
		"x-client-platform",
		"x-client-version",
		"x-app-version",
		"x-client-locale",
		"Accept-Language",
		"sec-ch-ua",
		"sec-ch-ua-mobile",
		"sec-ch-ua-platform",
		"Cookie",
		"Origin",
		"Referer",
		"Accept-Encoding",
		"Content-Type",
		"Host",
	}
	for _, k := range required {
		if v, ok := h[k]; !ok || strings.TrimSpace(v) == "" {
			t.Fatalf("expected header %q to be set, got %q (full=%v)", k, v, h)
		}
	}
	if h["x-client-platform"] != "web" {
		t.Fatalf("expected web platform, got %q", h["x-client-platform"])
	}
	if _, ok := h["accept-charset"]; ok {
		t.Fatalf("accept-charset header should not be sent (browsers omit it)")
	}
	if _, ok := h["x-trace-id"]; ok {
		t.Fatalf("x-trace-id should not be sent (web client does not send it)")
	}
}

func TestResetFingerprintCacheChangesIdentityWhenSaltDiffers(t *testing.T) {
	SetFingerprintSalt("salt-a")
	a := FingerprintForAccount("u@example.com")
	SetFingerprintSalt("salt-b")
	b := FingerprintForAccount("u@example.com")
	if a == b {
		t.Fatalf("expected different fingerprints with different salts")
	}
}
