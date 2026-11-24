package config

import (
	"testing"
)

// loadForTest seeds the required secrets so Load exercises the defaults and
// host overrides instead of failing fast.
func loadForTest(t *testing.T) *Config {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://test/test?sslmode=disable")
	t.Setenv("SESSION_SECRET", "test-secret")
	t.Setenv("ENCRYPTION_KEY", "test-key")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return cfg
}

func TestLoadHonorsPortWhenAddrUnset(t *testing.T) {
	t.Setenv("PORT", "10000")
	t.Setenv("HTTP_ADDR", "")
	if cfg := loadForTest(t); cfg.HTTPAddr != ":10000" {
		t.Fatalf("HTTPAddr = %q, want :10000", cfg.HTTPAddr)
	}
}

func TestLoadExplicitAddrBeatsPort(t *testing.T) {
	t.Setenv("PORT", "10000")
	t.Setenv("HTTP_ADDR", ":9000")
	if cfg := loadForTest(t); cfg.HTTPAddr != ":9000" {
		t.Fatalf("HTTPAddr = %q, want :9000", cfg.HTTPAddr)
	}
}

func TestLoadDefaultsAddrWithoutPort(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("HTTP_ADDR", "")
	if cfg := loadForTest(t); cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
}

func TestLoadRenderURLDefaults(t *testing.T) {
	t.Setenv("RENDER_EXTERNAL_URL", "https://satlight.onrender.com")
	t.Setenv("BASE_URL", "")
	t.Setenv("WEB_URL", "")
	cfg := loadForTest(t)
	if cfg.BaseURL != "https://satlight.onrender.com" || cfg.WebURL != "https://satlight.onrender.com" {
		t.Fatalf("BaseURL/WebURL = %q/%q, want the Render URL", cfg.BaseURL, cfg.WebURL)
	}
}
