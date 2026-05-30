package config

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Identifier returns the stable key used across the account pool, session
// cache, device fingerprint and admin APIs. Email and mobile take precedence;
// a token-only (direct-token) account falls back to a deterministic id derived
// from the token so it can still be addressed everywhere without leaking the
// raw secret.
func (a Account) Identifier() string {
	if strings.TrimSpace(a.Email) != "" {
		return strings.TrimSpace(a.Email)
	}
	if mobile := NormalizeMobileForStorage(a.Mobile); mobile != "" {
		return mobile
	}
	if token := strings.TrimSpace(a.Token); token != "" {
		return TokenAccountIdentifier(token)
	}
	return ""
}

// IsDirectToken reports whether the account is a "direct-token" account: it
// carries a DeepSeek session token but has no email/mobile credentials to log
// in with. Such accounts use the token directly as the upstream Bearer and are
// never re-logged-in, refreshed, or token-stripped on persistence.
func (a Account) IsDirectToken() bool {
	if strings.TrimSpace(a.Email) != "" {
		return false
	}
	if NormalizeMobileForStorage(a.Mobile) != "" {
		return false
	}
	return strings.TrimSpace(a.Token) != ""
}

// TokenAccountIdentifier derives a stable, non-secret identifier for a
// token-only account. The token is hashed so the raw secret never appears in
// logs, queue ids, or the admin UI, while the id stays stable for a given
// token.
func TokenAccountIdentifier(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return "token:" + hex.EncodeToString(sum[:6])
}
