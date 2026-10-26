//go:build integration

package bounty

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/satlight/satlight/internal/config"
	"github.com/satlight/satlight/internal/ledger"
)

func integrationFixture(t *testing.T) (*Service, *pgxpool.Pool, int64) {
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

	unique := uuid.NewString()
	var userID, projectID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (github_id, github_login, email)
		VALUES ($1, $2, $3) RETURNING id
	`, time.Now().UnixNano(), "integration-"+unique[:8], "test-"+unique[:8]+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (owner_user_id, github_installation_id, repo_full_name)
		VALUES ($1, $2, $3) RETURNING id
	`, userID, time.Now().UnixNano(), "integration/"+unique[:8]).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO wallets (project_id) VALUES ($1)`, projectID); err != nil {
		t.Fatal(err)
	}
	service := NewService(&config.Config{}, pool)
	service.SetDependencies(ledger.NewService(nil, pool), nil)
	return service, pool, projectID
}

func TestIntegrationExpiredUnderpaidDepositCreditsActualAmountExactlyOnce(t *testing.T) {
	service, pool, projectID := integrationFixture(t)
	ctx := context.Background()
	hash := uuid.New()
	deposit, err := service.CreateDeposit(ctx, projectID, "integration-deposit-"+hash.String(), "test-invoice", hash[:], 1000, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.MarkDepositExpired(ctx, hash[:]); err != nil {
		t.Fatal(err)
	}
	if err := service.SettleDeposit(ctx, hash[:], 950); err != nil {
		t.Fatal(err)
	}
	if err := service.SettleDeposit(ctx, hash[:], 950); err != nil {
		t.Fatal(err)
	}

	got, err := service.GetDepositByIdempotencyKey(ctx, projectID, "integration-deposit-"+hash.String())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != deposit.ID || got.Status != "settled" || !got.SettledLate || got.ReceivedSats == nil || *got.ReceivedSats != 950 {
		t.Fatalf("settled deposit = %#v, want settled late with 950 sats received", got)
	}
	var available, entries int64
	if err := pool.QueryRow(ctx, `SELECT available_sats FROM wallets WHERE project_id = $1`, projectID).Scan(&available); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE ref_type = 'deposit' AND ref_id = $1`, deposit.ID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if available != 950 || entries != 2 {
		t.Fatalf("wallet=%d sats, ledger entries=%d; want wallet=950 and exactly 2 ledger entries", available, entries)
	}

	ledgerService := ledger.NewService(nil, pool)
	ledgerService.SetNodeBalanceProvider(staticNodeBalance(0))
	if err := ledgerService.RunReconciliation(ctx); err == nil {
		t.Fatal("shortfall reconciliation should fail and set pause flags")
	}
	var fundingPaused, outflowsPaused bool
	if err := pool.QueryRow(ctx, `SELECT enabled FROM system_flags WHERE name = 'funding_paused'`).Scan(&fundingPaused); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT enabled FROM system_flags WHERE name = 'outflows_paused'`).Scan(&outflowsPaused); err != nil {
		t.Fatal(err)
	}
	if !fundingPaused || !outflowsPaused {
		t.Fatalf("pause flags funding=%t outflows=%t; want both true", fundingPaused, outflowsPaused)
	}
}

func TestIntegrationUnmatchedReceiptIsQuarantinedIdempotently(t *testing.T) {
	service, pool, _ := integrationFixture(t)
	ctx := context.Background()
	hash := uuid.New()
	if err := service.RecordUnmatchedReceipt(ctx, hash[:], 2000); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordUnmatchedReceipt(ctx, hash[:], 2000); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordUnmatchedReceipt(ctx, hash[:], 2100); err == nil {
		t.Fatal("same payment hash with a changed amount should be rejected")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM unmatched_receipts WHERE payment_hash = $1`, hash[:]).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("quarantined receipt count = %d, want 1", count)
	}
}

func TestIntegrationSettlementRollsBackWithoutWallet(t *testing.T) {
	service, pool, projectID := integrationFixture(t)
	ctx := context.Background()
	hash := uuid.New()
	deposit, err := service.CreateDeposit(ctx, projectID, "integration-rollback-"+hash.String(), "test-invoice", hash[:], 1000, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM wallets WHERE project_id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	if err := service.SettleDeposit(ctx, hash[:], 1000); err == nil {
		t.Fatal("settlement without a wallet should fail")
	}
	var status string
	var entries int
	if err := pool.QueryRow(ctx, `SELECT status FROM deposits WHERE id = $1`, deposit.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE ref_type = 'deposit' AND ref_id = $1`, deposit.ID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || entries != 0 {
		t.Fatalf("failed settlement left status=%q and %d ledger entries; want pending and 0", status, entries)
	}
}

func TestIntegrationBountySearchFiltersAndCursorWorkTogether(t *testing.T) {
	service, pool, projectID := integrationFixture(t)
	ctx := context.Background()
	repo := "golang/integration-" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, `UPDATE projects SET repo_full_name = $1 WHERE id = $2`, repo, projectID); err != nil {
		t.Fatal(err)
	}
	fixtures := []struct {
		issue       int
		title       string
		description string
		difficulty  string
		reward      int64
	}{
		{issue: 1, title: "Docs setup", description: "Improve local setup", difficulty: "easy", reward: 1000},
		{issue: 2, title: "API docs", description: "Add endpoint docs", difficulty: "easy", reward: 1500},
		{issue: 3, title: "Docs translation", description: "Translate guide", difficulty: "hard", reward: 1500},
		{issue: 4, title: "Docs extra", description: "More help", difficulty: "easy", reward: 3000},
	}
	for _, fixture := range fixtures {
		if _, err := pool.Exec(ctx, `
			INSERT INTO bounties (project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, 20, 'open', now() + interval '1 day')
		`, projectID, fixture.issue, fixture.title, fixture.description, fixture.difficulty, fixture.reward); err != nil {
			t.Fatal(err)
		}
	}
	filters := ListFilters{Query: "docs", Language: "golang", Difficulty: "easy", MinReward: 1000, MaxReward: 2000, Limit: 1}
	firstPage, cursor, err := service.ListBounties(ctx, filters)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage) != 1 || cursor == "" {
		t.Fatalf("first page has %d bounties and cursor %q; want one result and a cursor", len(firstPage), cursor)
	}
	filters.Cursor = cursor
	secondPage, nextCursor, err := service.ListBounties(ctx, filters)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage) != 1 || nextCursor != "" || firstPage[0].ID == secondPage[0].ID {
		t.Fatalf("second page has %d bounties, cursor %q; want the other matching result and no next page", len(secondPage), nextCursor)
	}
}

func TestIntegrationConcurrentBountyFundingCannotOverspendWallet(t *testing.T) {
	service, pool, projectID := integrationFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE wallets SET available_sats = 1000 WHERE project_id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 2)
	for i := range ids {
		if err := pool.QueryRow(ctx, `
			INSERT INTO bounties (project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at)
			VALUES ($1, $2, $3, 'integration', 'easy', 1000, 20, 'draft', now() + interval '1 day') RETURNING id
		`, projectID, i+1, fmt.Sprintf("Integration bounty %d", i)).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}

	results := make(chan error, len(ids))
	for _, id := range ids {
		go func(bountyID int64) {
			_, err := service.FundBounty(ctx, bountyID, projectID)
			results <- err
		}(id)
	}
	succeeded, insufficient := 0, 0
	for range ids {
		err := <-results
		if err == nil {
			succeeded++
		} else if errors.Is(err, ErrInsufficientFunds) {
			insufficient++
		} else {
			t.Fatalf("FundBounty() error = %v", err)
		}
	}
	var available, reserved int64
	if err := pool.QueryRow(ctx, `SELECT available_sats, reserved_sats FROM wallets WHERE project_id = $1`, projectID).Scan(&available, &reserved); err != nil {
		t.Fatal(err)
	}
	var openBounties, draftBounties int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE state = 'open'), count(*) FILTER (WHERE state = 'draft') FROM bounties WHERE project_id = $1`, projectID).Scan(&openBounties, &draftBounties); err != nil {
		t.Fatal(err)
	}
	if succeeded != 1 || insufficient != 1 || available != 0 || reserved != 1000 || openBounties != 1 || draftBounties != 1 {
		t.Fatalf("success=%d insufficient=%d available=%d reserved=%d open=%d draft=%d", succeeded, insufficient, available, reserved, openBounties, draftBounties)
	}
}

type staticNodeBalance int64

func (b staticNodeBalance) GetTotalBalance(context.Context) (int64, error) { return int64(b), nil }
