//go:build integration

package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/go-github/v60/github"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/satlight/satlight/internal/queue"
)

type webhookFixture struct {
	service           *Service
	pool              *pgxpool.Pool
	projectID         int64
	installationID    int64
	repoFullName      string
	contributorID     int64
	contributorGithub int64
	bountyID          int64
}

func newWebhookFixture(t *testing.T) *webhookFixture {
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
	var ownerID, contributorID int64
	contributorGithub := now + 1
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (github_id, github_login, email) VALUES ($1, $2, $3) RETURNING id
	`, now, "owner-"+unique[:8], "owner-"+unique[:8]+"@example.test").Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (github_id, github_login, email, lightning_address) VALUES ($1, $2, $3, $4) RETURNING id
	`, contributorGithub, "contrib-"+unique[:8], "contrib-"+unique[:8]+"@example.test", "contrib@example.test").Scan(&contributorID); err != nil {
		t.Fatal(err)
	}
	repo := "webhook/" + unique[:8]
	installationID := now + 2
	var projectID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects (owner_user_id, github_installation_id, repo_full_name) VALUES ($1, $2, $3) RETURNING id
	`, ownerID, installationID, repo).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO wallets (project_id, available_sats, reserved_sats) VALUES ($1, 95000, 5000)`, projectID); err != nil {
		t.Fatal(err)
	}
	var bountyID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO bounties (project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at)
		VALUES ($1, 501, 'Webhook bounty', 'linked PR', 'easy', 5000, 100, 'claimed', now() + interval '1 day') RETURNING id
	`, projectID).Scan(&bountyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO claims (bounty_id, user_id, status, expires_at) VALUES ($1, $2, 'active', now() + interval '1 day')
	`, bountyID, contributorID); err != nil {
		t.Fatal(err)
	}
	service := NewService(&Config{WebhookSecret: "test"}, pool)
	service.SetQueue(queue.NewClient(pool))
	return &webhookFixture{
		service: service, pool: pool, projectID: projectID,
		installationID: installationID, repoFullName: repo,
		contributorID: contributorID, contributorGithub: contributorGithub, bountyID: bountyID,
	}
}

type mockPullRequest struct {
	Number        int
	Body          string
	Merged        bool
	MergedAt      *time.Time
	MergeSHA      string
	HeadSHA       string
	AuthorID      int64
	BaseRef       string
	DefaultBranch string
	MergedByID    int64
}

func (f *webhookFixture) useMockGitHub(t *testing.T, pr *mockPullRequest) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.Contains(r.URL.Path, "/pulls/") {
			if err := json.NewEncoder(w).Encode(&github.Repository{DefaultBranch: github.String(pr.DefaultBranch)}); err != nil {
				t.Errorf("encode mock repo: %v", err)
			}
			return
		}
		ghPR := &github.PullRequest{
			Number:         github.Int(pr.Number),
			Body:           github.String(pr.Body),
			Merged:         github.Bool(pr.Merged),
			MergeCommitSHA: github.String(pr.MergeSHA),
			Head:           &github.PullRequestBranch{SHA: github.String(pr.HeadSHA)},
			Base:           &github.PullRequestBranch{Ref: github.String(pr.BaseRef)},
			User:           &github.User{ID: github.Int64(pr.AuthorID)},
		}
		if pr.MergedAt != nil {
			ghPR.MergedAt = &github.Timestamp{Time: *pr.MergedAt}
		}
		if pr.MergedByID != 0 {
			ghPR.MergedBy = &github.User{ID: github.Int64(pr.MergedByID)}
		}
		if err := json.NewEncoder(w).Encode(ghPR); err != nil {
			t.Errorf("encode mock PR: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	f.service.SetInstallationClientFactory(func(_ int64) (*github.Client, error) {
		client := github.NewClient(nil)
		base, err := url.Parse(server.URL + "/")
		if err != nil {
			return nil, err
		}
		client.BaseURL = base
		client.UploadURL = base
		return client, nil
	})
}

func (f *webhookFixture) recordAndProcess(t *testing.T, event string, payload any) string {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	deliveryID := "delivery-" + uuid.NewString()
	duplicate, err := f.service.RecordWebhookDelivery(context.Background(), deliveryID, event, data)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate {
		t.Fatal("fresh delivery reported as duplicate")
	}
	if err := f.service.ProcessWebhookDelivery(context.Background(), deliveryID); err != nil {
		t.Fatal(err)
	}
	return deliveryID
}

func prPayload(action string, number int, repo string) map[string]any {
	return map[string]any{"action": action, "number": number, "repository": map[string]any{"full_name": repo}}
}

func (f *webhookFixture) bountyState(t *testing.T) string {
	t.Helper()
	var state string
	if err := f.pool.QueryRow(context.Background(), `SELECT state FROM bounties WHERE id = $1`, f.bountyID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestIntegrationWebhookOpenedMovesClaimedToInReview(t *testing.T) {
	f := newWebhookFixture(t)
	f.useMockGitHub(t, &mockPullRequest{
		Number: 7, Body: "Closes #501", Merged: false,
		HeadSHA: "abc123", AuthorID: f.contributorGithub,
	})
	deliveryID := f.recordAndProcess(t, "pull_request", prPayload("opened", 7, f.repoFullName))
	if state := f.bountyState(t); state != "in_review" {
		t.Fatalf("after PR opened: state=%q, want in_review", state)
	}
	var merged bool
	var headSHA string
	err := f.pool.QueryRow(context.Background(), `
		SELECT pr.merged, pr.head_sha FROM pull_requests pr
		JOIN claims c ON pr.claim_id = c.id WHERE c.bounty_id = $1 AND pr.github_pr_number = 7
	`, f.bountyID).Scan(&merged, &headSHA)
	if err != nil {
		t.Fatal(err)
	}
	if merged || headSHA != "abc123" {
		t.Fatalf("PR row merged=%t head=%q, want merged=false head=abc123", merged, headSHA)
	}
	if err := f.service.ProcessWebhookDelivery(context.Background(), deliveryID); err != nil {
		t.Fatal(err)
	}
	if state := f.bountyState(t); state != "in_review" {
		t.Fatalf("after redelivery: state=%q, want in_review", state)
	}
}

func TestIntegrationWebhookMergeRequiresAPIConfirmation(t *testing.T) {
	f := newWebhookFixture(t)
	pr := &mockPullRequest{
		Number: 8, Body: "Closes #501", Merged: false,
		HeadSHA: "def456", AuthorID: f.contributorGithub,
	}
	f.useMockGitHub(t, pr)
	// The payload claims a merge, but the API says otherwise: no payout state.
	forged := prPayload("closed", 8, f.repoFullName)
	forged["pull_request"] = map[string]any{"merged": true, "merge_commit_sha": "forged"}
	f.recordAndProcess(t, "pull_request", forged)
	if state := f.bountyState(t); state != "claimed" {
		t.Fatalf("after forged merge payload: state=%q, want claimed (unchanged)", state)
	}
	mergedAt := time.Now().UTC().Truncate(time.Second)
	pr.Merged = true
	pr.MergedAt = &mergedAt
	pr.MergeSHA = "realmergesha"
	f.recordAndProcess(t, "pull_request", prPayload("closed", 8, f.repoFullName))
	if state := f.bountyState(t); state != "merged" {
		t.Fatalf("after API-confirmed merge: state=%q, want merged", state)
	}
	var mergeSHA string
	var dbMergedAt time.Time
	err := f.pool.QueryRow(context.Background(), `
		SELECT pr.merge_commit_sha, pr.merged_at FROM pull_requests pr
		JOIN claims c ON pr.claim_id = c.id WHERE c.bounty_id = $1 AND pr.github_pr_number = 8
	`, f.bountyID).Scan(&mergeSHA, &dbMergedAt)
	if err != nil {
		t.Fatal(err)
	}
	if mergeSHA != "realmergesha" || !dbMergedAt.Equal(mergedAt) {
		t.Fatalf("merge record sha=%q at=%v, want realmergesha at %v", mergeSHA, dbMergedAt, mergedAt)
	}
}

func TestIntegrationWebhookMergeRejectsNonDefaultBase(t *testing.T) {
	f := newWebhookFixture(t)
	f.useMockGitHub(t, &mockPullRequest{
		Number: 12, Body: "Closes #501", Merged: true,
		MergeSHA: "x", HeadSHA: "y", AuthorID: f.contributorGithub,
		BaseRef: "develop", DefaultBranch: "main",
	})
	f.recordAndProcess(t, "pull_request", prPayload("closed", 12, f.repoFullName))
	if state := f.bountyState(t); state != "claimed" {
		t.Fatalf("after non-default-base merge: state=%q, want claimed (no merge)", state)
	}
	var count int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = 'pr.wrong_base'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count < 1 {
		t.Fatal("non-default-base merge was not recorded in the audit log")
	}
}

func TestIntegrationWebhookMergeFlagsSelfMerge(t *testing.T) {
	f := newWebhookFixture(t)
	f.useMockGitHub(t, &mockPullRequest{
		Number: 13, Body: "Closes #501", Merged: true,
		MergeSHA: "selfmerge", HeadSHA: "z", AuthorID: f.contributorGithub,
		BaseRef: "main", DefaultBranch: "main", MergedByID: f.contributorGithub,
	})
	f.recordAndProcess(t, "pull_request", prPayload("closed", 13, f.repoFullName))
	if state := f.bountyState(t); state != "merged" {
		t.Fatalf("after self-merge: state=%q, want merged (flagged, not blocked)", state)
	}
	var count int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = 'pr.self_merge'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count < 1 {
		t.Fatal("self-merge was not recorded in the audit log")
	}
	var mergedBy *int64
	err := f.pool.QueryRow(context.Background(), `
		SELECT pr.merged_by_github_id FROM pull_requests pr
		JOIN claims c ON pr.claim_id = c.id WHERE c.bounty_id = $1 AND pr.github_pr_number = 13
	`, f.bountyID).Scan(&mergedBy)
	if err != nil {
		t.Fatal(err)
	}
	if mergedBy == nil || *mergedBy != f.contributorGithub {
		t.Fatalf("stored merged_by = %v, want %d", mergedBy, f.contributorGithub)
	}
}

func TestIntegrationWebhookClosedUnmergedRevertsReview(t *testing.T) {
	f := newWebhookFixture(t)
	f.useMockGitHub(t, &mockPullRequest{
		Number: 9, Body: "Closes #501", Merged: false,
		HeadSHA: "aaa111", AuthorID: f.contributorGithub,
	})
	f.recordAndProcess(t, "pull_request", prPayload("opened", 9, f.repoFullName))
	if state := f.bountyState(t); state != "in_review" {
		t.Fatalf("after opened: state=%q, want in_review", state)
	}
	f.recordAndProcess(t, "pull_request", prPayload("closed", 9, f.repoFullName))
	if state := f.bountyState(t); state != "claimed" {
		t.Fatalf("after unmerged close: state=%q, want claimed", state)
	}
}

func TestIntegrationWebhookIgnoresForeignAuthor(t *testing.T) {
	f := newWebhookFixture(t)
	f.useMockGitHub(t, &mockPullRequest{
		Number: 10, Body: "Closes #501", Merged: true,
		MergeSHA: "x", HeadSHA: "y", AuthorID: f.contributorGithub + 999,
	})
	f.recordAndProcess(t, "pull_request", prPayload("closed", 10, f.repoFullName))
	if state := f.bountyState(t); state != "claimed" {
		t.Fatalf("after foreign-author PR: state=%q, want claimed (unchanged)", state)
	}
	var count int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = 'pr.unlinked'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count < 1 {
		t.Fatal("foreign-author PR was not recorded in the audit log")
	}
}

func TestIntegrationDuplicateDeliveryIsIdempotent(t *testing.T) {
	f := newWebhookFixture(t)
	data, err := json.Marshal(prPayload("opened", 11, f.repoFullName))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	deliveryID := "delivery-" + uuid.NewString()
	duplicate, err := f.service.RecordWebhookDelivery(ctx, deliveryID, "pull_request", data)
	if err != nil || duplicate {
		t.Fatalf("first record: duplicate=%t err=%v, want duplicate=false", duplicate, err)
	}
	duplicate, err = f.service.RecordWebhookDelivery(ctx, deliveryID, "pull_request", data)
	if err != nil || !duplicate {
		t.Fatalf("second record: duplicate=%t err=%v, want duplicate=true", duplicate, err)
	}
	var jobs int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'process_webhook' AND args->>'delivery_id' = $1`, deliveryID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 {
		t.Fatalf("webhook jobs enqueued for this delivery = %d, want exactly 1", jobs)
	}
}

func TestIntegrationInstallationEventsFreezeAndUnfreeze(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()
	status := func() string {
		var s string
		if err := f.pool.QueryRow(ctx, `SELECT status FROM projects WHERE id = $1`, f.projectID).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	payload := func(action string) map[string]any {
		return map[string]any{"action": action, "installation": map[string]any{"id": f.installationID}}
	}
	f.recordAndProcess(t, "installation", payload("suspend"))
	if status() != "frozen" {
		t.Fatalf("after suspend: status=%q, want frozen", status())
	}
	f.recordAndProcess(t, "installation", payload("unsuspend"))
	if status() != "active" {
		t.Fatalf("after unsuspend: status=%q, want active", status())
	}
	f.recordAndProcess(t, "installation", payload("deleted"))
	if status() != "frozen" {
		t.Fatalf("after deleted: status=%q, want frozen", status())
	}
}

func TestIntegrationIssueDeletedFlagsActiveBounty(t *testing.T) {
	f := newWebhookFixture(t)
	f.recordAndProcess(t, "issues", map[string]any{
		"action":     "deleted",
		"issue":      map[string]any{"number": 501},
		"repository": map[string]any{"full_name": f.repoFullName},
	})
	var count int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = 'issue.deleted_with_active_bounty'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count < 1 {
		t.Fatal("deleted issue with an active bounty was not flagged in the audit log")
	}
	if state := f.bountyState(t); state != "claimed" {
		t.Fatalf("after issue deleted: state=%q, want claimed (flag only, no state change)", state)
	}
}
