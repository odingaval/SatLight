//go:build integration

package payout

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/satlight/satlight/internal/config"
	"github.com/satlight/satlight/internal/ledger"
	"github.com/satlight/satlight/internal/notify"
	"github.com/satlight/satlight/internal/queue"
)

type fakeLightning struct {
	payFunc    func(invoice string, maxFee int64) ([]byte, error)
	trackFunc  func(hash []byte) (string, int64, error)
	decodeFunc func(pr string) (*lnrpc.PayReq, error)
	sends      int
}

func (f *fakeLightning) PayInvoice(ctx context.Context, invoice string, maxFeeSats int64) ([]byte, error) {
	f.sends++
	return f.payFunc(invoice, maxFeeSats)
}

func (f *fakeLightning) GetPaymentResult(ctx context.Context, paymentHash []byte) (string, int64, error) {
	return f.trackFunc(paymentHash)
}

func (f *fakeLightning) DecodePayReq(ctx context.Context, payReq string) (*lnrpc.PayReq, error) {
	return f.decodeFunc(payReq)
}

type fakeMergeVerifier struct {
	fresh bool
	err   error
	calls int
}

func (f *fakeMergeVerifier) VerifyMergeFresh(ctx context.Context, bountyID int64) (bool, error) {
	f.calls++
	return f.fresh, f.err
}

type payoutFixture struct {
	service      *Service
	pool         *pgxpool.Pool
	projectID    int64
	ownerID      int64
	contribID    int64
	bountyID     int64
	rewardSats   int64
	feeSats      int64
	lightning    *fakeLightning
	merge        *fakeMergeVerifier
	lnurlAmounts []int64
}

func testConfig() *config.Config {
	return &config.Config{
		MinRewardSats: 500, ClaimExpiryDays: 7, BountyDefaultExpiryDays: 90,
		DailyPayoutCapSats: 1000000, PayoutMaxFeePPM: 10000, PayoutMaxFeeFloorSats: 10,
		BaseURL: "http://localhost:8080", WebURL: "http://localhost:3000",
		SMTPURL: "smtp://localhost:1025", EmailFrom: "noreply@test",
	}
}

func newPayoutFixture(t *testing.T) *payoutFixture {
	t.Helper()
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

	unique := uuid.NewString()
	now := time.Now().UnixNano()
	var ownerID, contribID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (github_id, github_login, email) VALUES ($1, $2, $3) RETURNING id
	`, now, "owner-"+unique[:8], "owner-"+unique[:8]+"@example.test").Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (github_id, github_login, email, lightning_address) VALUES ($1, $2, $3, $4) RETURNING id
	`, now+1, "contrib-"+unique[:8], "contrib-"+unique[:8]+"@example.test", "placeholder@example.test").Scan(&contribID); err != nil {
		t.Fatal(err)
	}
	var projectID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (owner_user_id, github_installation_id, repo_full_name) VALUES ($1, $2, $3) RETURNING id
	`, ownerID, now+2, "payout/"+unique[:8]).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	const reward, fee = 5000, 100
	if _, err := pool.Exec(ctx, `INSERT INTO wallets (project_id, available_sats, reserved_sats) VALUES ($1, 95000, $2)`, projectID, reward); err != nil {
		t.Fatal(err)
	}
	var bountyID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO bounties (project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at)
		VALUES ($1, 901, 'Merged work', 'pay me', 'easy', $2, $3, 'merged', now() + interval '1 day') RETURNING id
	`, projectID, reward, fee).Scan(&bountyID); err != nil {
		t.Fatal(err)
	}
	var claimID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO claims (bounty_id, user_id, status, expires_at) VALUES ($1, $2, 'active', now() + interval '1 day') RETURNING id
	`, bountyID, contribID).Scan(&claimID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO pull_requests (claim_id, github_pr_number, author_github_id, merged, merged_at, merged_by_github_id)
		VALUES ($1, 3, $2, true, now(), $3)
	`, claimID, now+1, now); err != nil {
		t.Fatal(err)
	}

	fx := &payoutFixture{
		pool: pool, projectID: projectID, ownerID: ownerID, contribID: contribID,
		bountyID: bountyID, rewardSats: reward, feeSats: fee,
		lightning: &fakeLightning{}, merge: &fakeMergeVerifier{fresh: true},
	}
	svc := NewService(testConfig(), pool)
	svc.SetDependencies(ledger.NewService(nil, pool), fx.lightning, fx.merge, notify.NewService(testConfig(), pool), queue.NewClient(pool))
	fx.service = svc
	return fx
}

// useMockLNURL points the contributor's Lightning address at a test LNURL-pay
// server and records every requested amount.
func (f *payoutFixture) useMockLNURL(t *testing.T) {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/.well-known/lnurlp/") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"callback": server.URL + "/cb", "minSendable": 1000,
				"maxSendable": 1000000000, "tag": "payRequest", "metadata": "[]",
			})
			return
		}
		var msats int64
		_, _ = fmt.Sscanf(r.URL.Query().Get("amount"), "%d", &msats)
		f.lnurlAmounts = append(f.lnurlAmounts, msats)
		_ = json.NewEncoder(w).Encode(map[string]any{"pr": fmt.Sprintf("lnbc%dmockinvoice", msats/1000), "routes": []any{}})
	}))
	t.Cleanup(server.Close)
	f.service.http = server.Client()
	domain := strings.TrimPrefix(server.URL, "https://")
	if _, err := f.pool.Exec(context.Background(), `UPDATE users SET lightning_address = $1 WHERE id = $2`,
		"contrib@"+domain, f.contribID); err != nil {
		t.Fatal(err)
	}
}

func (f *payoutFixture) payoutRow(t *testing.T) (status string, attempts int64, token *string) {
	t.Helper()
	err := f.pool.QueryRow(context.Background(),
		`SELECT status, attempts, claim_link_token FROM payouts WHERE bounty_id = $1`, f.bountyID).Scan(&status, &attempts, &token)
	if err != nil {
		t.Fatal(err)
	}
	return status, attempts, token
}

func (f *payoutFixture) bountyState(t *testing.T) string {
	t.Helper()
	var state string
	if err := f.pool.QueryRow(context.Background(), `SELECT state FROM bounties WHERE id = $1`, f.bountyID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func testHash(seed byte) []byte {
	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = seed + byte(i)
	}
	return hash
}

func TestIntegrationPayoutHappyPath(t *testing.T) {
	f := newPayoutFixture(t)
	f.useMockLNURL(t)
	ctx := context.Background()
	sentHash := testHash(1)
	netAmount := f.rewardSats - f.feeSats
	f.lightning.decodeFunc = func(pr string) (*lnrpc.PayReq, error) {
		return &lnrpc.PayReq{NumSatoshis: netAmount, PaymentHash: hex.EncodeToString(sentHash)}, nil
	}
	var sentInvoice string
	var sentMaxFee int64
	f.lightning.payFunc = func(invoice string, maxFee int64) ([]byte, error) {
		sentInvoice, sentMaxFee = invoice, maxFee
		return sentHash, nil
	}
	f.lightning.trackFunc = func(hash []byte) (string, int64, error) { return "SUCCEEDED", 2, nil }

	if err := f.service.ProcessBountyPayout(ctx, f.bountyID); err != nil {
		t.Fatal(err)
	}
	if sentInvoice == "" {
		t.Fatal("no invoice was paid")
	}
	if sentMaxFee != 49 {
		t.Fatalf("fee limit = %d, want 49 (1%% of %d)", sentMaxFee, netAmount)
	}
	if len(f.lnurlAmounts) != 1 || f.lnurlAmounts[0] != netAmount*1000 {
		t.Fatalf("LNURL amounts = %v, want [%d]", f.lnurlAmounts, netAmount*1000)
	}
	if status, _, _ := f.payoutRow(t); status != "settled" {
		t.Fatalf("payout status = %q, want settled", status)
	}
	if state := f.bountyState(t); state != "paid" {
		t.Fatalf("bounty state = %q, want paid", state)
	}
	var claimStatus string
	if err := f.pool.QueryRow(ctx, `SELECT status FROM claims WHERE bounty_id = $1`, f.bountyID).Scan(&claimStatus); err != nil {
		t.Fatal(err)
	}
	if claimStatus != "completed" {
		t.Fatalf("claim status = %q, want completed", claimStatus)
	}
	var debits, credits int64
	if err := f.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN direction = 'debit' THEN amount_sats END), 0),
		       COALESCE(SUM(CASE WHEN direction = 'credit' THEN amount_sats END), 0)
		FROM ledger_entries WHERE ref_type = 'payout' AND ref_id = $1
	`, f.bountyID).Scan(&debits, &credits); err != nil {
		t.Fatal(err)
	}
	if debits != credits || debits != f.rewardSats+2 {
		t.Fatalf("payout ledger debits=%d credits=%d, want balanced %d", debits, credits, f.rewardSats+2)
	}
	var notifications int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE kind = 'payout_settled' AND ref_id = $1`, f.bountyID).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if notifications != 1 {
		t.Fatalf("payout_settled notifications = %d, want 1", notifications)
	}
	// Second run is a no-op: exactly one send, one payout row.
	if err := f.service.ProcessBountyPayout(ctx, f.bountyID); err != nil {
		t.Fatal(err)
	}
	if f.lightning.sends != 1 {
		t.Fatalf("lightning sends = %d, want exactly 1", f.lightning.sends)
	}
}

func TestIntegrationPayoutRecoversByHashAfterCrash(t *testing.T) {
	f := newPayoutFixture(t)
	ctx := context.Background()
	sentHash := testHash(7)
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO payouts (bounty_id, user_id, amount_sats, fee_sats, destination, status, payment_hash, attempts)
		VALUES ($1, $2, $3, $4, 'crashed@example.test', 'sending', $5, 1)
	`, f.bountyID, f.contribID, f.rewardSats-f.feeSats, f.feeSats, sentHash); err != nil {
		t.Fatal(err)
	}
	f.lightning.trackFunc = func(hash []byte) (string, int64, error) { return "SUCCEEDED", 0, nil }
	f.lightning.payFunc = func(invoice string, maxFee int64) ([]byte, error) {
		t.Fatal("recovery must not send again")
		return nil, nil
	}
	if err := f.service.ProcessBountyPayout(ctx, f.bountyID); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := f.payoutRow(t); status != "settled" {
		t.Fatalf("payout status = %q, want settled", status)
	}
	if state := f.bountyState(t); state != "paid" {
		t.Fatalf("bounty state = %q, want paid", state)
	}
}

func TestIntegrationPayoutFailuresBackOffThenClaimLink(t *testing.T) {
	f := newPayoutFixture(t)
	f.useMockLNURL(t)
	ctx := context.Background()
	netAmount := f.rewardSats - f.feeSats
	f.lightning.decodeFunc = func(pr string) (*lnrpc.PayReq, error) {
		return &lnrpc.PayReq{NumSatoshis: netAmount, PaymentHash: hex.EncodeToString(testHash(9))}, nil
	}
	f.lightning.payFunc = func(invoice string, maxFee int64) ([]byte, error) { return nil, errors.New("no route") }
	f.lightning.trackFunc = func(hash []byte) (string, int64, error) { return "FAILED", 0, nil }

	for i := 1; i <= 4; i++ {
		if err := f.service.ProcessBountyPayout(ctx, f.bountyID); err != nil {
			t.Fatal(err)
		}
		status, attempts, _ := f.payoutRow(t)
		if status != "failed" || attempts != int64(i) {
			t.Fatalf("after failure %d: status=%q attempts=%d", i, status, attempts)
		}
	}
	var jobs int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'process_payout' AND args->>'bounty_id' = $1`,
		fmt.Sprint(f.bountyID)).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 4 {
		t.Fatalf("retry jobs = %d, want 4", jobs)
	}
	if err := f.service.ProcessBountyPayout(ctx, f.bountyID); err != nil {
		t.Fatal(err)
	}
	status, attempts, token := f.payoutRow(t)
	if status != "claim_link_issued" || attempts != 5 || token == nil || *token == "" {
		t.Fatalf("after final failure: status=%q attempts=%d token set=%t", status, attempts, token != nil)
	}
	var notifications int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE kind = 'payout_claim_link'`).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if notifications < 1 {
		t.Fatal("no claim-link notification recorded")
	}
	if state := f.bountyState(t); state != "merged" {
		t.Fatalf("bounty state = %q, want merged (funds still reserved)", state)
	}
}

func TestIntegrationPayoutHoldsSelfMergeWithClaimLink(t *testing.T) {
	f := newPayoutFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `
		UPDATE pull_requests SET merged_by_github_id = author_github_id
		WHERE claim_id = (SELECT id FROM claims WHERE bounty_id = $1)
	`, f.bountyID); err != nil {
		t.Fatal(err)
	}
	f.lightning.payFunc = func(invoice string, maxFee int64) ([]byte, error) {
		t.Fatal("self-merge must not auto-pay")
		return nil, nil
	}
	if err := f.service.ProcessBountyPayout(ctx, f.bountyID); err != nil {
		t.Fatal(err)
	}
	status, _, token := f.payoutRow(t)
	if status != "claim_link_issued" || token == nil {
		t.Fatalf("self-merge: status=%q token set=%t, want claim_link_issued", status, token != nil)
	}
}

func TestIntegrationPayoutHeldWhenMergeReverted(t *testing.T) {
	f := newPayoutFixture(t)
	f.merge.fresh = false
	if err := f.service.ProcessBountyPayout(context.Background(), f.bountyID); err != nil {
		t.Fatal(err)
	}
	var payouts int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM payouts WHERE bounty_id = $1`, f.bountyID).Scan(&payouts); err != nil {
		t.Fatal(err)
	}
	if payouts != 0 {
		t.Fatalf("payouts created = %d, want 0 for reverted merge", payouts)
	}
	var notifications int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM notifications WHERE kind = 'merge_reverted' AND ref_id = $1`, f.bountyID).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if notifications != 1 {
		t.Fatalf("merge_reverted notifications = %d, want 1", notifications)
	}
}

func TestIntegrationPayoutRespectsPauseAndCap(t *testing.T) {
	f := newPayoutFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE system_flags SET enabled = TRUE WHERE name = 'outflows_paused'`); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ProcessBountyPayout(ctx, f.bountyID); !errors.Is(err, ErrOutflowsPaused) {
		t.Fatalf("paused outflows: err=%v, want ErrOutflowsPaused", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE system_flags SET enabled = FALSE WHERE name = 'outflows_paused'`); err != nil {
		t.Fatal(err)
	}
	capped := NewService(testConfig(), f.pool)
	cappedCfg := testConfig()
	cappedCfg.DailyPayoutCapSats = 100
	capped.cfg = cappedCfg
	capped.SetDependencies(ledger.NewService(nil, f.pool), f.lightning, f.merge, notify.NewService(testConfig(), f.pool), queue.NewClient(f.pool))
	if err := capped.ProcessBountyPayout(ctx, f.bountyID); !errors.Is(err, ledger.ErrDailyPayoutCapExceeded) {
		t.Fatalf("exceeded cap: err=%v, want ErrDailyPayoutCapExceeded", err)
	}
}

func TestIntegrationPayoutDelaysFreshDestination(t *testing.T) {
	f := newPayoutFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE users SET payout_changed_at = now() WHERE id = $1`, f.contribID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ProcessBountyPayout(ctx, f.bountyID); err != nil {
		t.Fatal(err)
	}
	var payouts int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM payouts WHERE bounty_id = $1`, f.bountyID).Scan(&payouts); err != nil {
		t.Fatal(err)
	}
	if payouts != 0 {
		t.Fatalf("payouts created = %d, want 0 during destination cooldown", payouts)
	}
	var scheduledAt time.Time
	if err := f.pool.QueryRow(ctx, `SELECT scheduled_at FROM river_job WHERE kind = 'process_payout' AND args->>'bounty_id' = $1 ORDER BY id DESC LIMIT 1`,
		fmt.Sprint(f.bountyID)).Scan(&scheduledAt); err != nil {
		t.Fatal(err)
	}
	if time.Until(scheduledAt) < 47*time.Hour {
		t.Fatalf("cooldown retry scheduled at %v, want ~48h out", scheduledAt)
	}
}

func TestIntegrationClaimLinkWithdrawFlow(t *testing.T) {
	f := newPayoutFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `
		UPDATE pull_requests SET merged_by_github_id = author_github_id
		WHERE claim_id = (SELECT id FROM claims WHERE bounty_id = $1)
	`, f.bountyID); err != nil {
		t.Fatal(err)
	}
	f.lightning.payFunc = func(invoice string, maxFee int64) ([]byte, error) {
		t.Fatal("self-merge must not auto-pay")
		return nil, nil
	}
	if err := f.service.ProcessBountyPayout(ctx, f.bountyID); err != nil {
		t.Fatal(err)
	}
	_, _, token := f.payoutRow(t)
	req, err := f.service.GetWithdrawRequest(ctx, *token)
	if err != nil {
		t.Fatal(err)
	}
	netAmount := f.rewardSats - f.feeSats
	if req.MinWithdrawableMs != netAmount*1000 || req.MaxWithdrawableMs != netAmount*1000 {
		t.Fatalf("withdraw bounds = %d/%d msats, want %d", req.MinWithdrawableMs, req.MaxWithdrawableMs, netAmount*1000)
	}
	if !strings.HasSuffix(req.CallbackURL, "/api/v1/payouts/claim/callback") {
		t.Fatalf("callback URL = %q", req.CallbackURL)
	}
	sentHash := testHash(3)
	calls := 0
	f.lightning.decodeFunc = func(pr string) (*lnrpc.PayReq, error) {
		calls++
		if pr == "lnbc_wrong_amount" {
			return &lnrpc.PayReq{NumSatoshis: 1, PaymentHash: hex.EncodeToString(sentHash)}, nil
		}
		return &lnrpc.PayReq{NumSatoshis: netAmount, PaymentHash: hex.EncodeToString(sentHash)}, nil
	}
	if err := f.service.RedeemClaimLink(ctx, *token, "lnbc_wrong_amount"); !errors.Is(err, ErrInvoiceAmountMismatch) {
		t.Fatalf("wrong-amount invoice: err=%v, want ErrInvoiceAmountMismatch", err)
	}
	f.lightning.payFunc = func(invoice string, maxFee int64) ([]byte, error) { return sentHash, nil }
	f.lightning.trackFunc = func(hash []byte) (string, int64, error) { return "SUCCEEDED", 1, nil }
	if err := f.service.RedeemClaimLink(ctx, *token, "lnbc_correct"); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := f.payoutRow(t); status != "claimed_via_link" {
		t.Fatalf("payout status = %q, want claimed_via_link", status)
	}
	if state := f.bountyState(t); state != "paid" {
		t.Fatalf("bounty state = %q, want paid", state)
	}
	if err := f.service.RedeemClaimLink(ctx, *token, "lnbc_correct"); !errors.Is(err, ErrClaimLinkUsed) {
		t.Fatalf("second redemption: err=%v, want ErrClaimLinkUsed", err)
	}
	if calls != 3 {
		t.Fatalf("decode calls = %d, want 3", calls)
	}
}
