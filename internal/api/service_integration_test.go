//go:build integration

package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/satlight/satlight/internal/auth"
	"github.com/satlight/satlight/internal/config"
)

type apiFixture struct {
	router  *gin.Engine
	pool    *pgxpool.Pool
	cfg     *config.Config
	ownerID int64
}

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("GITHUB_TEST_MODE", "true")
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL must point to a disposable integration-test database")
	}
	m, err := migrate.New("file://../../migrations", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatal(err)
	}
	if sourceErr, databaseErr := m.Close(); sourceErr != nil || databaseErr != nil {
		t.Fatalf("close migration handle: source=%v database=%v", sourceErr, databaseErr)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `UPDATE system_flags SET enabled = FALSE, reason = NULL`); err != nil {
		t.Fatal(err)
	}
	riverMigrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := riverMigrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		AppEnv: "test", Network: "regtest", HTTPAddr: ":0",
		BaseURL: "http://localhost:8080", WebURL: "http://localhost:3000",
		DatabaseURL: databaseURL, GitHubWebhookSecret: "test-secret",
		PlatformFeeBPS: 200, BountyDefaultExpiryDays: 90, ClaimExpiryDays: 7,
		MinRewardSats: 500, SessionSecret: "test-session-secret",
		SMTPURL: "smtp://localhost:1025", EmailFrom: "noreply@test",
		PayoutMaxFeePPM: 10000, PayoutMaxFeeFloorSats: 10, DailyPayoutCapSats: 1000000,
	}
	unique := uuid.NewString()
	var ownerID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (github_id, github_login, email) VALUES ($1, $2, $3) RETURNING id
	`, time.Now().UnixNano(), "apiowner-"+unique[:8], "apiowner-"+unique[:8]+"@example.test").Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	return &apiFixture{router: NewServer(cfg, pool).Router(), pool: pool, cfg: cfg, ownerID: ownerID}
}

func (f *apiFixture) cookie(t *testing.T, userID int64) *http.Cookie {
	t.Helper()
	token, err := auth.NewService(f.cfg, f.pool).GenerateToken(userID)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "satlight_session", Value: token}
}

func (f *apiFixture) user(t *testing.T, lightningAddress string) int64 {
	t.Helper()
	unique := uuid.NewString()
	var userID int64
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO users (github_id, github_login, email, lightning_address) VALUES ($1, $2, $3, $4) RETURNING id
	`, time.Now().UnixNano(), "apiuser-"+unique[:8], "apiuser-"+unique[:8]+"@example.test", lightningAddress).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	return userID
}

func (f *apiFixture) project(t *testing.T, ownerID int64, fundedSats int64) int64 {
	t.Helper()
	unique := uuid.NewString()
	var projectID int64
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO projects (owner_user_id, github_installation_id, repo_full_name) VALUES ($1, $2, $3) RETURNING id
	`, ownerID, time.Now().UnixNano(), "api/"+unique[:8]).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO wallets (project_id, available_sats, reserved_sats) VALUES ($1, $2, 0)`,
		projectID, fundedSats); err != nil {
		t.Fatal(err)
	}
	return projectID
}

func doRequest(t *testing.T, router *gin.Engine, method, path string, body any, headers map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else if raw, ok := body.([]byte); ok {
		reader = bytes.NewReader(raw)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &data); err != nil {
		t.Fatalf("decode response %d: %v (%s)", recorder.Code, err, recorder.Body.String())
	}
	return data
}

func TestIntegrationAPIProjectCreateAndWallet(t *testing.T) {
	f := newAPIFixture(t)
	ownerCookie := f.cookie(t, f.ownerID)

	recorder := doRequest(t, f.router, "POST", "/api/v1/projects",
		map[string]any{"repo_full_name": "api/new-" + uuid.NewString()[:8], "installation_id": 4242}, nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated project create = %d, want 401", recorder.Code)
	}
	recorder = doRequest(t, f.router, "POST", "/api/v1/projects",
		map[string]any{"repo_full_name": "api/new-" + uuid.NewString()[:8], "installation_id": 4242}, nil, ownerCookie)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("project create without GitHub admin proof = %d (%s), want 403", recorder.Code, recorder.Body.String())
	}
	projectID := f.project(t, f.ownerID, 100000)
	recorder = doRequest(t, f.router, "GET", fmt.Sprintf("/api/v1/projects/%d/wallet", projectID), nil, nil, ownerCookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("get wallet = %d, want 200", recorder.Code)
	}
	otherCookie := f.cookie(t, f.user(t, ""))
	recorder = doRequest(t, f.router, "GET", fmt.Sprintf("/api/v1/projects/%d/wallet", projectID), nil, nil, otherCookie)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-owner wallet read = %d, want 403", recorder.Code)
	}
}

func TestIntegrationAPIBountyCreateIdempotency(t *testing.T) {
	f := newAPIFixture(t)
	ownerCookie := f.cookie(t, f.ownerID)
	projectID := f.project(t, f.ownerID, 100000)
	bountyBody := func() map[string]any {
		return map[string]any{"github_issue_number": 11, "title": "API bounty", "description": "via api",
			"difficulty": "easy", "reward_sats": 5000}
	}
	path := fmt.Sprintf("/api/v1/projects/%d/bounties", projectID)

	recorder := doRequest(t, f.router, "POST", path, bountyBody(), nil, ownerCookie)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key = %d, want 400", recorder.Code)
	}
	key := "bounty-" + uuid.NewString()
	headers := map[string]string{"Idempotency-Key": key}
	recorder = doRequest(t, f.router, "POST", path, bountyBody(), headers, ownerCookie)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("bounty create = %d (%s), want 201", recorder.Code, recorder.Body.String())
	}
	firstID := decodeBody(t, recorder)["id"]
	recorder = doRequest(t, f.router, "POST", path, bountyBody(), headers, ownerCookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("bounty retry = %d, want 200 replay", recorder.Code)
	}
	if decodeBody(t, recorder)["id"] != firstID {
		t.Fatal("idempotent retry returned a different bounty")
	}
	changed := bountyBody()
	changed["reward_sats"] = 6000
	recorder = doRequest(t, f.router, "POST", path, changed, headers, ownerCookie)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("key reuse with different data = %d, want 409", recorder.Code)
	}
	recorder = doRequest(t, f.router, "POST", path, bountyBody(),
		map[string]string{"Idempotency-Key": "other-" + uuid.NewString()}, ownerCookie)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("duplicate issue = %d, want 409", recorder.Code)
	}
	poorProject := f.project(t, f.ownerID, 100)
	recorder = doRequest(t, f.router, "POST", fmt.Sprintf("/api/v1/projects/%d/bounties", poorProject), bountyBody(),
		map[string]string{"Idempotency-Key": "poor-" + uuid.NewString()}, ownerCookie)
	if recorder.Code != http.StatusPaymentRequired {
		t.Fatalf("unfunded bounty = %d, want 402", recorder.Code)
	}
	strangerCookie := f.cookie(t, f.user(t, ""))
	recorder = doRequest(t, f.router, "POST", path, bountyBody(),
		map[string]string{"Idempotency-Key": "stranger-" + uuid.NewString()}, strangerCookie)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-owner bounty create = %d, want 403", recorder.Code)
	}
}

func TestIntegrationAPIClaimFlow(t *testing.T) {
	f := newAPIFixture(t)
	ownerCookie := f.cookie(t, f.ownerID)
	projectID := f.project(t, f.ownerID, 100000)
	recorder := doRequest(t, f.router, "POST", fmt.Sprintf("/api/v1/projects/%d/bounties", projectID),
		map[string]any{"github_issue_number": 21, "title": "Claim me", "description": "claim",
			"difficulty": "easy", "reward_sats": 5000},
		map[string]string{"Idempotency-Key": "b-" + uuid.NewString()}, ownerCookie)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("setup bounty create = %d", recorder.Code)
	}
	bountyID := int64(decodeBody(t, recorder)["id"].(float64))
	claimPath := fmt.Sprintf("/api/v1/bounties/%d/claim", bountyID)

	recorder = doRequest(t, f.router, "POST", claimPath, nil, map[string]string{"Idempotency-Key": "c-" + uuid.NewString()})
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated claim = %d, want 401", recorder.Code)
	}
	noAddress := f.cookie(t, f.user(t, ""))
	recorder = doRequest(t, f.router, "POST", claimPath, nil, map[string]string{"Idempotency-Key": "c-" + uuid.NewString()}, noAddress)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("claim without destination = %d, want 422", recorder.Code)
	}
	contribID := f.user(t, "contrib@example.test")
	contribCookie := f.cookie(t, contribID)
	recorder = doRequest(t, f.router, "POST", claimPath, nil, map[string]string{"Idempotency-Key": "c-" + uuid.NewString()}, contribCookie)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("claim = %d (%s), want 201", recorder.Code, recorder.Body.String())
	}
	recorder = doRequest(t, f.router, "POST", claimPath, nil, map[string]string{"Idempotency-Key": "c-" + uuid.NewString()}, contribCookie)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("second claim = %d, want 409", recorder.Code)
	}
	recorder = doRequest(t, f.router, "GET", "/api/v1/me/claims", nil, nil, contribCookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("get my claims = %d, want 200", recorder.Code)
	}
	var claims []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &claims); err != nil || len(claims) != 1 {
		t.Fatalf("my claims = %s, want exactly 1", recorder.Body.String())
	}
	recorder = doRequest(t, f.router, "DELETE", claimPath, nil, nil, contribCookie)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("release claim = %d (%s), want 204", recorder.Code, recorder.Body.String())
	}
	recorder = doRequest(t, f.router, "GET", "/api/v1/me/payouts", nil, nil, contribCookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("get my payouts = %d, want 200", recorder.Code)
	}
}

func TestIntegrationAPIDisputeResolve(t *testing.T) {
	f := newAPIFixture(t)
	ownerCookie := f.cookie(t, f.ownerID)
	projectID := f.project(t, f.ownerID, 100000)
	recorder := doRequest(t, f.router, "POST", fmt.Sprintf("/api/v1/projects/%d/bounties", projectID),
		map[string]any{"github_issue_number": 31, "title": "Disputed", "description": "dispute",
			"difficulty": "easy", "reward_sats": 5000},
		map[string]string{"Idempotency-Key": "b-" + uuid.NewString()}, ownerCookie)
	bountyID := int64(decodeBody(t, recorder)["id"].(float64))
	contribCookie := f.cookie(t, f.user(t, "c@example.test"))

	recorder = doRequest(t, f.router, "POST", fmt.Sprintf("/api/v1/bounties/%d/dispute", bountyID),
		map[string]any{"reason": "scope unclear"}, nil, contribCookie)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("dispute = %d (%s), want 201", recorder.Code, recorder.Body.String())
	}
	recorder = doRequest(t, f.router, "GET", fmt.Sprintf("/api/v1/bounties/%d", bountyID), nil, nil)
	if recorder.Code != http.StatusOK || decodeBody(t, recorder)["state"] != "disputed" {
		t.Fatalf("bounty after dispute = %d %s, want 200 disputed", recorder.Code, recorder.Body.String())
	}
	var disputeID int64
	if err := f.pool.QueryRow(context.Background(), `SELECT id FROM disputes WHERE bounty_id = $1`, bountyID).Scan(&disputeID); err != nil {
		t.Fatal(err)
	}
	resolvePath := fmt.Sprintf("/api/v1/disputes/%d/resolve", disputeID)
	recorder = doRequest(t, f.router, "POST", resolvePath, map[string]any{"resolution": "resolved_cancel"}, nil, contribCookie)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-owner resolve = %d, want 403", recorder.Code)
	}
	recorder = doRequest(t, f.router, "POST", resolvePath, map[string]any{"resolution": "resolved_cancel"}, nil, ownerCookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("owner resolve = %d (%s), want 200", recorder.Code, recorder.Body.String())
	}
	recorder = doRequest(t, f.router, "POST", resolvePath, map[string]any{"resolution": "resolved_cancel"}, nil, ownerCookie)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("second resolve = %d, want 409", recorder.Code)
	}
}

func TestIntegrationAPIGitHubWebhookDedupes(t *testing.T) {
	f := newAPIFixture(t)
	payload := []byte(`{"action":"opened","number":1,"repository":{"full_name":"unknown/repo"}}`)
	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write(payload)
	headers := map[string]string{
		"X-GitHub-Delivery":   "delivery-" + uuid.NewString(),
		"X-GitHub-Event":      "pull_request",
		"X-Hub-Signature-256": "sha256=" + hex.EncodeToString(mac.Sum(nil)),
	}
	recorder := doRequest(t, f.router, "POST", "/api/v1/webhooks/github", payload, headers)
	if recorder.Code != http.StatusOK {
		t.Fatalf("webhook = %d (%s), want 200", recorder.Code, recorder.Body.String())
	}
	recorder = doRequest(t, f.router, "POST", "/api/v1/webhooks/github", payload, headers)
	if recorder.Code != http.StatusOK {
		t.Fatalf("webhook redelivery = %d, want 200", recorder.Code)
	}
	var events, jobs int
	deliveryID := headers["X-GitHub-Delivery"]
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM webhook_events WHERE delivery_id = $1`, deliveryID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM river_job WHERE kind = 'process_webhook' AND args->>'delivery_id' = $1`, deliveryID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if events != 1 || jobs != 1 {
		t.Fatalf("events=%d jobs=%d, want exactly 1 of each", events, jobs)
	}
	badHeaders := map[string]string{
		"X-GitHub-Delivery":   "delivery-" + uuid.NewString(),
		"X-GitHub-Event":      "pull_request",
		"X-Hub-Signature-256": "sha256=deadbeef",
	}
	recorder = doRequest(t, f.router, "POST", "/api/v1/webhooks/github", payload, badHeaders)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("forged webhook = %d, want 401", recorder.Code)
	}
}

func TestIntegrationAPIWithdrawCallbackValidation(t *testing.T) {
	f := newAPIFixture(t)
	recorder := doRequest(t, f.router, "GET", "/api/v1/payouts/claim/does-not-exist", nil, nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown claim link = %d, want 404", recorder.Code)
	}
	recorder = doRequest(t, f.router, "GET", "/api/v1/payouts/claim/callback", nil, nil)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("callback without params = %d, want 400", recorder.Code)
	}
	recorder = doRequest(t, f.router, "GET", "/healthz", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("healthz = %d, want 200", recorder.Code)
	}
	recorder = doRequest(t, f.router, "GET", "/metrics", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("metrics = %d, want 200", recorder.Code)
	}
}
