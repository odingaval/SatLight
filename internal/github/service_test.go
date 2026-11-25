package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v60/github"
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

func TestSplitRepoFullName(t *testing.T) {
	owner, repo, ok := splitRepoFullName("octo/demo")
	if !ok || owner != "octo" || repo != "demo" {
		t.Fatalf("splitRepoFullName(octo/demo) = %q %q %t", owner, repo, ok)
	}
	for _, bad := range []string{"", "noseparator", "/repo", "owner/", "a/b/c"} {
		if _, _, ok := splitRepoFullName(bad); ok {
			t.Errorf("splitRepoFullName(%q) ok = true, want false", bad)
		}
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

func TestListAdminRepos(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/installation/repositories":
			_, _ = w.Write([]byte(`{"total_count":3,"repositories":[
				{"full_name":"octo/zebra","private":true},
				{"full_name":"octo/apple","private":false},
				{"full_name":"octo/shared","private":false}]}`))
		case "/repos/octo/zebra/collaborators/maintainer/permission":
			_, _ = w.Write([]byte(`{"permission":"admin"}`))
		case "/repos/octo/apple/collaborators/maintainer/permission":
			_, _ = w.Write([]byte(`{"permission":"admin"}`))
		case "/repos/octo/shared/collaborators/maintainer/permission":
			_, _ = w.Write([]byte(`{"permission":"write"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	service := NewService(&Config{}, nil)
	service.SetInstallationClientFactory(func(int64) (*github.Client, error) {
		client := github.NewClient(nil)
		base, err := url.Parse(server.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		client.BaseURL = base
		client.UploadURL = base
		return client, nil
	})
	repos, err := service.ListAdminRepos(context.Background(), 123, "maintainer")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0].FullName != "octo/apple" || repos[1].FullName != "octo/zebra" {
		t.Fatalf("ListAdminRepos() = %+v, want sorted admin-only [octo/apple octo/zebra]", repos)
	}
	if repos[0].Private || !repos[1].Private {
		t.Fatalf("privacy flags = %+v, want apple public, zebra private", repos)
	}
}

func TestListRegistrableRepos(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/app/installations":
			_, _ = w.Write([]byte(`[{"id":11,"account":{"login":"octo"}},{"id":22,"account":{"login":"acme"}}]`))
		case "/installation/repositories":
			_, _ = w.Write([]byte(`{"total_count":1,"repositories":[{"full_name":"octo/app","private":true}]}`))
		case "/repos/octo/app/collaborators/maintainer/permission":
			_, _ = w.Write([]byte(`{"permission":"admin"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	service := NewService(&Config{}, nil)
	mockClient := func() (*github.Client, error) {
		client := github.NewClient(nil)
		base, err := url.Parse(server.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		client.BaseURL = base
		client.UploadURL = base
		return client, nil
	}
	service.SetAppClientFactory(mockClient)
	service.SetInstallationClientFactory(func(int64) (*github.Client, error) { return mockClient() })

	installations, err := service.ListInstallations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(installations) != 2 || installations[0].ID != 11 || installations[1].AccountLogin != "acme" {
		t.Fatalf("ListInstallations() = %+v, want IDs 11 and 22", installations)
	}
	repos, err := service.ListRegistrableRepos(context.Background(), "maintainer")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0].InstallationID != 11 || repos[1].InstallationID != 22 {
		t.Fatalf("ListRegistrableRepos() = %+v, want octo/app paired with installations 11 and 22", repos)
	}
}

func TestListAdminReposPropagatesListErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	service := NewService(&Config{}, nil)
	service.SetInstallationClientFactory(func(int64) (*github.Client, error) {
		client := github.NewClient(nil)
		base, err := url.Parse(server.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		client.BaseURL = base
		client.UploadURL = base
		return client, nil
	})
	if _, err := service.ListAdminRepos(context.Background(), 123, "maintainer"); err == nil {
		t.Fatal("ListAdminRepos() = nil, want the API error")
	}
}
