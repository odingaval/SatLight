package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/go-github/v60/github"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog"
	"github.com/satlight/satlight/internal/bounty"
	"github.com/satlight/satlight/internal/metrics"
	"github.com/satlight/satlight/internal/notify"
	"github.com/satlight/satlight/internal/queue"
	"golang.org/x/oauth2"
)

var (
	ErrInvalidSignature = errors.New("invalid webhook signature")
	ErrNotAdmin         = errors.New("user is not admin of repository")
)

type Service struct {
	cfg    *Config
	pool   *pgxpool.Pool
	client *github.Client
	log    zerolog.Logger
	queue  *queue.Client
	notify *notify.Service
	// newInstallationClient builds per-installation API clients. Tests
	// override it with a factory pointed at a mock server.
	newInstallationClient func(installationID int64) (*github.Client, error)
}

type Config struct {
	AppID             int64
	PrivateKeyPath    string
	WebhookSecret     string
	OAuthClientID     string
	OAuthClientSecret string
}

func NewService(cfg interface{}, pool *pgxpool.Pool) *Service {
	c := cfg.(*Config)
	logger := zerolog.New(zerolog.ConsoleWriter{Out: gin.DefaultWriter, TimeFormat: zerolog.TimeFormatUnix}).With().Timestamp().Logger()
	svc := &Service{
		cfg:  c,
		pool: pool,
		log:  logger,
	}
	svc.newInstallationClient = svc.GetInstallationClient
	return svc
}

// SetQueue wires the job queue used to process webhooks asynchronously.
func (s *Service) SetQueue(q *queue.Client) {
	s.queue = q
}

// SetNotifier wires maintainer-webhook notifications (optional; skipped when nil).
func (s *Service) SetNotifier(n *notify.Service) {
	s.notify = n
}

// SetInstallationClientFactory overrides per-installation API client
// construction (used by tests with a mock GitHub server).
func (s *Service) SetInstallationClientFactory(f func(int64) (*github.Client, error)) {
	s.newInstallationClient = f
}

func (s *Service) Connect(ctx context.Context) error {
	itr, err := ghinstallation.NewKeyFromFile(http.DefaultTransport, s.cfg.AppID, 0, s.cfg.PrivateKeyPath)
	if err != nil {
		return err
	}

	s.client = github.NewClient(&http.Client{Transport: itr})
	return nil
}

func (s *Service) GetInstallationClient(installationID int64) (*github.Client, error) {
	itr, err := ghinstallation.NewKeyFromFile(http.DefaultTransport, s.cfg.AppID, installationID, s.cfg.PrivateKeyPath)
	if err != nil {
		return nil, err
	}
	return github.NewClient(&http.Client{Transport: itr}), nil
}

func (s *Service) VerifyWebhookSignature(payload []byte, signature string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sig := strings.TrimPrefix(signature, "sha256=")
	mac := hmac.New(sha256.New, []byte(s.cfg.WebhookSecret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

// RecordWebhookDelivery persists a verified webhook and enqueues it for
// asynchronous processing, atomically. It reports whether the delivery is a
// duplicate of an already recorded one.
func (s *Service) RecordWebhookDelivery(ctx context.Context, deliveryID, event string, payload []byte) (bool, error) {
	if s.queue == nil {
		return false, errors.New("webhook queue is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	res, err := tx.Exec(ctx, `
		INSERT INTO webhook_events (delivery_id, event, payload) VALUES ($1, $2, $3)
		ON CONFLICT (delivery_id) DO NOTHING
	`, deliveryID, event, payload)
	if err != nil {
		return false, err
	}
	if res.RowsAffected() == 0 {
		return true, tx.Commit(ctx)
	}
	if err := s.queue.InsertTx(ctx, tx, &queue.ProcessWebhookArgs{DeliveryID: deliveryID}, river.InsertOpts{Queue: "webhooks"}); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	metrics.WebhooksReceived.WithLabelValues(event).Inc()
	return false, nil
}

// ProcessWebhookDelivery handles one recorded delivery in the worker. It is
// idempotent: a redelivered job finds processed_at set and stops. Transient
// failures (GitHub API errors) return an error so River retries.
func (s *Service) ProcessWebhookDelivery(ctx context.Context, deliveryID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var event string
	var payload json.RawMessage
	var processed bool
	err = tx.QueryRow(ctx, `SELECT event, payload, processed_at IS NOT NULL FROM webhook_events WHERE delivery_id = $1 FOR UPDATE`, deliveryID).Scan(&event, &payload, &processed)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if processed {
		return tx.Commit(ctx)
	}

	switch event {
	case "pull_request":
		var prEvent github.PullRequestEvent
		if err := json.Unmarshal(payload, &prEvent); err != nil {
			return err
		}
		if err := s.processPullRequestEvent(ctx, tx, &prEvent, deliveryID); err != nil {
			return err
		}
	case "issues":
		var issueEvent github.IssuesEvent
		if err := json.Unmarshal(payload, &issueEvent); err != nil {
			return err
		}
		if err := s.processIssuesEvent(ctx, tx, &issueEvent); err != nil {
			return err
		}
	case "installation":
		var instEvent github.InstallationEvent
		if err := json.Unmarshal(payload, &instEvent); err != nil {
			return err
		}
		if err := s.processInstallationEvent(ctx, tx, &instEvent); err != nil {
			return err
		}
	case "installation_repositories":
		var reposEvent github.InstallationRepositoriesEvent
		if err := json.Unmarshal(payload, &reposEvent); err != nil {
			return err
		}
		if err := s.processInstallationRepositoriesEvent(ctx, tx, &reposEvent); err != nil {
			return err
		}
	default:
		s.log.Info().Str("event", event).Str("delivery_id", deliveryID).Msg("ignoring unsupported GitHub event")
	}

	_, err = tx.Exec(ctx, `UPDATE webhook_events SET processed_at = now() WHERE delivery_id = $1`, deliveryID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// processPullRequestEvent links a pull request to its bounty and advances the
// lifecycle. Merge state always comes from a fresh GitHub API read, never
// from the webhook payload alone.
func (s *Service) processPullRequestEvent(ctx context.Context, tx pgx.Tx, event *github.PullRequestEvent, deliveryID string) error {
	repoFullName := event.GetRepo().GetFullName()
	prNumber := event.GetNumber()
	if repoFullName == "" || prNumber == 0 {
		return nil
	}
	var projectID, installationID int64
	err := tx.QueryRow(ctx, `SELECT id, github_installation_id FROM projects WHERE repo_full_name = $1`, repoFullName).Scan(&projectID, &installationID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	owner, repo, ok := splitRepoFullName(repoFullName)
	if !ok {
		return nil
	}
	client, err := s.newInstallationClient(installationID)
	if err != nil {
		return err
	}
	pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		return err
	}
	issueNum := extractIssueNumber(pr.GetBody())
	if issueNum == 0 {
		return nil
	}
	var bountyID int64
	var bountyState string
	err = tx.QueryRow(ctx, `
		SELECT b.id, b.state FROM bounties b
		WHERE b.project_id = $1 AND b.github_issue_number = $2 AND b.state IN ('claimed', 'in_review', 'merged')
	`, projectID, issueNum).Scan(&bountyID, &bountyState)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	var claimID, claimUserGithubID int64
	err = tx.QueryRow(ctx, `
		SELECT c.id, u.github_id FROM claims c
		JOIN users u ON c.user_id = u.id
		WHERE c.bounty_id = $1 AND c.status = 'active'
	`, bountyID).Scan(&claimID, &claimUserGithubID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if pr.GetUser().GetID() != claimUserGithubID {
		_, err = tx.Exec(ctx, `
			INSERT INTO audit_log (actor, action, subject, metadata)
			VALUES ('github', 'pr.unlinked', $1, $2::jsonb)
		`, "bounty:"+strconv.FormatInt(bountyID, 10), fmt.Sprintf(`{"pr":%d}`, prNumber))
		return err
	}

	actor := "github:" + deliveryID
	switch {
	case pr.GetMerged():
		if bountyState == "merged" {
			return nil
		}
		repoInfo, _, err := client.Repositories.Get(ctx, owner, repo)
		if err != nil {
			return err
		}
		if base, def := pr.GetBase().GetRef(), repoInfo.GetDefaultBranch(); base != "" && def != "" && base != def {
			_, err = tx.Exec(ctx, `
				INSERT INTO audit_log (actor, action, subject, metadata)
				VALUES ('github', 'pr.wrong_base', $1, $2::jsonb)
			`, "bounty:"+strconv.FormatInt(bountyID, 10), fmt.Sprintf(`{"pr":%d,"base":%q}`, prNumber, base))
			return err
		}
		mergedBy := pr.GetMergedBy().GetID()
		if mergedBy != 0 && mergedBy == pr.GetUser().GetID() {
			if _, err := tx.Exec(ctx, `
				INSERT INTO audit_log (actor, action, subject, metadata)
				VALUES ('github', 'pr.self_merge', $1, $2::jsonb)
			`, "bounty:"+strconv.FormatInt(bountyID, 10), fmt.Sprintf(`{"pr":%d}`, prNumber)); err != nil {
				return err
			}
		}
		var mergedByID any
		if mergedBy != 0 {
			mergedByID = mergedBy
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO pull_requests (claim_id, github_pr_number, author_github_id, head_sha, merged, merged_at, merge_commit_sha, merged_by_github_id)
			VALUES ($1, $2, $3, $4, true, $5, $6, $7)
			ON CONFLICT (claim_id, github_pr_number) DO UPDATE SET merged = true, merged_at = $5, merge_commit_sha = $6, head_sha = $4, merged_by_github_id = $7
		`, claimID, prNumber, pr.GetUser().GetID(), pr.Head.GetSHA(), pr.GetMergedAt().Time, pr.GetMergeCommitSHA(), mergedByID)
		if err != nil {
			return err
		}
		if err := bounty.TransitionBounty(ctx, tx, bounty.Transition{
			BountyID: bountyID, To: "merged",
			Actor: actor, Action: "bounty.merged",
			Metadata: map[string]any{"pr": prNumber, "merge_commit_sha": pr.GetMergeCommitSHA()},
		}); err != nil {
			return err
		}
		if s.queue == nil {
			return errors.New("payout queue is not configured")
		}
		if err := s.queue.InsertTx(ctx, tx, &queue.ProcessPayoutArgs{BountyID: bountyID}, river.InsertOpts{Queue: "payouts"}); err != nil {
			return err
		}
		if s.notify != nil {
			if err := s.notify.EnqueueProjectEvent(ctx, tx, s.queue, projectID, "bounty.merged",
				map[string]any{"bounty_id": bountyID, "pr": prNumber}); err != nil {
				return err
			}
		}
		return nil
	case event.GetAction() == "closed":
		if bountyState != "in_review" {
			return nil
		}
		return bounty.TransitionBounty(ctx, tx, bounty.Transition{
			BountyID: bountyID, From: "in_review", To: "claimed",
			Actor: actor, Action: "bounty.review_closed",
			Metadata: map[string]any{"pr": prNumber},
		})
	default:
		_, err = tx.Exec(ctx, `
			INSERT INTO pull_requests (claim_id, github_pr_number, author_github_id, head_sha, merged)
			VALUES ($1, $2, $3, $4, false)
			ON CONFLICT (claim_id, github_pr_number) DO UPDATE SET head_sha = $4
		`, claimID, prNumber, pr.GetUser().GetID(), pr.Head.GetSHA())
		if err != nil {
			return err
		}
		if bountyState == "in_review" {
			return nil
		}
		return bounty.TransitionBounty(ctx, tx, bounty.Transition{
			BountyID: bountyID, To: "in_review",
			Actor: actor, Action: "bounty.in_review",
			Metadata: map[string]any{"pr": prNumber},
		})
	}
}

// processIssuesEvent flags active bounties on a deleted issue for staff
// review. All other issue activity needs no platform action.
func (s *Service) processIssuesEvent(ctx context.Context, tx pgx.Tx, event *github.IssuesEvent) error {
	if event.GetAction() != "deleted" {
		return nil
	}
	repoFullName := event.GetRepo().GetFullName()
	issueNum := event.GetIssue().GetNumber()
	var bountyID int64
	err := tx.QueryRow(ctx, `
		SELECT b.id FROM bounties b
		JOIN projects p ON b.project_id = p.id
		WHERE p.repo_full_name = $1 AND b.github_issue_number = $2 AND b.state IN ('open', 'claimed', 'in_review')
	`, repoFullName, issueNum).Scan(&bountyID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_log (actor, action, subject, metadata)
		VALUES ('github', 'issue.deleted_with_active_bounty', $1, $2::jsonb)
	`, "bounty:"+strconv.FormatInt(bountyID, 10), fmt.Sprintf(`{"issue":%d}`, issueNum))
	return err
}

// processInstallationEvent freezes projects when the GitHub App is removed or
// suspended, and reactivates them on unsuspend. Frozen projects keep their
// escrow but cannot move money until reactivated.
func (s *Service) processInstallationEvent(ctx context.Context, tx pgx.Tx, event *github.InstallationEvent) error {
	installationID := event.GetInstallation().GetID()
	switch event.GetAction() {
	case "deleted", "suspend":
		if _, err := tx.Exec(ctx, `UPDATE projects SET status = 'frozen' WHERE github_installation_id = $1`, installationID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO audit_log (actor, action, subject, metadata)
			VALUES ('github', 'project.frozen', $1, $2::jsonb)
		`, "installation:"+strconv.FormatInt(installationID, 10), fmt.Sprintf(`{"reason":"github_app_%s"}`, event.GetAction()))
		return err
	case "unsuspend":
		_, err := tx.Exec(ctx, `UPDATE projects SET status = 'active' WHERE github_installation_id = $1 AND status = 'frozen'`, installationID)
		return err
	default:
		return nil
	}
}

// processInstallationRepositoriesEvent freezes projects whose repository was
// removed from the GitHub App installation.
func (s *Service) processInstallationRepositoriesEvent(ctx context.Context, tx pgx.Tx, event *github.InstallationRepositoriesEvent) error {
	for _, repo := range event.RepositoriesRemoved {
		fullName := repo.GetFullName()
		if fullName == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE projects SET status = 'frozen' WHERE repo_full_name = $1`, fullName); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_log (actor, action, subject, metadata)
			VALUES ('github', 'project.frozen', $1, '{"reason":"repository removed from GitHub App installation"}')
		`, "project:"+fullName); err != nil {
			return err
		}
	}
	return nil
}

// VerifyMergeFresh re-checks via the GitHub API that a bounty's linked PR is
// still merged by its claim holder. The payout flow calls this just before
// sending (pay-time re-verification).
func (s *Service) VerifyMergeFresh(ctx context.Context, bountyID int64) (bool, error) {
	var installationID int64
	var repoFullName string
	var prNumber int
	var authorGithubID int64
	err := s.pool.QueryRow(ctx, `
		SELECT p.github_installation_id, p.repo_full_name, pr.github_pr_number, pr.author_github_id
		FROM pull_requests pr
		JOIN claims c ON pr.claim_id = c.id
		JOIN bounties b ON c.bounty_id = b.id
		JOIN projects p ON b.project_id = p.id
		WHERE b.id = $1 AND pr.merged
		ORDER BY pr.merged_at DESC NULLS LAST LIMIT 1
	`, bountyID).Scan(&installationID, &repoFullName, &prNumber, &authorGithubID)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	owner, repo, ok := splitRepoFullName(repoFullName)
	if !ok {
		return false, nil
	}
	client, err := s.newInstallationClient(installationID)
	if err != nil {
		return false, err
	}
	pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		return false, err
	}
	return pr.GetMerged() && pr.GetUser().GetID() == authorGithubID, nil
}

func splitRepoFullName(fullName string) (string, string, bool) {
	parts := strings.Split(fullName, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func extractIssueNumber(body string) int {
	patterns := []string{"Closes #", "Fixes #", "Resolves #", "closes #", "fixes #", "resolves #"}
	for _, p := range patterns {
		idx := strings.Index(body, p)
		if idx >= 0 {
			start := idx + len(p)
			end := start
			for end < len(body) && body[end] >= '0' && body[end] <= '9' {
				end++
			}
			if end > start {
				var num int
				fmt.Sscanf(body[start:end], "%d", &num)
				return num
			}
		}
	}
	return 0
}

func (s *Service) VerifyRepoAdmin(ctx context.Context, installationID int64, githubLogin string, repoFullName string) (bool, error) {
	client, err := s.GetInstallationClient(installationID)
	if err != nil {
		return false, err
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return false, errors.New("invalid repo format")
	}

	_, _, err = client.Repositories.Get(ctx, parts[0], parts[1])
	if err != nil {
		return false, err
	}

	perm, _, err := client.Repositories.GetPermissionLevel(ctx, parts[0], parts[1], githubLogin)
	if err != nil {
		return false, err
	}
	return perm.GetPermission() == "admin", nil
}

var oauth2Endpoint = oauth2.Endpoint{
	AuthURL:  "https://github.com/login/oauth/authorize",
	TokenURL: "https://github.com/login/oauth/access_token",
}

func (s *Service) GetOAuthURL(state, redirectURL string) string {
	config := &oauth2.Config{
		ClientID:     s.cfg.OAuthClientID,
		ClientSecret: s.cfg.OAuthClientSecret,
		Endpoint:     oauth2Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       []string{"read:user", "user:email"},
	}
	return config.AuthCodeURL(state, oauth2.AccessTypeOnline)
}

func (s *Service) ExchangeCode(ctx context.Context, code, redirectURL string) (*oauth2.Token, error) {
	config := &oauth2.Config{
		ClientID:     s.cfg.OAuthClientID,
		ClientSecret: s.cfg.OAuthClientSecret,
		Endpoint:     oauth2Endpoint,
		RedirectURL:  redirectURL,
	}
	return config.Exchange(ctx, code)
}

func (s *Service) GetUser(ctx context.Context, token *oauth2.Token) (*github.User, error) {
	client := github.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(token)))
	user, _, err := client.Users.Get(ctx, "")
	return user, err
}
