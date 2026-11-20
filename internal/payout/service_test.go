package payout

import (
	"context"
	"encoding/base64"
	"net"
	"strings"
	"testing"

	"github.com/satlight/satlight/internal/config"
)

func TestParseLightningAddress(t *testing.T) {
	user, domain, err := ParseLightningAddress("contrib@wallet.example")
	if err != nil || user != "contrib" || domain != "wallet.example" {
		t.Fatalf("parse = %q %q %v", user, domain, err)
	}
	for _, bad := range []string{"", "no-at-sign", "@domain", "user@", "a@b@c", "user@do main", "user@do/main", "lnbc1230invoice"} {
		if _, _, err := ParseLightningAddress(bad); err == nil {
			t.Errorf("ParseLightningAddress(%q) err = nil, want error", bad)
		}
	}
}

func TestMaxRoutingFeeSats(t *testing.T) {
	svc := NewService(&config.Config{PayoutMaxFeePPM: 10000, PayoutMaxFeeFloorSats: 10}, nil)
	if got := svc.maxRoutingFeeSats(100000); got != 1000 {
		t.Errorf("maxRoutingFeeSats(100000) = %d, want 1000", got)
	}
	if got := svc.maxRoutingFeeSats(500); got != 10 {
		t.Errorf("maxRoutingFeeSats(500) = %d, want floor 10", got)
	}
}

func TestNewClaimToken(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := newClaimToken()
		if err != nil {
			t.Fatal(err)
		}
		if len(token) != 43 || strings.ContainsAny(token, "+/=") {
			t.Fatalf("token %q is not 43-char base64url", token)
		}
		raw, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil || len(raw) != 32 {
			t.Fatalf("token %q does not decode to 32 bytes", token)
		}
		if seen[token] {
			t.Fatal("duplicate claim token generated")
		}
		seen[token] = true
	}
}

func TestValidateOutboundURL(t *testing.T) {
	for _, good := range []string{
		"https://wallet.example/.well-known/lnurlp/alice",
		"https://wallet.example:8443/cb?amount=1000",
		"https://93.184.216.34/callback",
	} {
		if err := validateOutboundURL(good); err != nil {
			t.Errorf("validateOutboundURL(%q) = %v, want nil", good, err)
		}
	}
	for _, bad := range []string{
		"http://wallet.example/callback",
		"https://localhost/callback",
		"https://127.0.0.1/callback",
		"https://10.0.0.5/callback",
		"https://192.168.1.1/callback",
		"https://169.254.169.254/latest/meta-data",
		"https://[::1]/callback",
		"https://[fd00::1]/callback",
		"https://user:pass@wallet.example/callback",
		"https:///no-host",
		"://not-a-url",
	} {
		if err := validateOutboundURL(bad); err == nil {
			t.Errorf("validateOutboundURL(%q) = nil, want error", bad)
		}
	}
}

func TestIsPublicIP(t *testing.T) {
	for _, tc := range []struct {
		ip   string
		want bool
	}{
		{"93.184.216.34", true},
		{"2001:4860:4860::8888", true},
		{"127.0.0.1", false},
		{"10.1.2.3", false},
		{"172.16.0.1", false},
		{"192.168.0.1", false},
		{"169.254.169.254", false},
		{"::1", false},
		{"fd00::1", false},
		{"fe80::1", false},
		{"0.0.0.0", false},
	} {
		if got := isPublicIP(net.ParseIP(tc.ip)); got != tc.want {
			t.Errorf("isPublicIP(%q) = %v, want %v", tc.ip, got, tc.want)
		}
	}
	if isPublicIP(nil) {
		t.Error("isPublicIP(nil) = true, want false")
	}
}

func TestGetJSONRefusesPrivateTargets(t *testing.T) {
	svc := NewService(&config.Config{}, nil)
	var v any
	// Plain http is rejected before any network access.
	if err := svc.getJSON(context.Background(), "http://127.0.0.1/", &v); err == nil {
		t.Error("getJSON(http loopback) = nil, want error")
	}
	// Non-public IP literals are rejected during validation.
	if err := svc.getJSON(context.Background(), "https://127.0.0.1:1/", &v); err == nil {
		t.Error("getJSON(https loopback literal) = nil, want error")
	}
	// Hostnames resolving to private addresses are refused by the dial guard.
	if err := svc.getJSON(context.Background(), "https://localhost:1/", &v); err == nil {
		t.Error("getJSON(https localhost) = nil, want error")
	} else if !strings.Contains(err.Error(), "non-public") {
		t.Errorf("getJSON(https localhost) error = %q, want non-public refusal", err)
	}
}

func TestValidateLNURLTargetAllowPrivate(t *testing.T) {
	svc := NewService(&config.Config{}, nil)
	svc.allowPrivateLNURL = true
	// Loopback targets pass validation so httptest fixtures work.
	for _, good := range []string{
		"https://127.0.0.1:8443/.well-known/lnurlp/alice",
		"https://localhost:8443/callback",
	} {
		if err := svc.validateLNURLTarget(good); err != nil {
			t.Errorf("validateLNURLTarget(%q) = %v, want nil", good, err)
		}
	}
	// Scheme, credential, and host checks still apply.
	for _, bad := range []string{
		"http://127.0.0.1:8443/callback",
		"https://user:pass@127.0.0.1/callback",
		"https:///no-host",
	} {
		if err := svc.validateLNURLTarget(bad); err == nil {
			t.Errorf("validateLNURLTarget(%q) = nil, want error", bad)
		}
	}
	// The default service stays strict.
	strict := NewService(&config.Config{}, nil)
	if err := strict.validateLNURLTarget("https://127.0.0.1/callback"); err == nil {
		t.Error("default validateLNURLTarget(https loopback) = nil, want error")
	}
}
