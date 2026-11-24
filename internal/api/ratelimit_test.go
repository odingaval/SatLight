package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/satlight/satlight/internal/config"
)

func TestRateLimiterBurstAndRefill(t *testing.T) {
	now := time.Now()
	limiter := &rateLimiter{buckets: map[string]*rateBucket{}, now: func() time.Time { return now }}

	// Burst of 3 at 60/min: the first three pass, the fourth is held.
	for i := 0; i < 3; i++ {
		if ok, _ := limiter.allow("k", 60, 3); !ok {
			t.Fatalf("request %d: denied within burst", i)
		}
	}
	if ok, retry := limiter.allow("k", 60, 3); ok || retry <= 0 {
		t.Fatalf("request 4: ok=%v retry=%v, want denied with positive backoff", ok, retry)
	}
	// After a full window the bucket refills completely.
	now = now.Add(time.Minute)
	for i := 0; i < 3; i++ {
		if ok, _ := limiter.allow("k", 60, 3); !ok {
			t.Fatalf("refilled request %d: denied, want allowed", i)
		}
	}
}

func TestRateLimiterRetryAfter(t *testing.T) {
	now := time.Now()
	limiter := &rateLimiter{buckets: map[string]*rateBucket{}, now: func() time.Time { return now }}

	if ok, _ := limiter.allow("k", 60, 1); !ok {
		t.Fatal("first request denied, want allowed")
	}
	ok, retry := limiter.allow("k", 60, 1)
	if ok {
		t.Fatal("second request allowed past burst 1")
	}
	// At 60/min one token accrues per second; the middleware must tell the
	// client to wait at least that long.
	if retry < time.Second || retry > 2*time.Second {
		t.Fatalf("retryAfter = %v, want ~1s", retry)
	}
}

func TestRateLimiterKeyPartition(t *testing.T) {
	limiter := newRateLimiter()
	if ok, _ := limiter.allow("user:1", 60, 1); !ok {
		t.Fatal("user:1 denied, want allowed")
	}
	if ok, _ := limiter.allow("user:1", 60, 1); ok {
		t.Fatal("user:1 second request allowed past burst 1")
	}
	if ok, _ := limiter.allow("user:2", 60, 1); !ok {
		t.Fatal("user:2 denied by user:1's bucket, want independent keys")
	}
	if ok, _ := limiter.allow("ip:198.51.100.7", 60, 1); !ok {
		t.Fatal("ip key denied by user buckets, want independent keys")
	}
}

// The middleware keys authenticated callers by user and anonymous callers by
// IP, answers 429 with a Retry-After header, and keeps the project's error
// body shape.
func TestRateLimitMiddlewareKeysAndHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newRouter := func() *gin.Engine {
		server := &Server{
			cfg:     &config.Config{RateLimitClaimsPerMinute: 60, RateLimitClaimsBurst: 1},
			limiter: newRateLimiter(),
		}
		router := gin.New()
		// The stub stands in for the auth middleware: it marks the request
		// authenticated when the test sends a user cookie.
		router.GET("/claims", func(c *gin.Context) {
			if user, err := c.Cookie("user"); err == nil {
				id, _ := strconv.ParseInt(user, 10, 64)
				c.Set("user_id", id)
			}
			c.Next()
		}, server.rateLimitMiddleware("claims"), func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		})
		return router
	}

	serve := func(router *gin.Engine, user, ip string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/claims", nil)
		request.RemoteAddr = ip + ":1234"
		if user != "" {
			request.AddCookie(&http.Cookie{Name: "user", Value: user})
		}
		router.ServeHTTP(recorder, request)
		return recorder
	}

	t.Run("authenticated users are partitioned", func(t *testing.T) {
		router := newRouter()
		if code := serve(router, "1", "198.51.100.7").Code; code != http.StatusNoContent {
			t.Fatalf("user 1 first = %d, want 204", code)
		}
		recorder := serve(router, "1", "198.51.100.7")
		if recorder.Code != http.StatusTooManyRequests {
			t.Fatalf("user 1 second = %d, want 429", recorder.Code)
		}
		if recorder.Header().Get("Retry-After") == "" {
			t.Fatal("429 is missing the Retry-After header")
		}
		if body := recorder.Body.String(); body == "" || !strings.Contains(body, "RATE_LIMITED") {
			t.Fatalf("429 body = %q, want RATE_LIMITED code", body)
		}
		if code := serve(router, "2", "198.51.100.7").Code; code != http.StatusNoContent {
			t.Fatalf("user 2 same IP = %d, want 204 (independent bucket)", code)
		}
	})

	t.Run("anonymous callers share the IP bucket", func(t *testing.T) {
		router := newRouter()
		if code := serve(router, "", "203.0.113.9").Code; code != http.StatusNoContent {
			t.Fatalf("first anonymous = %d, want 204", code)
		}
		if code := serve(router, "", "203.0.113.9").Code; code != http.StatusTooManyRequests {
			t.Fatalf("second anonymous same IP = %d, want 429", code)
		}
		if code := serve(router, "", "203.0.113.10").Code; code != http.StatusNoContent {
			t.Fatalf("other IP = %d, want 204", code)
		}
	})
}

func TestLimitForScopeFallsBackToDefaults(t *testing.T) {
	server := &Server{cfg: &config.Config{}}
	for _, scope := range []string{"claims", "deposits", "webhooks", "login", "waitlist", "unknown"} {
		perMinute, burst := server.limitForScope(scope)
		if perMinute <= 0 || burst <= 0 {
			t.Fatalf("scope %q: perMinute=%d burst=%d, want positive defaults", scope, perMinute, burst)
		}
	}
	server = &Server{cfg: &config.Config{RateLimitClaimsPerMinute: 5, RateLimitClaimsBurst: 2}}
	if perMinute, burst := server.limitForScope("claims"); perMinute != 5 || burst != 2 {
		t.Fatalf("claims override = %d/%d, want 5/2", perMinute, burst)
	}
}
