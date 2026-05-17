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
	if a.UserAgent == "" || a.DeviceID == "" || a.Version == "" {
		t.Fatalf("fingerprint missing required fields: %+v", a)
	}
	if !strings.HasPrefix(a.UserAgent, "DeepSeek/") {
		t.Fatalf("unexpected UA: %q", a.UserAgent)
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
		"x-client-locale",
		"Accept-Language",
		"x-app-build",
		"x-os-version",
		"x-device-id",
		"Accept-Encoding",
		"Content-Type",
		"Host",
	}
	for _, k := range required {
		if v, ok := h[k]; !ok || strings.TrimSpace(v) == "" {
			t.Fatalf("expected header %q to be set, got %q (full=%v)", k, v, h)
		}
	}
	if _, ok := h["accept-charset"]; ok {
		t.Fatalf("accept-charset header should not be sent (modern Android OkHttp omits it)")
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
