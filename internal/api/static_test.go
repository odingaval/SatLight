package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func TestStaticRoutesServeAppWithSPAFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fsys := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>satlight</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	router := gin.New()
	registerStaticRoutes(router, fsys)

	cases := []struct {
		name        string
		path        string
		wantCode    int
		wantContent string
	}{
		{"root serves index", "/", http.StatusOK, "<title>satlight</title>"},
		{"app route falls back to index", "/bounties/123", http.StatusOK, "<title>satlight</title>"},
		{"nested app route falls back to index", "/my-work", http.StatusOK, "<title>satlight</title>"},
		{"asset serves file bytes", "/assets/app.js", http.StatusOK, "console.log(1)"},
		{"unknown api path keeps json 404", "/api/v1/nope", http.StatusNotFound, "NOT_FOUND"},
		{"metrics keeps json 404", "/metrics", http.StatusNotFound, "NOT_FOUND"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if recorder.Code != tc.wantCode {
				t.Fatalf("GET %s = %d, want %d", tc.path, recorder.Code, tc.wantCode)
			}
			if !strings.Contains(recorder.Body.String(), tc.wantContent) {
				t.Fatalf("GET %s body %q lacks %q", tc.path, recorder.Body.String(), tc.wantContent)
			}
		})
	}
}

func TestStaticRoutesWithoutIndexReturnJSON404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerStaticRoutes(router, fstest.MapFS{})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("GET / without index = %d, want 404", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "NOT_FOUND") {
		t.Fatalf("body %q lacks NOT_FOUND", recorder.Body.String())
	}
}
