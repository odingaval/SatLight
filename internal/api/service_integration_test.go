//go:build integration

package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
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

func TestIntegrationAPIDestinationChangeConfirmFlow(t *testing.T) {
	f := newAPIFixture(t)
	ctx := context.Background()
	userID := f.user(t, "old@example.test")
	cookie := f.cookie(t, userID)

	// Malformed addresses are rejected before any session or email work.
	recorder := doRequest(t, f.router, "PUT", "/api/v1/me/payout-destination",
		map[string]any{"lightning_address": "not-an-address"}, nil, cookie)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("malformed address = %d, want 400", recorder.Code)
	}

	// Stale sessions must sign in again.
	staleToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &auth.Claims{
		UserID:   userID,
		AuthTime: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}).SignedString([]byte("test-session-secret"))
	if err != nil {
		t.Fatal(err)
	}
	recorder = doRequest(t, f.router, "PUT", "/api/v1/me/payout-destination",
		map[string]any{"lightning_address": "new@example.test"}, nil,
		&http.Cookie{Name: "satlight_session", Value: staleToken})
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("stale session = %d, want 401", recorder.Code)
	}
	if code := decodeBody(t, recorder)["error"].(map[string]any)["code"]; code != "REAUTH_REQUIRED" {
		t.Fatalf("stale session code = %v, want REAUTH_REQUIRED", code)
	}

	// Repeating the current address is a no-op success.
	recorder = doRequest(t, f.router, "PUT", "/api/v1/me/payout-destination",
		map[string]any{"lightning_address": "old@example.test"}, nil, cookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unchanged address = %d (%s), want 200", recorder.Code, recorder.Body.String())
	}

	// A new address stages a request and emails a confirmation link.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	bodies := make(chan string, 4)
	serveFakeSMTP(t, listener, bodies)
	smtpCfg := *f.cfg
	smtpCfg.SMTPURL = "smtp://" + listener.Addr().String()
	router := NewServer(&smtpCfg, f.pool).Router()

	recorder = doRequest(t, router, "PUT", "/api/v1/me/payout-destination",
		map[string]any{"lightning_address": "new@example.test"}, nil, cookie)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("stage change = %d (%s), want 202", recorder.Code, recorder.Body.String())
	}
	var current string
	if err := f.pool.QueryRow(ctx, `SELECT lightning_address FROM users WHERE id = $1`, userID).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current != "old@example.test" {
		t.Fatalf("address changed before confirm: %q", current)
	}
	var emailed string
	select {
	case emailed = <-bodies:
	case <-time.After(10 * time.Second):
		t.Fatal("confirmation email was not sent")
	}
	const marker = "/settings/confirm?token="
	idx := strings.Index(emailed, marker)
	if idx < 0 {
		t.Fatalf("email has no confirm link:\n%s", emailed)
	}
	token := strings.Fields(emailed[idx+len(marker):])[0]

	recorder = doRequest(t, f.router, "POST", "/api/v1/me/payout-destination/confirm",
		map[string]any{"token": token}, nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated confirm = %d, want 401", recorder.Code)
	}
	recorder = doRequest(t, f.router, "POST", "/api/v1/me/payout-destination/confirm",
		map[string]any{"token": "bogus-token"}, nil, cookie)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("bogus token = %d, want 400", recorder.Code)
	}
	recorder = doRequest(t, f.router, "POST", "/api/v1/me/payout-destination/confirm",
		map[string]any{"token": token}, nil, cookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("confirm = %d (%s), want 200", recorder.Code, recorder.Body.String())
	}
	var changedAt time.Time
	if err := f.pool.QueryRow(ctx, `SELECT lightning_address, payout_changed_at FROM users WHERE id = $1`, userID).Scan(&current, &changedAt); err != nil {
		t.Fatal(err)
	}
	if current != "new@example.test" || time.Since(changedAt) > time.Minute {
		t.Fatalf("after confirm: address=%q changed_at=%v", current, changedAt)
	}
	var audits int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'payout_destination.confirmed' AND subject = $1`,
		fmt.Sprintf("user:%d", userID)).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("confirmation audits = %d, want 1", audits)
	}
	recorder = doRequest(t, f.router, "POST", "/api/v1/me/payout-destination/confirm",
		map[string]any{"token": token}, nil, cookie)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("second confirm = %d, want 400", recorder.Code)
	}

	// Expired and foreign requests are rejected.
	expiredSum := sha256.Sum256([]byte("expired-token"))
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO destination_change_requests (user_id, new_address, token_hash, expires_at)
		VALUES ($1, 'expired@example.test', $2, now() - interval '1 hour')
	`, userID, expiredSum[:]); err != nil {
		t.Fatal(err)
	}
	recorder = doRequest(t, f.router, "POST", "/api/v1/me/payout-destination/confirm",
		map[string]any{"token": "expired-token"}, nil, cookie)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expired token = %d, want 400", recorder.Code)
	}
	foreignSum := sha256.Sum256([]byte("foreign-token"))
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO destination_change_requests (user_id, new_address, token_hash, expires_at)
		VALUES ($1, 'foreign@example.test', $2, now() + interval '1 hour')
	`, f.ownerID, foreignSum[:]); err != nil {
		t.Fatal(err)
	}
	recorder = doRequest(t, f.router, "POST", "/api/v1/me/payout-destination/confirm",
		map[string]any{"token": "foreign-token"}, nil, cookie)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("foreign token = %d, want 400", recorder.Code)
	}
}

// serveFakeSMTP accepts mail for tests and delivers message bodies to bodies.
// It speaks just enough SMTP for net/smtp.SendMail without auth or TLS.
func serveFakeSMTP(t *testing.T, listener net.Listener, bodies chan<- string) {
	t.Helper()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				rw := bufio.NewReadWriter(bufio.NewReader(c), bufio.NewWriter(c))
				_, _ = rw.WriteString("220 satlight-test smtp\r\n")
				_ = rw.Flush()
				var data strings.Builder
				inData := false
				for {
					line, err := rw.ReadString('\n')
					if err != nil {
						return
					}
					trimmed := strings.TrimRight(line, "\r\n")
					if inData {
						if trimmed == "." {
							inData = false
							_, _ = rw.WriteString("250 queued\r\n")
							_ = rw.Flush()
							select {
							case bodies <- data.String():
							default:
							}
							data.Reset()
						} else {
							data.WriteString(line)
						}
						continue
					}
					switch upper := strings.ToUpper(trimmed); {
					case strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO"):
						_, _ = rw.WriteString("250 hello\r\n")
					case strings.HasPrefix(upper, "DATA"):
						inData = true
						_, _ = rw.WriteString("354 end with .\r\n")
					case strings.HasPrefix(upper, "QUIT"):
						_, _ = rw.WriteString("221 bye\r\n")
						_ = rw.Flush()
						return
					default:
						_, _ = rw.WriteString("250 ok\r\n")
					}
					_ = rw.Flush()
				}
			}(conn)
		}
	}()
}

func TestIntegrationAPIRateLimiting(t *testing.T) {
	f := newAPIFixture(t)
	limited := *f.cfg
	limited.RateLimitWaitlistPerMinute = 2
	limited.RateLimitWaitlistBurst = 2
	limited.RateLimitClaimsPerMinute = 2
	limited.RateLimitClaimsBurst = 2
	router := NewServer(&limited, f.pool).Router()

	// Anonymous waitlist joins share one IP bucket: two pass, the third is
	// held with a Retry-After header.
	for i := 0; i < 2; i++ {
		recorder := doRequest(t, router, "POST", "/api/v1/waitlist",
			map[string]any{"email": fmt.Sprintf("ratelimit-%d-%d@example.test", time.Now().UnixNano(), i), "role": "contributor"}, nil)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("waitlist join %d = %d (%s), want 204", i, recorder.Code, recorder.Body.String())
		}
	}
	recorder := doRequest(t, router, "POST", "/api/v1/waitlist",
		map[string]any{"email": fmt.Sprintf("ratelimit-%d-x@example.test", time.Now().UnixNano()), "role": "contributor"}, nil)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("third waitlist join = %d, want 429", recorder.Code)
	}
	if recorder.Header().Get("Retry-After") == "" {
		t.Fatal("429 is missing the Retry-After header")
	}
	if code := decodeBody(t, recorder)["error"].(map[string]any)["code"]; code != "RATE_LIMITED" {
		t.Fatalf("429 code = %v, want RATE_LIMITED", code)
	}

	// Authenticated claims are keyed per user: exhausting one user's budget
	// (against a missing bounty, so no fixture bounty is needed) must not
	// affect another user from the same IP.
	userA := f.user(t, "ratelimit-a@example.test")
	userB := f.user(t, "ratelimit-b@example.test")
	cookieA := f.cookie(t, userA)
	cookieB := f.cookie(t, userB)
	claim := func(cookie *http.Cookie) *httptest.ResponseRecorder {
		return doRequest(t, router, "POST", "/api/v1/bounties/999999999/claim", nil,
			map[string]string{"Idempotency-Key": uuid.NewString()}, cookie)
	}
	for i := 0; i < 2; i++ {
		if recorder := claim(cookieA); recorder.Code != http.StatusNotFound {
			t.Fatalf("user A claim %d = %d, want 404 (missing bounty)", i, recorder.Code)
		}
	}
	if recorder := claim(cookieA); recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("user A third claim = %d, want 429", recorder.Code)
	}
	if recorder := claim(cookieB); recorder.Code != http.StatusNotFound {
		t.Fatalf("user B claim = %d, want 404 (independent budget)", recorder.Code)
	}
}

func TestIntegrationAPIStatusReflectsPauseFlags(t *testing.T) {
	f := newAPIFixture(t)
	ctx := context.Background()

	recorder := doRequest(t, f.router, "GET", "/api/v1/status", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := decodeBody(t, recorder)
	if body["funding_paused"] != false || body["outflows_paused"] != false {
		t.Fatalf("status = %v, want all running (fixture clears flags)", body)
	}
	if _, hasMessage := body["message"]; hasMessage {
		t.Fatalf("running status carries message %q, want none", body["message"])
	}

	if _, err := f.pool.Exec(ctx, `UPDATE system_flags SET enabled = TRUE WHERE name IN ('funding_paused','outflows_paused')`); err != nil {
		t.Fatal(err)
	}
	recorder = doRequest(t, f.router, "GET", "/api/v1/status", nil, nil)
	body = decodeBody(t, recorder)
	if body["funding_paused"] != true || body["outflows_paused"] != true {
		t.Fatalf("status = %v, want both paused", body)
	}
	if body["message"] == nil || body["message"] == "" {
		t.Fatal("paused status has no user message")
	}
}

func TestIntegrationAPIListProjects(t *testing.T) {
	f := newAPIFixture(t)
	owner := f.user(t, "projects-owner@example.test")
	other := f.user(t, "projects-other@example.test")
	first := f.project(t, owner, 0)
	second := f.project(t, owner, 0)
	f.project(t, other, 0)

	recorder := doRequest(t, f.router, "GET", "/api/v1/projects", nil, nil, f.cookie(t, owner))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/projects = %d, want 200", recorder.Code)
	}
	items, _ := decodeBody(t, recorder)["projects"].([]any)
	if len(items) != 2 {
		t.Fatalf("projects = %d items, want 2 (own only)", len(items))
	}
	// Newest first.
	if got := int64(items[0].(map[string]any)["id"].(float64)); got != second {
		t.Fatalf("first item id = %d, want newest project %d", got, second)
	}
	if got := int64(items[1].(map[string]any)["id"].(float64)); got != first {
		t.Fatalf("second item id = %d, want oldest project %d", got, first)
	}
	if repo := items[0].(map[string]any)["repo_full_name"].(string); repo == "" {
		t.Fatal("project is missing repo_full_name")
	}

	recorder = doRequest(t, f.router, "GET", "/api/v1/projects", nil, nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /api/v1/projects = %d, want 401", recorder.Code)
	}
}
