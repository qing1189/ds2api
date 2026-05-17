package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"ds2api/internal/account"
	"ds2api/internal/config"
)

type ctxKey string

const authCtxKey ctxKey = "auth_context"

var (
	ErrUnauthorized = errors.New("unauthorized: missing auth token")
	ErrNoAccount    = errors.New("no accounts configured or all accounts are busy")
)

type RequestAuth struct {
	UseConfigToken bool
	DeepSeekToken  string
	CallerID       string
	APIKey         string
	AccountID      string
	TargetAccount  string
	Account        config.Account
	TriedAccounts  map[string]bool
	resolver       *Resolver

	requestOutcomeSet bool
	requestSuccess    bool
	usageInputTokens  int
	usageOutputTokens int
}

type LoginFunc func(ctx context.Context, acc config.Account) (string, error)

type Resolver struct {
	Store *config.Store
	Pool  *account.Pool
	Login LoginFunc

	mu               sync.Mutex
	tokenRefreshedAt map[string]time.Time
}

func NewResolver(store *config.Store, pool *account.Pool, login LoginFunc) *Resolver {
	return &Resolver{
		Store:            store,
		Pool:             pool,
		Login:            login,
		tokenRefreshedAt: map[string]time.Time{},
	}
}

func (r *Resolver) Determine(req *http.Request) (*RequestAuth, error) {
	callerKey := extractCallerToken(req)
	if callerKey == "" {
		return nil, ErrUnauthorized
	}
	callerID := callerTokenID(callerKey)
	ctx := req.Context()
	if !r.Store.HasAPIKey(callerKey) {
		return &RequestAuth{
			UseConfigToken: false,
			DeepSeekToken:  callerKey,
			CallerID:       callerID,
			resolver:       r,
			TriedAccounts:  map[string]bool{},
		}, nil
	}
	target := strings.TrimSpace(req.Header.Get("X-Ds2-Target-Account"))
	a, err := r.acquireManagedRequestAuth(ctx, callerID, target)
	if err != nil {
		return nil, err
	}
	a.APIKey = callerKey
	return a, nil
}

func (r *Resolver) acquireManagedRequestAuth(ctx context.Context, callerID, target string) (*RequestAuth, error) {
	tried := map[string]bool{}
	var lastEnsureErr error
	for {
		if target == "" && len(tried) >= len(r.Store.Accounts()) {
			if lastEnsureErr != nil {
				return nil, lastEnsureErr
			}
			return nil, ErrNoAccount
		}
		acc, ok := r.Pool.AcquireWait(ctx, target, tried)
		if !ok {
			if lastEnsureErr != nil {
				return nil, lastEnsureErr
			}
			return nil, ErrNoAccount
		}

		a := &RequestAuth{
			UseConfigToken: true,
			CallerID:       callerID,
			AccountID:      acc.Identifier(),
			TargetAccount:  target,
			Account:        acc,
			TriedAccounts:  tried,
			resolver:       r,
		}

		if err := r.ensureManagedToken(ctx, a); err != nil {
			lastEnsureErr = err
			tried[a.AccountID] = true
			r.Pool.Release(a.AccountID)
			r.ReportRequestFailure(a.AccountID)
			if target != "" {
				return nil, err
			}
			continue
		}
		return a, nil
	}
}

// DetermineCaller resolves caller identity without acquiring any pooled account.
// Use this for local-cache lookup routes that only need tenant isolation.
func (r *Resolver) DetermineCaller(req *http.Request) (*RequestAuth, error) {
	callerKey := extractCallerToken(req)
	if callerKey == "" {
		return nil, ErrUnauthorized
	}
	callerID := callerTokenID(callerKey)
	a := &RequestAuth{
		UseConfigToken: false,
		CallerID:       callerID,
		resolver:       r,
		TriedAccounts:  map[string]bool{},
	}
	if r == nil || r.Store == nil || !r.Store.HasAPIKey(callerKey) {
		a.DeepSeekToken = callerKey
	}
	return a, nil
}

func WithAuth(ctx context.Context, a *RequestAuth) context.Context {
	return context.WithValue(ctx, authCtxKey, a)
}

func FromContext(ctx context.Context) (*RequestAuth, bool) {
	v := ctx.Value(authCtxKey)
	a, ok := v.(*RequestAuth)
	return a, ok
}

func (r *Resolver) loginAndPersist(ctx context.Context, a *RequestAuth) error {
	token, err := r.Login(ctx, a.Account)
	if err != nil {
		return err
	}
	a.Account.Token = token
	a.DeepSeekToken = token
	r.markTokenRefreshedNow(a.AccountID)
	return r.Store.UpdateAccountToken(a.AccountID, token)
}

func (r *Resolver) RefreshToken(ctx context.Context, a *RequestAuth) bool {
	if !a.UseConfigToken || a.AccountID == "" {
		return false
	}
	_ = r.Store.UpdateAccountToken(a.AccountID, "")
	a.Account.Token = ""
	if err := r.loginAndPersist(ctx, a); err != nil {
		config.Logger.Error("[refresh_token] failed", "account", a.AccountID, "error", err)
		return false
	}
	return true
}

func (r *Resolver) MarkTokenInvalid(a *RequestAuth) {
	if !a.UseConfigToken || a.AccountID == "" {
		return
	}
	a.Account.Token = ""
	a.DeepSeekToken = ""
	r.clearTokenRefreshMark(a.AccountID)
	_ = r.Store.UpdateAccountToken(a.AccountID, "")
	r.ReportRequestFailure(a.AccountID)
}

func (r *Resolver) SwitchAccount(ctx context.Context, a *RequestAuth) bool {
	if !a.UseConfigToken {
		return false
	}
	if strings.TrimSpace(a.TargetAccount) != "" {
		return false
	}
	if a.TriedAccounts == nil {
		a.TriedAccounts = map[string]bool{}
	}
	if a.AccountID != "" {
		a.TriedAccounts[a.AccountID] = true
		r.Pool.Release(a.AccountID)
	}
	for {
		acc, ok := r.Pool.Acquire("", a.TriedAccounts)
		if !ok {
			return false
		}
		a.Account = acc
		a.AccountID = acc.Identifier()
		if err := r.ensureManagedToken(ctx, a); err != nil {
			a.TriedAccounts[a.AccountID] = true
			r.Pool.Release(a.AccountID)
			continue
		}
		return true
	}
}

func (a *RequestAuth) SwitchAccount(ctx context.Context) bool {
	if a == nil || a.resolver == nil {
		return false
	}
	return a.resolver.SwitchAccount(ctx, a)
}

func (r *Resolver) Release(a *RequestAuth) {
	if a == nil || !a.UseConfigToken || a.AccountID == "" {
		return
	}
	// Report outcome if the handler explicitly marked it.
	if a.requestOutcomeSet {
		if a.requestSuccess {
			r.ReportRequestSuccess(a.AccountID)
			r.Pool.RecordRequestSuccess(a.AccountID, a.APIKey, a.usageInputTokens, a.usageOutputTokens)
		} else {
			r.ReportRequestFailure(a.AccountID)
			r.Pool.RecordRequestFailure(a.AccountID, a.APIKey)
		}
	}
	r.Pool.Release(a.AccountID)
}

// ReportRequestSuccess records a successful request for weighted account selection.
func (r *Resolver) ReportRequestSuccess(accountID string) {
	if accountID == "" {
		return
	}
	r.Pool.ReportAccountSuccess(accountID)
}

// ReportRequestFailure records a failed request for weighted account selection.
func (r *Resolver) ReportRequestFailure(accountID string) {
	if accountID == "" {
		return
	}
	r.Pool.ReportAccountFailure(accountID)
}

func (a *RequestAuth) ReportSuccess() {
	if a == nil || a.resolver == nil {
		return
	}
	a.resolver.ReportRequestSuccess(a.AccountID)
}

func (a *RequestAuth) ReportFailure() {
	if a == nil || a.resolver == nil {
		return
	}
	a.resolver.ReportRequestFailure(a.AccountID)
}

// MarkOutcome marks the request as successful or failed after completion.
// The outcome is reported when the account is released. If MarkOutcome is not called,
// no outcome is reported (failures are still tracked via MarkTokenInvalid etc).
func (a *RequestAuth) MarkOutcome(success bool) {
	if a == nil {
		return
	}
	a.requestOutcomeSet = true
	a.requestSuccess = success
}

// MarkUsage records the input/output token counts for the current request.
// They will be aggregated into per-account / per-api-key stats when the request
// is released with a successful outcome.
func (a *RequestAuth) MarkUsage(inputTokens, outputTokens int) {
	if a == nil {
		return
	}
	if inputTokens > 0 {
		a.usageInputTokens = inputTokens
	}
	if outputTokens > 0 {
		a.usageOutputTokens = outputTokens
	}
}

// MarkUsageFromMap accepts a usage map produced by the various format builders
// and extracts input/output token counts using the conventional field names.
// Missing or zero fields are ignored.
//
// Recognized field names (in priority order):
//   - input_tokens / prompt_tokens / promptTokenCount  → input tokens
//   - output_tokens / completion_tokens / candidatesTokenCount → output tokens
func (a *RequestAuth) MarkUsageFromMap(usage map[string]any) {
	if a == nil || len(usage) == 0 {
		return
	}
	in := pickIntField(usage, "input_tokens", "prompt_tokens", "promptTokenCount")
	out := pickIntField(usage, "output_tokens", "completion_tokens", "candidatesTokenCount")
	a.MarkUsage(in, out)
}

func pickIntField(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if raw, ok := m[k]; ok {
			switch v := raw.(type) {
			case int:
				if v > 0 {
					return v
				}
			case int32:
				if v > 0 {
					return int(v)
				}
			case int64:
				if v > 0 {
					return int(v)
				}
			case float32:
				if v > 0 {
					return int(v)
				}
			case float64:
				if v > 0 {
					return int(v)
				}
			}
		}
	}
	return 0
}

func extractCallerToken(req *http.Request) string {
	authHeader := strings.TrimSpace(req.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		token := strings.TrimSpace(authHeader[7:])
		if token != "" {
			return token
		}
	}
	if key := strings.TrimSpace(req.Header.Get("x-api-key")); key != "" {
		return key
	}
	// Gemini/Google clients commonly send API key via x-goog-api-key.
	if key := strings.TrimSpace(req.Header.Get("x-goog-api-key")); key != "" {
		return key
	}
	// Gemini AI Studio compatibility: allow query key fallback only when no
	// header-based credential is present.
	if key := strings.TrimSpace(req.URL.Query().Get("key")); key != "" {
		return key
	}
	return strings.TrimSpace(req.URL.Query().Get("api_key"))
}

func callerTokenID(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return "caller:" + hex.EncodeToString(sum[:8])
}

func (r *Resolver) ensureManagedToken(ctx context.Context, a *RequestAuth) error {
	if strings.TrimSpace(a.Account.Token) == "" {
		return r.loginAndPersist(ctx, a)
	}
	if r.shouldForceRefresh(a.AccountID) {
		if err := r.loginAndPersist(ctx, a); err != nil {
			return err
		}
		return nil
	}
	a.DeepSeekToken = a.Account.Token
	return nil
}

func (r *Resolver) shouldForceRefresh(accountID string) bool {
	if r == nil || r.Store == nil {
		return false
	}
	if strings.TrimSpace(accountID) == "" {
		return false
	}
	intervalHours := r.Store.RuntimeTokenRefreshIntervalHours()
	if intervalHours <= 0 {
		return false
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	last, ok := r.tokenRefreshedAt[accountID]
	if !ok || last.IsZero() {
		r.tokenRefreshedAt[accountID] = now
		return false
	}
	return now.Sub(last) >= time.Duration(intervalHours)*time.Hour
}

func (r *Resolver) markTokenRefreshedNow(accountID string) {
	if strings.TrimSpace(accountID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokenRefreshedAt[accountID] = time.Now()
}

func (r *Resolver) clearTokenRefreshMark(accountID string) {
	if strings.TrimSpace(accountID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tokenRefreshedAt, accountID)
}
