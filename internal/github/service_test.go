package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"testing"
)

func TestVerifyWebhookSignature(t *testing.T) {
	payload := []byte(`{"action":"opened","number":12}`)
	service := &Service{cfg: &Config{WebhookSecret: "test-webhook-secret"}}
	mac := hmac.New(sha256.New, []byte(service.cfg.WebhookSecret))
	_, _ = mac.Write(payload)
	valid := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	for _, test := range []struct {
		name      string
		payload   []byte
		signature string
		want      bool
	}{
		{name: "valid signature", payload: payload, signature: valid, want: true},
		{name: "missing algorithm prefix", payload: payload, signature: valid[len("sha256="):], want: false},
		{name: "tampered payload", payload: []byte(`{"action":"closed","number":12}`), signature: valid, want: false},
		{name: "bad digest", payload: payload, signature: "sha256=not-hex", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := service.VerifyWebhookSignature(test.payload, test.signature); got != test.want {
				t.Fatalf("VerifyWebhookSignature() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestGetOAuthURL(t *testing.T) {
	service := &Service{cfg: &Config{OAuthClientID: "client-id", OAuthClientSecret: "client-secret"}}
	got, err := url.Parse(service.GetOAuthURL("random-state", "https://app.example.test/api/v1/auth/github/callback"))
	if err != nil {
		t.Fatalf("GetOAuthURL() returned invalid URL: %v", err)
	}
	if got.Host != "github.com" || got.Path != "/login/oauth/authorize" {
		t.Fatalf("OAuth URL = %q, want GitHub authorize URL", got)
	}
	query := got.Query()
	if query.Get("client_id") != "client-id" {
		t.Fatalf("client_id = %q, want client-id", query.Get("client_id"))
	}
	if query.Get("redirect_uri") != "https://app.example.test/api/v1/auth/github/callback" {
		t.Fatalf("redirect_uri = %q", query.Get("redirect_uri"))
	}
	if query.Get("state") != "random-state" {
		t.Fatalf("state = %q, want random-state", query.Get("state"))
	}
	if query.Get("scope") != "read:user user:email" {
		t.Fatalf("scope = %q, want user profile and email scopes", query.Get("scope"))
	}
	if query.Has("client_secret") {
		t.Fatal("OAuth client secret must not be exposed in the authorization URL")
	}
}

func TestExtractIssueNumber(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want int
	}{
		{name: "closes", body: "Fixes docs. Closes #42", want: 42},
		{name: "fixes lowercase", body: "fixes #7 and improves tests", want: 7},
		{name: "resolves", body: "Resolves #123\n\nMore detail", want: 123},
		{name: "no reference", body: "This pull request adds a feature", want: 0},
		{name: "reference without digits", body: "Closes #issue", want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := extractIssueNumber(test.body); got != test.want {
				t.Fatalf("extractIssueNumber(%q) = %d, want %d", test.body, got, test.want)
			}
		})
	}
}
