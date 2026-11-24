package api

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/satlight/satlight/internal/auth"
)

// Rate-limit scopes. Each abuse-prone route group gets its own bucket so a
// burst of webhook deliveries can never starve interactive claim traffic.
const (
	rateLimitScopeClaims   = "claims"
	rateLimitScopeDeposits = "deposits"
	rateLimitScopeWebhooks = "webhooks"
	rateLimitScopeLogin    = "login"
	rateLimitScopeWaitlist = "waitlist"
)

// defaultRateLimits backs limitForScope when the operator leaves a scope
// unconfigured (or sets a non-positive value). They are generous enough for
// normal use and the test suite; tighten per environment via RATE_LIMIT_*.
var defaultRateLimits = map[string][2]int{
	rateLimitScopeClaims:   {30, 10},
	rateLimitScopeDeposits: {30, 10},
	rateLimitScopeWebhooks: {600, 100},
	rateLimitScopeLogin:    {30, 10},
	rateLimitScopeWaitlist: {20, 5},
}

// rateBucketMaxKeys bounds memory under a flood of distinct client IPs. Past
// the cap the limiter sweeps buckets idle longer than rateBucketIdleTTL.
const rateBucketMaxKeys = 10000

const rateBucketIdleTTL = 10 * time.Minute

type rateBucket struct {
	tokens  float64
	updated time.Time
}

// rateLimiter is a per-key token bucket. It is deliberately in-memory: limits
// are per process, which is sufficient for abuse throttling on a single API
// deployment. The now hook exists so tests can drive refills deterministically.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
	now     func() time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: map[string]*rateBucket{}, now: time.Now}
}

// allow consumes one token for key. It reports whether the request may
// proceed and, when it may not, how long to wait for the next token.
func (l *rateLimiter) allow(key string, perMinute, burst int) (bool, time.Duration) {
	if perMinute <= 0 {
		perMinute = 60
	}
	if burst <= 0 {
		burst = 1
	}
	ratePerSecond := float64(perMinute) / 60

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) >= rateBucketMaxKeys {
		l.sweepLocked(l.now())
	}
	bucket, ok := l.buckets[key]
	if !ok {
		bucket = &rateBucket{tokens: float64(burst), updated: l.now()}
		l.buckets[key] = bucket
	}
	now := l.now()
	elapsed := now.Sub(bucket.updated)
	if elapsed > 0 {
		bucket.tokens += elapsed.Seconds() * ratePerSecond
		if bucket.tokens > float64(burst) {
			bucket.tokens = float64(burst)
		}
		bucket.updated = now
	}
	if bucket.tokens >= 1 {
		bucket.tokens--
		return true, 0
	}
	deficit := 1 - bucket.tokens
	return false, time.Duration(deficit / ratePerSecond * float64(time.Second))
}

func (l *rateLimiter) sweepLocked(now time.Time) {
	for key, bucket := range l.buckets {
		if now.Sub(bucket.updated) > rateBucketIdleTTL {
			delete(l.buckets, key)
		}
	}
}

// limitForScope resolves the configured per-minute rate and burst for a
// scope, falling back to built-in defaults for missing or invalid values.
func (s *Server) limitForScope(scope string) (perMinute, burst int) {
	fallback := defaultRateLimits[scope]
	if fallback == [2]int{} {
		fallback = [2]int{60, 10}
	}
	perMinute, burst = fallback[0], fallback[1]
	configured := [2]int{}
	switch scope {
	case rateLimitScopeClaims:
		configured = [2]int{s.cfg.RateLimitClaimsPerMinute, s.cfg.RateLimitClaimsBurst}
	case rateLimitScopeDeposits:
		configured = [2]int{s.cfg.RateLimitDepositsPerMinute, s.cfg.RateLimitDepositsBurst}
	case rateLimitScopeWebhooks:
		configured = [2]int{s.cfg.RateLimitWebhooksPerMinute, s.cfg.RateLimitWebhooksBurst}
	case rateLimitScopeLogin:
		configured = [2]int{s.cfg.RateLimitLoginPerMinute, s.cfg.RateLimitLoginBurst}
	case rateLimitScopeWaitlist:
		configured = [2]int{s.cfg.RateLimitWaitlistPerMinute, s.cfg.RateLimitWaitlistBurst}
	}
	if configured[0] > 0 {
		perMinute = configured[0]
	}
	if configured[1] > 0 {
		burst = configured[1]
	}
	return perMinute, burst
}

// rateLimitMiddleware throttles one route group. Authenticated callers are
// keyed by user ID and anonymous callers by client IP, so one abusive IP
// cannot consume another user's budget. Place it after authRequired on
// authenticated routes so the user key is available. Denials answer 429 with
// a Retry-After header per the API contract.
func (s *Server) rateLimitMiddleware(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.limiter == nil {
			c.Next()
			return
		}
		key := scope + "\x00" + rateLimitIdentity(c)
		perMinute, burst := s.limitForScope(scope)
		if ok, retryAfter := s.limiter.allow(key, perMinute, burst); !ok {
			seconds := int64(retryAfter.Round(time.Second).Seconds())
			if seconds < 1 {
				seconds = 1
			}
			c.Header("Retry-After", strconv.FormatInt(seconds, 10))
			c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"code": "RATE_LIMITED", "message": "too many requests; try again shortly"}})
			c.Abort()
			return
		}
		c.Next()
	}
}

func rateLimitIdentity(c *gin.Context) string {
	if userID, ok := auth.GetUserID(c); ok {
		return "user:" + strconv.FormatInt(userID, 10)
	}
	return "ip:" + c.ClientIP()
}
