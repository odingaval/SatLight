package payout

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/satlight/satlight/internal/config"
)

func TestParseLightningAddress(t *testing.T) {
	user, domain, err := parseLightningAddress("contrib@wallet.example")
	if err != nil || user != "contrib" || domain != "wallet.example" {
		t.Fatalf("parse = %q %q %v", user, domain, err)
	}
	for _, bad := range []string{"", "no-at-sign", "@domain", "user@", "a@b@c", "user@do main", "user@do/main", "lnbc1230invoice"} {
		if _, _, err := parseLightningAddress(bad); err == nil {
			t.Errorf("parseLightningAddress(%q) err = nil, want error", bad)
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
