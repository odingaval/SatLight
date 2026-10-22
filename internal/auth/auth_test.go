package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/satlight/satlight/internal/config"
)

func TestGenerateAndValidateToken(t *testing.T) {
	service := &Service{cfg: &config.Config{SessionSecret: "test-session-secret"}}

	token, err := service.GenerateToken(42)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	claims, err := service.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if claims.UserID != 42 {
		t.Fatalf("UserID = %d, want 42", claims.UserID)
	}
	if claims.ExpiresAt == nil || claims.ExpiresAt.Time.Before(time.Now()) {
		t.Fatalf("token expiration is missing or already expired: %#v", claims.ExpiresAt)
	}
}

func TestValidateTokenRejectsInvalidTokens(t *testing.T) {
	service := &Service{cfg: &config.Config{SessionSecret: "expected-secret"}}
	otherService := &Service{cfg: &config.Config{SessionSecret: "other-secret"}}
	wrongSecretToken, err := otherService.GenerateToken(9)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	expiredToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		UserID: 9,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
	}).SignedString([]byte("expected-secret"))
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}

	for _, test := range []struct {
		name  string
		token string
	}{
		{name: "malformed", token: "not-a-jwt"},
		{name: "wrong signing key", token: wrongSecretToken},
		{name: "expired", token: expiredToken},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.ValidateToken(test.token); err == nil {
				t.Fatal("ValidateToken() succeeded, want an error")
			}
		})
	}
}

func TestAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &Service{cfg: &config.Config{SessionSecret: "middleware-secret"}}
	validToken, err := service.GenerateToken(73)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	for _, test := range []struct {
		name       string
		cookie     string
		wantStatus int
		wantUserID string
	}{
		{name: "missing cookie", wantStatus: http.StatusUnauthorized},
		{name: "invalid cookie", cookie: "bad-token", wantStatus: http.StatusUnauthorized},
		{name: "valid cookie", cookie: validToken, wantStatus: http.StatusNoContent, wantUserID: "73"},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/private", service.Middleware(), func(c *gin.Context) {
				userID, ok := GetUserID(c)
				if !ok {
					c.Status(http.StatusInternalServerError)
					return
				}
				c.Header("X-User-ID", strconv.FormatInt(userID, 10))
				c.Status(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodGet, "/private", nil)
			if test.cookie != "" {
				request.AddCookie(&http.Cookie{Name: "satlight_session", Value: test.cookie})
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if got := recorder.Header().Get("X-User-ID"); got != test.wantUserID {
				t.Fatalf("X-User-ID = %q, want %q", got, test.wantUserID)
			}
		})
	}
}

func TestSetSessionCookieSecurityAttributes(t *testing.T) {
	for _, test := range []struct {
		name       string
		production bool
		wantSecure bool
	}{
		{name: "development", wantSecure: false},
		{name: "production", production: true, wantSecure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			service := &Service{cfg: &config.Config{AppEnv: map[bool]string{true: "production", false: "development"}[test.production]}}
			router.GET("/cookie", func(c *gin.Context) {
				service.SetSessionCookie(c, "session-value")
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/cookie", nil))
			cookies := recorder.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookie count = %d, want 1", len(cookies))
			}
			cookie := cookies[0]
			if !cookie.HttpOnly || cookie.Secure != test.wantSecure || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatalf("cookie security attributes = HttpOnly:%v Secure:%v SameSite:%v", cookie.HttpOnly, cookie.Secure, cookie.SameSite)
			}
		})
	}
}

func TestVerifyGitHubSignature(t *testing.T) {
	payload := []byte(`{"action":"opened"}`)
	secret := "webhook-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	validSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	for _, test := range []struct {
		name      string
		payload   []byte
		signature string
		want      bool
	}{
		{name: "valid", payload: payload, signature: validSignature, want: true},
		{name: "wrong prefix", payload: payload, signature: "sha1=" + validSignature, want: false},
		{name: "tampered payload", payload: []byte(`{"action":"closed"}`), signature: validSignature, want: false},
		{name: "tampered signature", payload: payload, signature: "sha256=deadbeef", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := VerifyGitHubSignature(test.payload, test.signature, secret); got != test.want {
				t.Fatalf("VerifyGitHubSignature() = %v, want %v", got, test.want)
			}
		})
	}
}
