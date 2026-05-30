package client

import (
	"context"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	trans "ds2api/internal/deepseek/transport"
	"ds2api/internal/devcapture"
	"ds2api/internal/util"
)

// intFrom is a package-internal alias for the shared util version.
var intFrom = util.IntFrom

type sessionCacheEntry struct {
	sessionID string
	expiresAt time.Time
}

type Client struct {
	Store      *config.Store
	Auth       *auth.Resolver
	capture    *devcapture.Store
	regular    trans.Doer
	stream     trans.Doer
	fallback   *http.Client
	fallbackS  *http.Client
	maxRetries int

	proxyClientsMu sync.RWMutex
	proxyClients   map[string]requestClients

	jitterMinMs int
	jitterMaxMs int
	jitterOnce  sync.Once

	sessionCache   map[string]sessionCacheEntry
	sessionCacheMu sync.Mutex
	sessionTTL     time.Duration
	sessionTTLOnce sync.Once

	// hifCache stores the DeepSeek web anti-bot tokens (x-hif-leim /
	// x-hif-dliq) per upstream token, with a TTL-based expiry.
	hifCache map[string]hifEntry
	hifMu    sync.Mutex
}

func NewClient(store *config.Store, resolver *auth.Resolver) *Client {
	return &Client{
		Store:        store,
		Auth:         resolver,
		capture:      devcapture.Global(),
		regular:      trans.New(60 * time.Second),
		stream:       trans.New(0),
		fallback:     &http.Client{Timeout: 60 * time.Second},
		fallbackS:    &http.Client{Timeout: 0},
		maxRetries:   3,
		proxyClients: map[string]requestClients{},
		hifCache:     map[string]hifEntry{},
	}
}

// PreloadPow 保留兼容接口，纯 Go 实现无需预加载。
func (c *Client) PreloadPow(_ context.Context) error {
	return nil
}

// initSessionTTL reads session cache TTL from env var once.
func (c *Client) initSessionTTL() {
	c.sessionTTLOnce.Do(func() {
		min := envInt("DS2API_SESSION_CACHE_TTL_MINUTES", 10)
		if min <= 0 {
			c.sessionTTL = 0
			return
		}
		c.sessionTTL = time.Duration(min) * time.Minute
	})
}

// getCachedSession returns a cached session ID for the given account, or empty string.
func (c *Client) getCachedSession(accountID string) string {
	c.initSessionTTL()
	if c.sessionTTL <= 0 || accountID == "" {
		return ""
	}
	c.sessionCacheMu.Lock()
	defer c.sessionCacheMu.Unlock()
	if c.sessionCache == nil {
		c.sessionCache = map[string]sessionCacheEntry{}
		return ""
	}
	entry, ok := c.sessionCache[accountID]
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			delete(c.sessionCache, accountID)
		}
		return ""
	}
	return entry.sessionID
}

// cacheSession stores a session ID for an account with TTL-based expiry.
func (c *Client) cacheSession(accountID, sessionID string) {
	c.initSessionTTL()
	if c.sessionTTL <= 0 || accountID == "" || sessionID == "" {
		return
	}
	c.sessionCacheMu.Lock()
	defer c.sessionCacheMu.Unlock()
	if c.sessionCache == nil {
		c.sessionCache = map[string]sessionCacheEntry{}
	}
	c.sessionCache[accountID] = sessionCacheEntry{
		sessionID: sessionID,
		expiresAt: time.Now().Add(c.sessionTTL),
	}
}

// invalidateSessionCache removes a cached session for the given account.
func (c *Client) invalidateSessionCache(accountID string) {
	if accountID == "" {
		return
	}
	c.sessionCacheMu.Lock()
	defer c.sessionCacheMu.Unlock()
	delete(c.sessionCache, accountID)
}

// initJitter reads jitter configuration from environment variables once.
//
// Two ranges are supported:
//   - DS2API_REQUEST_JITTER_{MIN,MAX}_MS: applied to every individual upstream
//     call (login, create_session, get_pow, completion). Defaults are kept
//     small so total per-request stack stays under ~1s in the worst case.
//   - The legacy alias of identical default 200/800 is preserved when the
//     operator has not changed defaults.
func (c *Client) initJitter() {
	c.jitterOnce.Do(func() {
		minMs := envInt("DS2API_REQUEST_JITTER_MIN_MS", 80)
		maxMs := envInt("DS2API_REQUEST_JITTER_MAX_MS", 350)
		if minMs < 0 {
			minMs = 0
		}
		if maxMs < minMs {
			maxMs = minMs
		}
		c.jitterMinMs = minMs
		c.jitterMaxMs = maxMs
	})
}

// Jitter sleeps for a random duration between min and max configured jitter.
// The sleep follows a right-skewed distribution (log-uniform) so most calls
// finish near the lower bound while a small tail occasionally stretches
// toward the upper bound, mimicking human-driven mobile networks. Pure
// uniform random is itself a fingerprint-able pattern.
//
// If both min and max are 0, no delay is added. The jitter respects an
// optional context to remain cancellable.
func (c *Client) Jitter() {
	c.JitterCtx(context.Background())
}

// JitterCtx is the context-aware variant of Jitter.
func (c *Client) JitterCtx(ctx context.Context) {
	c.initJitter()
	if c.jitterMaxMs <= 0 {
		return
	}
	d := pickJitterDelay(c.jitterMinMs, c.jitterMaxMs)
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// pickJitterDelay returns a random delay between minMs and maxMs that is
// log-uniform: the closer to the lower bound, the more likely. We sample
// from x = exp(ln(min+1) + r * (ln(max+1) - ln(min+1))) - 1.
//
// Examples (min=200, max=800):
//
//	~50% of values land in [200, 400]
//	~30% land in [400, 600]
//	~20% land in [600, 800]
//
// Pure uniform sampling is rejected because constant-rate request timing is
// itself a fingerprint that automated traffic detectors flag easily.
func pickJitterDelay(minMs, maxMs int) time.Duration {
	if maxMs <= 0 {
		return 0
	}
	if minMs >= maxMs {
		return time.Duration(maxMs) * time.Millisecond
	}
	lnLo := math.Log(float64(minMs) + 1)
	lnHi := math.Log(float64(maxMs) + 1)
	r := rand.Float64()
	v := math.Exp(lnLo+r*(lnHi-lnLo)) - 1
	if v < float64(minMs) {
		v = float64(minMs)
	}
	if v > float64(maxMs) {
		v = float64(maxMs)
	}
	return time.Duration(v) * time.Millisecond
}

// envInt returns the integer value of an env var, or a default if unset/invalid.
func envInt(key string, defaultVal int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return defaultVal
	}
	return v
}
