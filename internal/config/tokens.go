package config

import (
	"os"
	"strings"
)

// Env vars that let operators paste DeepSeek session tokens directly, without
// providing email/password. This mirrors the reference project's
// DS_TOKENS=token1,token2 convenience. DS2API_TOKENS takes a comma- or
// newline-separated list; DS2API_TOKEN is a single-token fallback.
const (
	EnvDirectTokens = "DS2API_TOKENS"
	EnvDirectToken  = "DS2API_TOKEN"
)

// LoadDirectTokensFromEnv reads DS2API_TOKENS / DS2API_TOKEN and returns the
// de-duplicated list of tokens, in declared order.
func LoadDirectTokensFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv(EnvDirectTokens))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv(EnvDirectToken))
	}
	return ParseDirectTokens(raw)
}

// ParseDirectTokens splits a comma/newline/semicolon-separated token blob into
// a clean, de-duplicated list. Blank entries are ignored.
func ParseDirectTokens(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ';'
	})
	out := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		token := strings.TrimSpace(f)
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MergeDirectTokens appends direct-token accounts for any env-provided tokens
// that are not already present in the config (matched by token value, then by
// derived identifier). It is a no-op when no tokens are configured.
func (c *Config) MergeDirectTokens(tokens []string) {
	if c == nil || len(tokens) == 0 {
		return
	}
	existingTokens := make(map[string]struct{}, len(c.Accounts))
	existingIDs := make(map[string]struct{}, len(c.Accounts))
	for _, acc := range c.Accounts {
		if tok := strings.TrimSpace(acc.Token); tok != "" {
			existingTokens[tok] = struct{}{}
		}
		if id := acc.Identifier(); id != "" {
			existingIDs[id] = struct{}{}
		}
	}
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if _, ok := existingTokens[token]; ok {
			continue
		}
		id := TokenAccountIdentifier(token)
		if _, ok := existingIDs[id]; ok {
			continue
		}
		c.Accounts = append(c.Accounts, Account{Token: token})
		existingTokens[token] = struct{}{}
		existingIDs[id] = struct{}{}
	}
}
