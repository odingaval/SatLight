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
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/satlight/satlight/internal/config"
	"github.com/satlight/satlight/internal/ledger"
	"github.com/satlight/satlight/internal/queue"
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
	riverMigrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := riverMigrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
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
	service := NewService(&config.Config{MinRewardSats: 500, ClaimExpiryDays: 7, BountyDefaultExpiryDays: 90}, pool)
	service.SetDependencies(ledger.NewService(nil, pool), queue.NewClient(pool))
	return service, pool, projectID
}

func integrationUser(t *testing.T, pool *pgxpool.Pool, lightningAddress string) int64 {
	t.Helper()
	unique := uuid.NewString()
	var userID int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO users (github_id, github_login, email, lightning_address)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, time.Now().UnixNano(), "user-"+unique[:8], "user-"+unique[:8]+"@example.test", lightningAddress).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	return userID
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

func TestIntegrationBountyCreateIsIdempotent(t *testing.T) {
	service, pool, projectID := integrationFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE wallets SET available_sats = 100000 WHERE project_id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(24 * time.Hour)
	params := CreateBountyParams{
		ProjectID: projectID, GitHubIssueNumber: 101, Title: "Idempotent bounty",
		Description: "retry safe", Difficulty: "easy", RewardSats: 5000,
		ExpiresAt: &expires, IdempotencyKey: "bounty-key-" + uuid.NewString(), IdempotencyHash: "hash-1",
	}
	first, created, err := service.CreateBounty(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !created || first.State != "open" {
		t.Fatalf("first create: created=%t state=%q, want created=true state=open", created, first.State)
	}
	second, created, err := service.CreateBounty(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if created || second.ID != first.ID {
		t.Fatalf("retry: created=%t id=%d, want created=false id=%d", created, second.ID, first.ID)
	}
	params.IdempotencyHash = "hash-2"
	if _, _, err := service.CreateBounty(ctx, params); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("same key with different data: err=%v, want ErrIdempotencyConflict", err)
	}
	params.IdempotencyKey = "other-key-" + uuid.NewString()
	params.IdempotencyHash = "hash-3"
	if _, _, err := service.CreateBounty(ctx, params); !errors.Is(err, ErrDuplicateIssueBounty) {
		t.Fatalf("same issue with different key: err=%v, want ErrDuplicateIssueBounty", err)
	}
	var available, reserved int64
	if err := pool.QueryRow(ctx, `SELECT available_sats, reserved_sats FROM wallets WHERE project_id = $1`, projectID).Scan(&available, &reserved); err != nil {
		t.Fatal(err)
	}
	if available != 95000 || reserved != 5000 {
		t.Fatalf("wallet available=%d reserved=%d, want 95000/5000 (reserved exactly once)", available, reserved)
	}
	var entries int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE ref_type = 'bounty_reserve' AND ref_id = $1`, first.ID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 2 {
		t.Fatalf("ledger reserve entries = %d, want exactly 2", entries)
	}
}

func TestIntegrationClaimIsIdempotent(t *testing.T) {
	service, pool, projectID := integrationFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE wallets SET available_sats = 100000 WHERE project_id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(24 * time.Hour)
	newOpenBounty := func(issue int) int64 {
		t.Helper()
		b, _, err := service.CreateBounty(ctx, CreateBountyParams{
			ProjectID: projectID, GitHubIssueNumber: issue, Title: "Claimable",
			Description: "claim me", Difficulty: "easy", RewardSats: 5000,
			ExpiresAt: &expires, IdempotencyKey: "bounty-" + uuid.NewString(), IdempotencyHash: "h",
		})
		if err != nil {
			t.Fatal(err)
		}
		return b.ID
	}
	contributor := integrationUser(t, pool, "contributor@example.test")
	bountyID := newOpenBounty(201)
	claimKey := "claim-key-" + uuid.NewString()
	claim, created, err := service.ClaimBounty(ctx, bountyID, contributor, claimKey)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first claim: created=false, want true")
	}
	replay, created, err := service.ClaimBounty(ctx, bountyID, contributor, claimKey)
	if err != nil {
		t.Fatal(err)
	}
	if created || replay.ID != claim.ID {
		t.Fatalf("claim retry: created=%t id=%d, want created=false id=%d", created, replay.ID, claim.ID)
	}
	if _, _, err := service.ClaimBounty(ctx, bountyID, contributor, "other-"+uuid.NewString()); !errors.Is(err, ErrBountyNotOpen) {
		t.Fatalf("second claim with new key: err=%v, want ErrBountyNotOpen", err)
	}
	withoutAddress := integrationUser(t, pool, "")
	if _, _, err := service.ClaimBounty(ctx, newOpenBounty(202), withoutAddress, "k-"+uuid.NewString()); !errors.Is(err, ErrPayoutDestinationMissing) {
		t.Fatalf("claim without payout destination: err=%v, want ErrPayoutDestinationMissing", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET lightning_address = NULL WHERE id = $1`, withoutAddress); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ClaimBounty(ctx, newOpenBounty(203), withoutAddress, "k-"+uuid.NewString()); !errors.Is(err, ErrPayoutDestinationMissing) {
		t.Fatalf("claim with NULL payout destination: err=%v, want ErrPayoutDestinationMissing", err)
	}
}

func TestIntegrationDisputeResolveCancelRefunds(t *testing.T) {
	service, pool, projectID := integrationFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE wallets SET available_sats = 100000 WHERE project_id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	var ownerID int64
	if err := pool.QueryRow(ctx, `SELECT owner_user_id FROM projects WHERE id = $1`, projectID).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(24 * time.Hour)
	bounty, _, err := service.CreateBounty(ctx, CreateBountyParams{
		ProjectID: projectID, GitHubIssueNumber: 301, Title: "Disputed work",
		Description: "under review", Difficulty: "medium", RewardSats: 20000,
		ExpiresAt: &expires, IdempotencyKey: "bounty-" + uuid.NewString(), IdempotencyHash: "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	contributor := integrationUser(t, pool, "contributor@example.test")
	if err := service.DisputeBounty(ctx, bounty.ID, contributor, "maintainer unresponsive", ""); err != nil {
		t.Fatal(err)
	}
	var state, previous string
	if err := pool.QueryRow(ctx, `SELECT state FROM bounties WHERE id = $1`, bounty.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	var disputeID int64
	if err := pool.QueryRow(ctx, `SELECT id, previous_state FROM disputes WHERE bounty_id = $1`, bounty.ID).Scan(&disputeID, &previous); err != nil {
		t.Fatal(err)
	}
	if state != "disputed" || previous != "open" {
		t.Fatalf("after dispute: state=%q previous=%q, want disputed/open", state, previous)
	}
	if err := service.ResolveDispute(ctx, disputeID, contributor, "resolved_cancel"); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("non-owner resolve: err=%v, want ErrNotAuthorized", err)
	}
	if err := service.ResolveDispute(ctx, disputeID, ownerID, "resolved_cancel"); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM bounties WHERE id = $1`, bounty.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	var available, reserved int64
	if err := pool.QueryRow(ctx, `SELECT available_sats, reserved_sats FROM wallets WHERE project_id = $1`, projectID).Scan(&available, &reserved); err != nil {
		t.Fatal(err)
	}
	if state != "cancelled" || available != 100000 || reserved != 0 {
		t.Fatalf("after resolve-cancel: state=%q available=%d reserved=%d, want cancelled/100000/0", state, available, reserved)
	}
	if err := service.ResolveDispute(ctx, disputeID, ownerID, "resolved_cancel"); !errors.Is(err, ErrDisputeClosed) {
		t.Fatalf("second resolve: err=%v, want ErrDisputeClosed", err)
	}
	if err := service.DisputeBounty(ctx, bounty.ID, contributor, "again", ""); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("dispute of cancelled bounty: err=%v, want ErrInvalidState", err)
	}
}

func TestIntegrationDisputePayRequiresVerifiedMerge(t *testing.T) {
	service, pool, projectID := integrationFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE wallets SET available_sats = 100000 WHERE project_id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	var ownerID int64
	if err := pool.QueryRow(ctx, `SELECT owner_user_id FROM projects WHERE id = $1`, projectID).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(24 * time.Hour)
	contributor := integrationUser(t, pool, "contributor@example.test")
	bounty, _, err := service.CreateBounty(ctx, CreateBountyParams{
		ProjectID: projectID, GitHubIssueNumber: 401, Title: "Pay dispute",
		Description: "was it merged", Difficulty: "easy", RewardSats: 5000,
		ExpiresAt: &expires, IdempotencyKey: "bounty-" + uuid.NewString(), IdempotencyHash: "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DisputeBounty(ctx, bounty.ID, contributor, "work was merged", ""); err != nil {
		t.Fatal(err)
	}
	var disputeID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM disputes WHERE bounty_id = $1`, bounty.ID).Scan(&disputeID); err != nil {
		t.Fatal(err)
	}
	if err := service.ResolveDispute(ctx, disputeID, ownerID, "resolved_pay"); !errors.Is(err, ErrDisputePayNotReady) {
		t.Fatalf("resolve-pay without merge: err=%v, want ErrDisputePayNotReady", err)
	}
	var contributorGithubID int64
	if err := pool.QueryRow(ctx, `SELECT github_id FROM users WHERE id = $1`, contributor).Scan(&contributorGithubID); err != nil {
		t.Fatal(err)
	}
	var claimID int64
	if err := pool.QueryRow(ctx, `INSERT INTO claims (bounty_id, user_id, status, expires_at) VALUES ($1, $2, 'active', now() + interval '1 day') RETURNING id`,
		bounty.ID, contributor).Scan(&claimID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO pull_requests (claim_id, github_pr_number, author_github_id, merged, merged_at) VALUES ($1, 7, $2, true, now())`,
		claimID, contributorGithubID); err != nil {
		t.Fatal(err)
	}
	if err := service.ResolveDispute(ctx, disputeID, ownerID, "resolved_pay"); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM bounties WHERE id = $1`, bounty.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "merged" {
		t.Fatalf("after resolve-pay with verified merge: state=%q, want merged", state)
	}

	other, _, err := service.CreateBounty(ctx, CreateBountyParams{
		ProjectID: projectID, GitHubIssueNumber: 402, Title: "Mutual resolution",
		Description: "talk it out", Difficulty: "easy", RewardSats: 5000,
		ExpiresAt: &expires, IdempotencyKey: "bounty-" + uuid.NewString(), IdempotencyHash: "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DisputeBounty(ctx, other.ID, contributor, "misunderstanding", ""); err != nil {
		t.Fatal(err)
	}
	var otherDisputeID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM disputes WHERE bounty_id = $1`, other.ID).Scan(&otherDisputeID); err != nil {
		t.Fatal(err)
	}
	if err := service.ResolveDispute(ctx, otherDisputeID, ownerID, "resolved_other"); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM bounties WHERE id = $1`, other.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "open" {
		t.Fatalf("after resolved_other: state=%q, want open (pre-dispute state)", state)
	}
}

func TestIntegrationClaimEligibilityBlocksBannedFrozenSelfClaim(t *testing.T) {
	service, pool, projectID := integrationFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE wallets SET available_sats = 100000 WHERE project_id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	var ownerID int64
	if err := pool.QueryRow(ctx, `SELECT owner_user_id FROM projects WHERE id = $1`, projectID).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(24 * time.Hour)
	bounty, _, err := service.CreateBounty(ctx, CreateBountyParams{
		ProjectID: projectID, GitHubIssueNumber: 601, Title: "Guarded",
		Description: "eligibility", Difficulty: "easy", RewardSats: 5000,
		ExpiresAt: &expires, IdempotencyKey: "bounty-" + uuid.NewString(), IdempotencyHash: "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	banned := integrationUser(t, pool, "banned@example.test")
	if _, err := pool.Exec(ctx, `UPDATE users SET is_banned = TRUE WHERE id = $1`, banned); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ClaimBounty(ctx, bounty.ID, banned, "k-"+uuid.NewString()); !errors.Is(err, ErrUserBanned) {
		t.Fatalf("banned user claim: err=%v, want ErrUserBanned", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE projects SET status = 'frozen' WHERE id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	contributor := integrationUser(t, pool, "contributor@example.test")
	if _, _, err := service.ClaimBounty(ctx, bounty.ID, contributor, "k-"+uuid.NewString()); !errors.Is(err, ErrProjectFrozen) {
		t.Fatalf("frozen project claim: err=%v, want ErrProjectFrozen", err)
	}
	if _, _, err := service.CreateBounty(ctx, CreateBountyParams{
		ProjectID: projectID, GitHubIssueNumber: 602, Title: "Frozen create",
		Description: "blocked", Difficulty: "easy", RewardSats: 5000,
		ExpiresAt: &expires, IdempotencyKey: "bounty-" + uuid.NewString(), IdempotencyHash: "h",
	}); !errors.Is(err, ErrProjectFrozen) {
		t.Fatalf("frozen project create: err=%v, want ErrProjectFrozen", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE projects SET status = 'active' WHERE id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET lightning_address = 'owner@example.test' WHERE id = $1`, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ClaimBounty(ctx, bounty.ID, ownerID, "k-"+uuid.NewString()); !errors.Is(err, ErrSelfClaim) {
		t.Fatalf("owner self-claim: err=%v, want ErrSelfClaim", err)
	}
}

func TestIntegrationClaimEnforcesGitHubAccountAge(t *testing.T) {
	service, pool, projectID := integrationFixture(t)
	ctx := context.Background()
	aged := NewService(&config.Config{MinRewardSats: 500, ClaimExpiryDays: 7, BountyDefaultExpiryDays: 90, ClaimMinGitHubAccountAgeDays: 30}, pool)
	aged.SetDependencies(ledger.NewService(nil, pool), queue.NewClient(pool))
	if _, err := pool.Exec(ctx, `UPDATE wallets SET available_sats = 100000 WHERE project_id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(24 * time.Hour)
	newOpenBounty := func(issue int) int64 {
		t.Helper()
		b, _, err := service.CreateBounty(ctx, CreateBountyParams{
			ProjectID: projectID, GitHubIssueNumber: issue, Title: "Age gated",
			Description: "age", Difficulty: "easy", RewardSats: 5000,
			ExpiresAt: &expires, IdempotencyKey: "bounty-" + uuid.NewString(), IdempotencyHash: "h",
		})
		if err != nil {
			t.Fatal(err)
		}
		return b.ID
	}
	newbie := integrationUser(t, pool, "newbie@example.test")
	if _, err := pool.Exec(ctx, `UPDATE users SET github_created_at = now() - interval '1 day' WHERE id = $1`, newbie); err != nil {
		t.Fatal(err)
	}
	if _, _, err := aged.ClaimBounty(ctx, newOpenBounty(701), newbie, "k-"+uuid.NewString()); !errors.Is(err, ErrClaimantAccountTooNew) {
		t.Fatalf("one-day-old account: err=%v, want ErrClaimantAccountTooNew", err)
	}
	unknownAge := integrationUser(t, pool, "unknown@example.test")
	if _, _, err := aged.ClaimBounty(ctx, newOpenBounty(702), unknownAge, "k-"+uuid.NewString()); !errors.Is(err, ErrClaimantAccountTooNew) {
		t.Fatalf("unknown account age: err=%v, want ErrClaimantAccountTooNew", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET github_created_at = now() - interval '60 days' WHERE id = $1`, newbie); err != nil {
		t.Fatal(err)
	}
	if _, created, err := aged.ClaimBounty(ctx, newOpenBounty(703), newbie, "k-"+uuid.NewString()); err != nil || !created {
		t.Fatalf("sixty-day-old account: created=%t err=%v, want created=true", created, err)
	}
	fresh := integrationUser(t, pool, "fresh@example.test")
	if _, created, err := service.ClaimBounty(ctx, newOpenBounty(704), fresh, "k-"+uuid.NewString()); err != nil || !created {
		t.Fatalf("rule disabled: created=%t err=%v, want created=true", created, err)
	}
}

func TestIntegrationClaimEnforcesMaxActiveClaims(t *testing.T) {
	_, pool, projectID := integrationFixture(t)
	ctx := context.Background()
	limited := NewService(&config.Config{MinRewardSats: 500, ClaimExpiryDays: 7, BountyDefaultExpiryDays: 90, ClaimMaxActiveClaims: 1}, pool)
	limited.SetDependencies(ledger.NewService(nil, pool), queue.NewClient(pool))
	if _, err := pool.Exec(ctx, `UPDATE wallets SET available_sats = 100000 WHERE project_id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(24 * time.Hour)
	newOpenBounty := func(issue int) int64 {
		t.Helper()
		b, _, err := limited.CreateBounty(ctx, CreateBountyParams{
			ProjectID: projectID, GitHubIssueNumber: issue, Title: "Limited",
			Description: "limits", Difficulty: "easy", RewardSats: 5000,
			ExpiresAt: &expires, IdempotencyKey: "bounty-" + uuid.NewString(), IdempotencyHash: "h",
		})
		if err != nil {
			t.Fatal(err)
		}
		return b.ID
	}
	squatter := integrationUser(t, pool, "squatter@example.test")
	if _, _, err := limited.ClaimBounty(ctx, newOpenBounty(705), squatter, "k-"+uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := limited.ClaimBounty(ctx, newOpenBounty(706), squatter, "k-"+uuid.NewString()); !errors.Is(err, ErrTooManyActiveClaims) {
		t.Fatalf("second active claim: err=%v, want ErrTooManyActiveClaims", err)
	}
}

func TestIntegrationReciprocalPatternsFlagged(t *testing.T) {
	_, pool, projectID := integrationFixture(t)
	ctx := context.Background()
	var ownerA int64
	if err := pool.QueryRow(ctx, `SELECT owner_user_id FROM projects WHERE id = $1`, projectID).Scan(&ownerA); err != nil {
		t.Fatal(err)
	}
	ownerB := integrationUser(t, pool, "owner-b@example.test")
	var projectB int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (owner_user_id, github_installation_id, repo_full_name)
		VALUES ($1, $2, $3) RETURNING id
	`, ownerB, time.Now().UnixNano(), "reciprocal/"+uuid.NewString()[:8]).Scan(&projectB); err != nil {
		t.Fatal(err)
	}
	mergedBounty := func(project, issue int64, claimUser int64) {
		t.Helper()
		var bountyID, claimID int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO bounties (project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at)
			VALUES ($1, $2, 'merged', 'merged', 'easy', 1000, 20, 'merged', now() + interval '1 day') RETURNING id
		`, project, issue).Scan(&bountyID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `
			INSERT INTO claims (bounty_id, user_id, status, expires_at) VALUES ($1, $2, 'active', now() + interval '1 day') RETURNING id
		`, bountyID, claimUser).Scan(&claimID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO pull_requests (claim_id, github_pr_number, author_github_id, merged, merged_at) VALUES ($1, 1, 0, true, now())
		`, claimID); err != nil {
			t.Fatal(err)
		}
	}
	mergedBounty(projectID, 801, ownerB)
	mergedBounty(projectID, 802, ownerB)
	mergedBounty(projectB, 801, ownerA)
	mergedBounty(projectB, 802, ownerA)
	loner := integrationUser(t, pool, "loner@example.test")
	mergedBounty(projectID, 803, loner)

	ledgerService := ledger.NewService(nil, pool)
	flags, err := ledgerService.DetectReciprocalPatterns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byPair := make(map[[2]int64]ledger.ReciprocalFlag)
	for _, flag := range flags {
		byPair[[2]int64{flag.OwnerUserID, flag.ContributorUserID}] = flag
	}
	ab, ok := byPair[[2]int64{ownerA, ownerB}]
	if !ok || ab.MergedBounties != 2 || !ab.Mutual {
		t.Fatalf("flag (A,B) = %+v, want 2 merged bounties and mutual", ab)
	}
	ba, ok := byPair[[2]int64{ownerB, ownerA}]
	if !ok || ba.MergedBounties != 2 || !ba.Mutual {
		t.Fatalf("flag (B,A) = %+v, want 2 merged bounties and mutual", ba)
	}
	if _, ok := byPair[[2]int64{ownerA, loner}]; ok {
		t.Fatal("single merged bounty was flagged, want no flag below threshold")
	}
}

func TestIntegrationProjectPayoutCapEnforced(t *testing.T) {
	_, pool, projectID := integrationFixture(t)
	ctx := context.Background()
	payer := integrationUser(t, pool, "payer@example.test")
	newBounty := func(issue int) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO bounties (project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at)
			VALUES ($1, $2, 'cap', 'cap', 'easy', 1000, 20, 'merged', now() + interval '1 day') RETURNING id
		`, projectID, issue).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	settled := func(bountyID int64, amount int64, when string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			INSERT INTO payouts (bounty_id, user_id, amount_sats, fee_sats, destination, status, settled_at)
			VALUES ($1, $2, $3, 0, 'dest@example.test', 'settled', now() - $4::interval)
		`, bountyID, payer, amount, when); err != nil {
			t.Fatal(err)
		}
	}
	settled(newBounty(901), 500000, "1 hour")
	settled(newBounty(902), 400000, "2 hours")
	settled(newBounty(903), 500000, "25 hours")
	if _, err := pool.Exec(ctx, `
		INSERT INTO payouts (bounty_id, user_id, amount_sats, fee_sats, destination, status)
		VALUES ($1, $2, 500000, 0, 'dest@example.test', 'pending')
	`, newBounty(904), payer); err != nil {
		t.Fatal(err)
	}
	ledgerService := ledger.NewService(nil, pool)
	paid, err := ledgerService.ProjectPaidLast24h(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if paid != 900000 {
		t.Fatalf("paid last 24h = %d, want 900000 (settled in-window only)", paid)
	}
	if err := ledgerService.CheckProjectPayoutCap(ctx, projectID, 200000, 1000000); !errors.Is(err, ledger.ErrDailyPayoutCapExceeded) {
		t.Fatalf("cap check over limit: err=%v, want ErrDailyPayoutCapExceeded", err)
	}
	if err := ledgerService.CheckProjectPayoutCap(ctx, projectID, 100000, 1000000); err != nil {
		t.Fatalf("cap check within limit: err=%v, want nil", err)
	}
	if err := ledgerService.CheckProjectPayoutCap(ctx, projectID, 1<<40, 0); err != nil {
		t.Fatalf("disabled cap: err=%v, want nil", err)
	}
}
