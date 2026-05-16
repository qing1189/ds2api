package client

import (
	"context"
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
func (c *Client) initJitter() {
	c.jitterOnce.Do(func() {
		minMs := envInt("DS2API_REQUEST_JITTER_MIN_MS", 200)
		maxMs := envInt("DS2API_REQUEST_JITTER_MAX_MS", 800)
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
// If both are 0, no delay is added. Call before each external API request
// to simulate human-like timing and reduce risk-control detection.
func (c *Client) Jitter() {
	c.initJitter()
	if c.jitterMaxMs <= 0 {
		return
	}
	d := c.jitterMinMs
	if c.jitterMaxMs > c.jitterMinMs {
		d += rand.Intn(c.jitterMaxMs - c.jitterMinMs + 1)
	}
	time.Sleep(time.Duration(d) * time.Millisecond)
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
