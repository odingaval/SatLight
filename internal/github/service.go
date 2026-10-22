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
	"strings"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/go-github/v60/github"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"golang.org/x/oauth2"
)

var (
	ErrInvalidSignature = errors.New("invalid webhook signature")
	ErrNotAdmin         = errors.New("user is not admin of repository")
	ErrPRNotMerged      = errors.New("pull request not merged")
	ErrPRNotLinked      = errors.New("pull request not linked to bounty")
)

type Service struct {
	cfg    *Config
	pool   *pgxpool.Pool
	client *github.Client
	log    zerolog.Logger
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
	return &Service{
		cfg:  c,
		pool: pool,
		log:  logger,
	}
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

func (s *Service) ProcessWebhook(ctx context.Context, deliveryID, event string, payload json.RawMessage) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO webhook_events (delivery_id, event, payload) VALUES ($1, $2, $3)
		ON CONFLICT (delivery_id) DO NOTHING
	`, deliveryID, event, payload)
	if err != nil {
		return err
	}

	var processed bool
	err = tx.QueryRow(ctx, `SELECT processed_at IS NOT NULL FROM webhook_events WHERE delivery_id = $1`, deliveryID).Scan(&processed)
	if err != nil {
		return err
	}
	if processed {
		return nil
	}

	switch event {
	case "pull_request":
		var prEvent github.PullRequestEvent
		if err := json.Unmarshal(payload, &prEvent); err != nil {
			return err
		}
		if err := s.handlePullRequestEvent(ctx, tx, prEvent); err != nil {
			return err
		}
	case "issues":
		var issueEvent github.IssuesEvent
		if err := json.Unmarshal(payload, &issueEvent); err != nil {
			return err
		}
	case "installation":
		var instEvent github.InstallationEvent
		if err := json.Unmarshal(payload, &instEvent); err != nil {
			return err
		}
	}

	_, err = tx.Exec(ctx, `UPDATE webhook_events SET processed_at = now() WHERE delivery_id = $1`, deliveryID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Service) handlePullRequestEvent(ctx context.Context, tx pgx.Tx, event github.PullRequestEvent) error {
	if event.GetAction() != "closed" || event.PullRequest == nil || event.PullRequest.MergedAt == nil {
		return nil
	}

	pr := event.PullRequest
	repo := event.Repo

	body := pr.GetBody()
	issueNum := extractIssueNumber(body)
	if issueNum == 0 {
		return nil
	}

	var bountyID int64
	err := tx.QueryRow(ctx, `
		SELECT b.id FROM bounties b
		JOIN projects p ON b.project_id = p.id
		WHERE p.repo_full_name = $1 AND b.github_issue_number = $2 AND b.state IN ('claimed', 'in_review')
	`, repo.GetFullName(), issueNum).Scan(&bountyID)
	if err != nil {
		return nil
	}

	var claimID int64
	err = tx.QueryRow(ctx, `SELECT id FROM claims WHERE bounty_id = $1 AND status = 'active'`, bountyID).Scan(&claimID)
	if err != nil {
		return nil
	}

	if pr.User.GetID() != getClaimUserID(ctx, tx, claimID) {
		return ErrPRNotLinked
	}

	mergedAt := pr.GetMergedAt().Time
	_, err = tx.Exec(ctx, `
		INSERT INTO pull_requests (claim_id, github_pr_number, author_github_id, head_sha, merged, merged_at, merge_commit_sha)
		VALUES ($1, $2, $3, $4, true, $5, $6)
		ON CONFLICT (claim_id, github_pr_number) DO UPDATE SET merged = true, merged_at = $5, merge_commit_sha = $6
	`, claimID, pr.GetNumber(), pr.User.GetID(), pr.Head.GetSHA(), mergedAt, pr.GetMergeCommitSHA())
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `UPDATE bounties SET state = 'merged', updated_at = now() WHERE id = $1`, bountyID)
	return err
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

func getClaimUserID(ctx context.Context, tx pgx.Tx, claimID int64) int64 {
	var userID int64
	tx.QueryRow(ctx, `SELECT user_id FROM claims WHERE id = $1`, claimID).Scan(&userID)
	return userID
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
