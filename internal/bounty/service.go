package bounty

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog"
	"github.com/satlight/satlight/internal/config"
	"github.com/satlight/satlight/internal/ledger"
	"github.com/satlight/satlight/internal/notify"
	"github.com/satlight/satlight/internal/queue"
)

var (
	ErrBountyNotFound           = errors.New("bounty not found")
	ErrBountyNotOpen            = errors.New("bounty is not open for claims")
	ErrBountyAlreadyClaimed     = errors.New("bounty already claimed")
	ErrDuplicateIssueBounty     = errors.New("this repository issue already has a bounty")
	ErrInsufficientFunds        = errors.New("insufficient wallet balance")
	ErrInvalidState             = errors.New("invalid bounty state for this operation")
	ErrNotAuthorized            = errors.New("not authorized")
	ErrDepositNotFound          = errors.New("deposit not found")
	ErrDepositUnderpaid         = errors.New("settled amount is below the invoice amount")
	ErrInvalidCursor            = errors.New("invalid bounty cursor")
	ErrInvalidExpiry            = errors.New("bounty expiry must be in the future")
	ErrIdempotencyConflict      = errors.New("idempotency key was reused with different request data")
	ErrFundingPaused            = errors.New("project funding is temporarily paused")
	ErrPayoutDestinationMissing = errors.New("payout destination not set")
	ErrUserBanned               = errors.New("user is banned")
	ErrProjectFrozen            = errors.New("project is frozen or closed")
	ErrSelfClaim                = errors.New("project owners cannot claim their own bounties")
	ErrClaimantAccountTooNew    = errors.New("GitHub account is too new to claim")
	ErrTooManyActiveClaims      = errors.New("too many active claims")
	ErrProjectNotFound          = errors.New("project not found")
	ErrDisputeNotFound          = errors.New("dispute not found")
	ErrDisputeClosed            = errors.New("dispute is already resolved")
	ErrDisputePayNotReady       = errors.New("cannot release payout without a verified merge")
)

type Service struct {
	cfg    *config.Config
	pool   *pgxpool.Pool
	log    zerolog.Logger
	ledger *ledger.Service
	queue  *queue.Client
	notify *notify.Service
}

func NewService(cfg *config.Config, pool *pgxpool.Pool) *Service {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: gin.DefaultWriter, TimeFormat: zerolog.TimeFormatUnix}).With().Timestamp().Logger()
	return &Service{
		cfg:    cfg,
		pool:   pool,
		log:    logger,
		ledger: nil,
		queue:  nil,
	}
}

func (s *Service) SetDependencies(ledger *ledger.Service, queue *queue.Client) {
	s.ledger = ledger
	s.queue = queue
}

// SetNotifier wires maintainer-webhook notifications (optional; skipped when nil).
func (s *Service) SetNotifier(n *notify.Service) {
	s.notify = n
}

type Bounty struct {
	ID                int64     `json:"id"`
	ProjectID         int64     `json:"project_id"`
	RepoFullName      string    `json:"repo_full_name"`
	GitHubIssueNumber int       `json:"github_issue_number"`
	Title             string    `json:"title"`
	Description       string    `json:"description"`
	Difficulty        string    `json:"difficulty"`
	RewardSats        int64     `json:"reward_sats"`
	FeeSats           int64     `json:"fee_sats"`
	NetPayoutSats     int64     `json:"net_payout_sats"`
	State             string    `json:"state"`
	ExpiresAt         time.Time `json:"expires_at"`
	CreatedAt         time.Time `json:"created_at"`
}

type CreateBountyParams struct {
	ProjectID         int64
	GitHubIssueNumber int
	Title             string
	Description       string
	Difficulty        string
	RewardSats        int64
	ExpiresAt         *time.Time
	// IdempotencyKey makes retries safe: the same key returns the original
	// bounty, while the same key with a different IdempotencyHash is rejected.
	IdempotencyKey  string
	IdempotencyHash string
}

// CreateBounty publishes a bounty and reserves its reward in one transaction.
// It returns created=false when the idempotency key replays an earlier call.
func (s *Service) CreateBounty(ctx context.Context, params CreateBountyParams) (*Bounty, bool, error) {
	if s.ledger == nil || s.queue == nil {
		return nil, false, errors.New("bounty funding dependencies are not configured")
	}
	if params.RewardSats < s.cfg.MinRewardSats {
		return nil, false, errors.New("reward below minimum")
	}
	// Replay check first: a retry must return the original bounty even if
	// funding was paused (or the bounty moved on) since the first call.
	if params.IdempotencyKey != "" {
		existing, _, err := s.findBountyByIdempotencyKey(ctx, params.ProjectID, params.IdempotencyKey, params.IdempotencyHash)
		if err == nil {
			return existing, false, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, false, err
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)
	if err := requireFundingEnabled(ctx, tx); err != nil {
		return nil, false, err
	}
	if err := requireProjectActive(ctx, tx, params.ProjectID); err != nil {
		return nil, false, err
	}

	expiresAt := params.ExpiresAt
	if expiresAt == nil {
		exp := time.Now().Add(s.cfg.BountyDefaultExpiryDuration())
		expiresAt = &exp
	}
	if !expiresAt.After(time.Now()) {
		return nil, false, ErrInvalidExpiry
	}

	feeSats := calculatePlatformFee(params.RewardSats, s.cfg.PlatformFeeBPS)
	netPayoutSats := params.RewardSats - feeSats

	var bounty Bounty
	err = tx.QueryRow(ctx, `
		INSERT INTO bounties (project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at, idempotency_key, idempotency_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'draft', $8, $9, $10)
		RETURNING id, project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at, created_at
	`, params.ProjectID, params.GitHubIssueNumber, params.Title, params.Description, params.Difficulty, params.RewardSats, feeSats, expiresAt, nullString(params.IdempotencyKey), nullString(params.IdempotencyHash)).Scan(
		&bounty.ID, &bounty.ProjectID, &bounty.GitHubIssueNumber, &bounty.Title, &bounty.Description,
		&bounty.Difficulty, &bounty.RewardSats, &bounty.FeeSats, &bounty.State, &bounty.ExpiresAt, &bounty.CreatedAt,
	)
	if err != nil {
		// The failed insert aborts this transaction; resolve the conflict
		// with a fresh read after rolling back.
		_ = tx.Rollback(ctx)
		if pgErr := asPgError(err); pgErr != nil && pgErr.Code == "23505" {
			switch pgErr.ConstraintName {
			case "bounties_project_idempotency_idx":
				return s.findBountyByIdempotencyKey(ctx, params.ProjectID, params.IdempotencyKey, params.IdempotencyHash)
			case "bounties_project_id_github_issue_number_key":
				return nil, false, ErrDuplicateIssueBounty
			}
		}
		return nil, false, err
	}
	var availableSats int64
	if err := tx.QueryRow(ctx, `SELECT available_sats FROM wallets WHERE project_id = $1 FOR UPDATE`, params.ProjectID).Scan(&availableSats); err != nil {
		return nil, false, err
	}
	if availableSats < params.RewardSats {
		return nil, false, ErrInsufficientFunds
	}
	if _, err := tx.Exec(ctx, `UPDATE wallets SET available_sats = available_sats - $1, reserved_sats = reserved_sats + $1, updated_at = now() WHERE project_id = $2`, params.RewardSats, params.ProjectID); err != nil {
		return nil, false, err
	}
	if err := TransitionBounty(ctx, tx, Transition{
		BountyID: bounty.ID, From: "draft", To: "open",
		Actor: "project:" + strconv.FormatInt(params.ProjectID, 10), Action: "bounty.opened",
		Metadata: map[string]any{"reward_sats": params.RewardSats},
	}); err != nil {
		return nil, false, err
	}
	if err := s.ledger.ReserveBountyFunds(ctx, tx, bounty.ProjectID, bounty.ID, bounty.RewardSats, bounty.FeeSats); err != nil {
		return nil, false, err
	}
	if err := s.queue.InsertTx(ctx, tx, queue.BountyExpiryArgs{BountyID: bounty.ID}, river.InsertOpts{ScheduledAt: *expiresAt}); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	bounty.State = "open"
	bounty.NetPayoutSats = netPayoutSats
	return &bounty, true, nil
}

// findBountyByIdempotencyKey returns the bounty created by an earlier call
// with the same key, or ErrIdempotencyConflict when the key was reused with
// different request data.
func (s *Service) findBountyByIdempotencyKey(ctx context.Context, projectID int64, key, hash string) (*Bounty, bool, error) {
	var bounty Bounty
	var storedHash *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at, created_at, idempotency_hash
		FROM bounties WHERE project_id = $1 AND idempotency_key = $2
	`, projectID, key).Scan(
		&bounty.ID, &bounty.ProjectID, &bounty.GitHubIssueNumber, &bounty.Title, &bounty.Description,
		&bounty.Difficulty, &bounty.RewardSats, &bounty.FeeSats, &bounty.State, &bounty.ExpiresAt, &bounty.CreatedAt, &storedHash,
	)
	if err != nil {
		return nil, false, err
	}
	if storedHash == nil || *storedHash != hash {
		return nil, false, ErrIdempotencyConflict
	}
	bounty.NetPayoutSats = bounty.RewardSats - bounty.FeeSats
	return &bounty, false, nil
}

func asPgError(err error) *pgconn.PgError {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr
	}
	return nil
}

// nullString stores empty optionals as NULL so partial unique indexes that
// ignore NULLs keep working for callers without a value.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func calculatePlatformFee(rewardSats int64, feeBPS int) int64 {
	fee := rewardSats * int64(feeBPS) / 10000
	if rewardSats > 0 && feeBPS > 0 && fee == 0 {
		return 1
	}
	return fee
}

func (s *Service) FundBounty(ctx context.Context, bountyID, projectID int64) (*Bounty, error) {
	if s.ledger == nil {
		return nil, errors.New("ledger service is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := requireFundingEnabled(ctx, tx); err != nil {
		return nil, err
	}
	if err := requireProjectActive(ctx, tx, projectID); err != nil {
		return nil, err
	}

	var bounty Bounty
	err = tx.QueryRow(ctx, `
		SELECT id, project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at, created_at
		FROM bounties WHERE id = $1 AND project_id = $2 FOR UPDATE
	`, bountyID, projectID).Scan(
		&bounty.ID, &bounty.ProjectID, &bounty.GitHubIssueNumber, &bounty.Title, &bounty.Description,
		&bounty.Difficulty, &bounty.RewardSats, &bounty.FeeSats, &bounty.State, &bounty.ExpiresAt, &bounty.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBountyNotFound
	}
	if err != nil {
		return nil, err
	}

	if bounty.State != "draft" {
		return nil, ErrInvalidState
	}

	var availableSats int64
	err = tx.QueryRow(ctx, `SELECT available_sats FROM wallets WHERE project_id = $1 FOR UPDATE`, projectID).Scan(&availableSats)
	if err != nil {
		return nil, err
	}

	if availableSats < bounty.RewardSats {
		return nil, ErrInsufficientFunds
	}

	_, err = tx.Exec(ctx, `UPDATE wallets SET available_sats = available_sats - $1, reserved_sats = reserved_sats + $1, updated_at = now() WHERE project_id = $2`, bounty.RewardSats, projectID)
	if err != nil {
		return nil, err
	}

	if err := TransitionBounty(ctx, tx, Transition{
		BountyID: bountyID, From: "draft", To: "open",
		Actor: "project:" + strconv.FormatInt(projectID, 10), Action: "bounty.opened",
		Metadata: map[string]any{"reward_sats": bounty.RewardSats},
	}); err != nil {
		return nil, err
	}

	bounty.State = "open"

	if err := s.ledger.ReserveBountyFunds(ctx, tx, bounty.ProjectID, bounty.ID, bounty.RewardSats, bounty.FeeSats); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &bounty, nil
}

func (s *Service) GetBounty(ctx context.Context, id int64) (*Bounty, error) {
	var bounty Bounty
	err := s.pool.QueryRow(ctx, `
		SELECT id, project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at, created_at
		FROM bounties WHERE id = $1
	`, id).Scan(
		&bounty.ID, &bounty.ProjectID, &bounty.GitHubIssueNumber, &bounty.Title, &bounty.Description,
		&bounty.Difficulty, &bounty.RewardSats, &bounty.FeeSats, &bounty.State, &bounty.ExpiresAt, &bounty.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBountyNotFound
	}
	if err != nil {
		return nil, err
	}
	bounty.NetPayoutSats = bounty.RewardSats - bounty.FeeSats
	return &bounty, nil
}

func (s *Service) ListBounties(ctx context.Context, filters ListFilters) ([]*Bounty, string, error) {
	query, args, limit, err := buildListBountiesQuery(filters)
	if err != nil {
		return nil, "", err
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var bounties []*Bounty
	for rows.Next() {
		var b Bounty
		err := rows.Scan(&b.ID, &b.ProjectID, &b.GitHubIssueNumber, &b.Title, &b.Description, &b.Difficulty, &b.RewardSats, &b.FeeSats, &b.State, &b.ExpiresAt, &b.CreatedAt, &b.RepoFullName)
		if err != nil {
			return nil, "", err
		}
		b.NetPayoutSats = b.RewardSats - b.FeeSats
		bounties = append(bounties, &b)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	var nextCursor string
	if len(bounties) > limit {
		last := bounties[limit-1]
		nextCursor = encodeBountyCursor(last.CreatedAt, last.ID)
		bounties = bounties[:limit]
	}

	return bounties, nextCursor, nil
}

func buildListBountiesQuery(filters ListFilters) (string, []interface{}, int, error) {
	query := `
		SELECT b.id, b.project_id, b.github_issue_number, b.title, b.description, b.difficulty, b.reward_sats, b.fee_sats, b.state, b.expires_at, b.created_at, p.repo_full_name
		FROM bounties b
		JOIN projects p ON b.project_id = p.id
		WHERE b.state = 'open' AND b.expires_at > now()
	`
	args := []interface{}{}
	placeholder := func() string { return "$" + strconv.Itoa(len(args)+1) }

	if filters.Query != "" {
		query += ` AND (b.title ILIKE ` + placeholder() + ` OR b.description ILIKE ` + placeholder() + `)`
		args = append(args, "%"+filters.Query+"%")
	}
	if filters.Language != "" {
		query += ` AND p.repo_full_name ILIKE ` + placeholder()
		args = append(args, "%"+filters.Language+"%")
	}
	if filters.Difficulty != "" {
		query += ` AND b.difficulty = ` + placeholder()
		args = append(args, filters.Difficulty)
	}
	if filters.MinReward > 0 {
		query += ` AND b.reward_sats >= ` + placeholder()
		args = append(args, filters.MinReward)
	}
	if filters.MaxReward > 0 {
		query += ` AND b.reward_sats <= ` + placeholder()
		args = append(args, filters.MaxReward)
	}

	if filters.Cursor != "" {
		cursorTime, cursorID, err := parseBountyCursor(filters.Cursor)
		if err != nil {
			return "", nil, 0, err
		}
		timePlaceholder := placeholder()
		args = append(args, cursorTime)
		idPlaceholder := placeholder()
		args = append(args, cursorID)
		query += ` AND (b.created_at, b.id) < (` + timePlaceholder + `::timestamptz, ` + idPlaceholder + `::bigint)`
	}

	limit := filters.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	query += ` ORDER BY b.created_at DESC, b.id DESC LIMIT ` + placeholder()
	args = append(args, limit+1)
	return query, args, limit, nil
}

func parseBountyCursor(cursor string) (time.Time, int64, error) {
	timestamp, rawID, ok := strings.Cut(cursor, "|")
	if !ok || timestamp == "" || rawID == "" {
		return time.Time{}, 0, ErrInvalidCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return time.Time{}, 0, ErrInvalidCursor
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 {
		return time.Time{}, 0, ErrInvalidCursor
	}
	return createdAt, id, nil
}

func encodeBountyCursor(createdAt time.Time, id int64) string {
	return createdAt.UTC().Format(time.RFC3339Nano) + "|" + strconv.FormatInt(id, 10)
}

type ListFilters struct {
	Query      string
	Language   string
	Difficulty string
	MinReward  int64
	MaxReward  int64
	Cursor     string
	Limit      int
}

type Claim struct {
	ID        int64     `json:"id"`
	BountyID  int64     `json:"bounty_id"`
	UserID    int64     `json:"user_id"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// ClaimBounty reserves an open bounty for one contributor. It returns
// created=false when the idempotency key replays an earlier call.
func (s *Service) ClaimBounty(ctx context.Context, bountyID, userID int64, idempotencyKey string) (*Claim, bool, error) {
	if s.queue == nil {
		return nil, false, errors.New("claim expiry queue is not configured")
	}
	// Replay check first: a retry must return the original claim even
	// though the bounty already left the open state.
	if idempotencyKey != "" {
		existing, _, err := s.findClaimByIdempotencyKey(ctx, bountyID, userID, idempotencyKey)
		if err == nil {
			return existing, false, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, false, err
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)

	var bounty Bounty
	err = tx.QueryRow(ctx, `
		SELECT id, project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at, created_at
		FROM bounties WHERE id = $1 FOR UPDATE
	`, bountyID).Scan(
		&bounty.ID, &bounty.ProjectID, &bounty.GitHubIssueNumber, &bounty.Title, &bounty.Description,
		&bounty.Difficulty, &bounty.RewardSats, &bounty.FeeSats, &bounty.State, &bounty.ExpiresAt, &bounty.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrBountyNotFound
	}
	if err != nil {
		return nil, false, err
	}

	if bounty.State != "open" {
		return nil, false, ErrBountyNotOpen
	}

	var claimant struct {
		lightningAddress string
		isBanned         bool
		githubCreatedAt  *time.Time
	}
	err = tx.QueryRow(ctx, `SELECT COALESCE(lightning_address, ''), is_banned, github_created_at FROM users WHERE id = $1`, userID).Scan(
		&claimant.lightningAddress, &claimant.isBanned, &claimant.githubCreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, errors.New("user not found")
	}
	if err != nil {
		return nil, false, err
	}
	if claimant.isBanned {
		return nil, false, ErrUserBanned
	}
	if claimant.lightningAddress == "" {
		return nil, false, ErrPayoutDestinationMissing
	}
	if minAgeDays := s.cfg.ClaimMinGitHubAccountAgeDays; minAgeDays > 0 {
		if claimant.githubCreatedAt == nil || time.Since(*claimant.githubCreatedAt) < time.Duration(minAgeDays)*24*time.Hour {
			return nil, false, ErrClaimantAccountTooNew
		}
	}
	var projectStatus string
	var projectOwnerID int64
	err = tx.QueryRow(ctx, `SELECT status, owner_user_id FROM projects WHERE id = $1`, bounty.ProjectID).Scan(&projectStatus, &projectOwnerID)
	if err != nil {
		return nil, false, err
	}
	if projectStatus != "active" {
		return nil, false, ErrProjectFrozen
	}
	if projectOwnerID == userID {
		return nil, false, ErrSelfClaim
	}
	if max := s.cfg.ClaimMaxActiveClaims; max > 0 {
		var active int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM claims WHERE user_id = $1 AND status = 'active'`, userID).Scan(&active); err != nil {
			return nil, false, err
		}
		if active >= max {
			return nil, false, ErrTooManyActiveClaims
		}
	}

	expiresAt := time.Now().Add(s.cfg.ClaimExpiryDuration())

	var claim Claim
	err = tx.QueryRow(ctx, `
		INSERT INTO claims (bounty_id, user_id, status, expires_at, idempotency_key)
		VALUES ($1, $2, 'active', $3, $4)
		RETURNING id, bounty_id, user_id, status, expires_at, created_at
	`, bountyID, userID, expiresAt, nullString(idempotencyKey)).Scan(
		&claim.ID, &claim.BountyID, &claim.UserID, &claim.Status, &claim.ExpiresAt, &claim.CreatedAt,
	)
	if err != nil {
		// The failed insert aborts this transaction; resolve the conflict
		// with a fresh read after rolling back.
		_ = tx.Rollback(ctx)
		if pgErr := asPgError(err); pgErr != nil && pgErr.Code == "23505" {
			switch pgErr.ConstraintName {
			case "claims_bounty_user_idempotency_idx":
				return s.findClaimByIdempotencyKey(ctx, bountyID, userID, idempotencyKey)
			case "claims_one_active_per_bounty":
				return nil, false, ErrBountyAlreadyClaimed
			}
		}
		return nil, false, err
	}

	if err := TransitionBounty(ctx, tx, Transition{
		BountyID: bountyID, From: "open", To: "claimed",
		Actor: "user:" + strconv.FormatInt(userID, 10), Action: "bounty.claimed",
	}); err != nil {
		return nil, false, err
	}

	if err := s.queue.InsertTx(ctx, tx, &queue.ClaimExpiryArgs{ClaimID: claim.ID}, river.InsertOpts{ScheduledAt: expiresAt}); err != nil {
		return nil, false, err
	}
	if s.notify != nil {
		if err := s.notify.EnqueueProjectEvent(ctx, tx, s.queue, bounty.ProjectID, "claim.created",
			map[string]any{"bounty_id": bountyID, "claim_id": claim.ID}); err != nil {
			return nil, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}

	return &claim, true, nil
}

// findClaimByIdempotencyKey returns the claim created by an earlier call with
// the same key. Claim parameters are fully determined by (bounty, user), so a
// key match always replays the same request.
func (s *Service) findClaimByIdempotencyKey(ctx context.Context, bountyID, userID int64, key string) (*Claim, bool, error) {
	var claim Claim
	err := s.pool.QueryRow(ctx, `
		SELECT id, bounty_id, user_id, status, expires_at, created_at
		FROM claims WHERE bounty_id = $1 AND user_id = $2 AND idempotency_key = $3
	`, bountyID, userID, key).Scan(
		&claim.ID, &claim.BountyID, &claim.UserID, &claim.Status, &claim.ExpiresAt, &claim.CreatedAt,
	)
	if err != nil {
		return nil, false, err
	}
	return &claim, false, nil
}

// ExpireClaim releases an elapsed active claim. If its bounty has also expired,
// the reserved reward is refunded instead of reopening the bounty.
func (s *Service) ExpireClaim(ctx context.Context, claimID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var bountyID int64
	err = tx.QueryRow(ctx, `SELECT bounty_id FROM claims WHERE id = $1`, claimID).Scan(&bountyID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var bounty Bounty
	err = tx.QueryRow(ctx, `SELECT id, project_id, reward_sats, state, expires_at FROM bounties WHERE id = $1 FOR UPDATE`, bountyID).Scan(
		&bounty.ID, &bounty.ProjectID, &bounty.RewardSats, &bounty.State, &bounty.ExpiresAt,
	)
	if err != nil {
		return err
	}
	var claim Claim
	err = tx.QueryRow(ctx, `SELECT id, status, expires_at FROM claims WHERE id = $1 FOR UPDATE`, claimID).Scan(&claim.ID, &claim.Status, &claim.ExpiresAt)
	if err != nil {
		return err
	}
	if claim.Status != "active" || claim.ExpiresAt.After(time.Now()) || bounty.State != "claimed" {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE claims SET status = 'expired' WHERE id = $1`, claimID); err != nil {
		return err
	}
	if !bounty.ExpiresAt.After(time.Now()) {
		if s.ledger == nil {
			return errors.New("ledger service is not configured")
		}
		result, err := tx.Exec(ctx, `UPDATE wallets SET available_sats = available_sats + $1, reserved_sats = reserved_sats - $1, updated_at = now() WHERE project_id = $2 AND reserved_sats >= $1`, bounty.RewardSats, bounty.ProjectID)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return errors.New("project wallet is missing or has insufficient reserved funds for claim expiry")
		}
		if err := TransitionBounty(ctx, tx, Transition{
			BountyID: bountyID, From: "claimed", To: "expired",
			Actor: "system", Action: "bounty.expired",
			Metadata: map[string]any{"refund_sats": bounty.RewardSats},
		}); err != nil {
			return err
		}
		if err := s.ledger.RefundBountyFunds(ctx, tx, bounty.ProjectID, bounty.ID, bounty.RewardSats); err != nil {
			return err
		}
	} else if err := TransitionBounty(ctx, tx, Transition{
		BountyID: bountyID, From: "claimed", To: "open",
		Actor: "system", Action: "bounty.reopened",
	}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log (actor, action, subject) VALUES ('system', 'claim.expired', $1)`, "claim:"+strconv.FormatInt(claimID, 10)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) ReleaseClaim(ctx context.Context, bountyID, userID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var bounty Bounty
	err = tx.QueryRow(ctx, `SELECT id, project_id, reward_sats, state, expires_at FROM bounties WHERE id = $1 FOR UPDATE`, bountyID).Scan(
		&bounty.ID, &bounty.ProjectID, &bounty.RewardSats, &bounty.State, &bounty.ExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBountyNotFound
	}
	if err != nil {
		return err
	}
	var claimID int64
	err = tx.QueryRow(ctx, `UPDATE claims SET status = 'released' WHERE bounty_id = $1 AND user_id = $2 AND status = 'active' RETURNING id`, bountyID, userID).Scan(&claimID)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("no active claim found")
	}
	if err != nil {
		return err
	}
	if bounty.State != "claimed" {
		return ErrInvalidState
	}
	if !bounty.ExpiresAt.After(time.Now()) {
		if s.ledger == nil {
			return errors.New("ledger service is not configured")
		}
		result, err := tx.Exec(ctx, `UPDATE wallets SET available_sats = available_sats + $1, reserved_sats = reserved_sats - $1, updated_at = now() WHERE project_id = $2 AND reserved_sats >= $1`, bounty.RewardSats, bounty.ProjectID)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return errors.New("project wallet is missing or has insufficient reserved funds for claim release")
		}
		if err := TransitionBounty(ctx, tx, Transition{
			BountyID: bountyID, From: "claimed", To: "expired",
			Actor: "user:" + strconv.FormatInt(userID, 10), Action: "bounty.expired",
			Metadata: map[string]any{"refund_sats": bounty.RewardSats},
		}); err != nil {
			return err
		}
		if err := s.ledger.RefundBountyFunds(ctx, tx, bounty.ProjectID, bounty.ID, bounty.RewardSats); err != nil {
			return err
		}
	} else if err := TransitionBounty(ctx, tx, Transition{
		BountyID: bountyID, From: "claimed", To: "open",
		Actor: "user:" + strconv.FormatInt(userID, 10), Action: "bounty.reopened",
	}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log (actor, action, subject) VALUES ($1, 'claim.released', $2)`, "user:"+strconv.FormatInt(userID, 10), "claim:"+strconv.FormatInt(claimID, 10)); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Service) CancelBounty(ctx context.Context, bountyID, projectID int64) (*Bounty, error) {
	if s.ledger == nil {
		return nil, errors.New("ledger service is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var bounty Bounty
	err = tx.QueryRow(ctx, `
		SELECT id, project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at, created_at
		FROM bounties WHERE id = $1 AND project_id = $2 FOR UPDATE
	`, bountyID, projectID).Scan(
		&bounty.ID, &bounty.ProjectID, &bounty.GitHubIssueNumber, &bounty.Title, &bounty.Description,
		&bounty.Difficulty, &bounty.RewardSats, &bounty.FeeSats, &bounty.State, &bounty.ExpiresAt, &bounty.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBountyNotFound
	}
	if err != nil {
		return nil, err
	}

	if !canCancelBounty(bounty.State) {
		return nil, ErrInvalidState
	}

	_, err = tx.Exec(ctx, `UPDATE wallets SET available_sats = available_sats + $1, reserved_sats = reserved_sats - $1, updated_at = now() WHERE project_id = $2`, bounty.RewardSats, projectID)
	if err != nil {
		return nil, err
	}

	if err := TransitionBounty(ctx, tx, Transition{
		BountyID: bountyID, From: "open", To: "cancelled",
		Actor: "project:" + strconv.FormatInt(projectID, 10), Action: "bounty.cancelled",
		Metadata: map[string]any{"refund_sats": bounty.RewardSats},
	}); err != nil {
		return nil, err
	}

	if err := s.ledger.RefundBountyFunds(ctx, tx, bounty.ProjectID, bounty.ID, bounty.RewardSats); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	bounty.State = "cancelled"
	return &bounty, nil
}

// ExpireBounty refunds an open bounty after its expiry. It is safe for a
// scheduled job to run more than once or after a maintainer already cancelled.
func (s *Service) ExpireBounty(ctx context.Context, bountyID int64) error {
	if s.ledger == nil {
		return errors.New("ledger service is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var bounty Bounty
	err = tx.QueryRow(ctx, `
		SELECT id, project_id, reward_sats, state, expires_at
		FROM bounties WHERE id = $1 FOR UPDATE
	`, bountyID).Scan(&bounty.ID, &bounty.ProjectID, &bounty.RewardSats, &bounty.State, &bounty.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBountyNotFound
	}
	if err != nil {
		return err
	}
	if !canExpireBounty(bounty.State, bounty.ExpiresAt, time.Now()) {
		return nil
	}

	walletUpdate, err := tx.Exec(ctx, `
		UPDATE wallets SET available_sats = available_sats + $1,
			reserved_sats = reserved_sats - $1, updated_at = now()
		WHERE project_id = $2 AND reserved_sats >= $1
	`, bounty.RewardSats, bounty.ProjectID)
	if err != nil {
		return err
	}
	if walletUpdate.RowsAffected() != 1 {
		return errors.New("project wallet is missing or has insufficient reserved funds for bounty expiry")
	}
	if err := TransitionBounty(ctx, tx, Transition{
		BountyID: bounty.ID, From: "open", To: "expired",
		Actor: "system", Action: "bounty.expired",
		Metadata: map[string]any{"refund_sats": bounty.RewardSats},
	}); err != nil {
		return err
	}
	if err := s.ledger.RefundBountyFunds(ctx, tx, bounty.ProjectID, bounty.ID, bounty.RewardSats); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func canExpireBounty(state string, expiresAt, now time.Time) bool {
	return state == "open" && !expiresAt.After(now)
}

func canCancelBounty(state string) bool {
	return validBountyTransition(state, "cancelled")
}

// allowedBountyTransitions is the single source of truth for the bounty
// lifecycle (see docs/specs/state-machine.md). Paid, expired, and cancelled
// are terminal.
var allowedBountyTransitions = map[string][]string{
	"draft":         {"open"},
	"open":          {"claimed", "cancelled", "expired", "disputed"},
	"claimed":       {"open", "in_review", "merged", "expired", "disputed"},
	"in_review":     {"claimed", "merged", "disputed"},
	"merged":        {"paid", "payout_failed"},
	"payout_failed": {"merged"},
	"disputed":      {"open", "claimed", "in_review", "merged", "cancelled"},
}

func validBountyTransition(from, to string) bool {
	for _, next := range allowedBountyTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// Transition describes a single audited bounty state change.
type Transition struct {
	BountyID int64
	// From is the expected current state; empty skips the expectation check
	// (the table validation against the actual state still applies).
	From     string
	To       string
	Actor    string
	Action   string
	Metadata map[string]any
}

// TransitionBounty is the only sanctioned way to change bounty state. It
// locks the row, validates the move against the lifecycle table, updates the
// state, and writes an audit entry, all inside the caller's transaction.
func TransitionBounty(ctx context.Context, tx pgx.Tx, tr Transition) error {
	var current string
	err := tx.QueryRow(ctx, `SELECT state FROM bounties WHERE id = $1 FOR UPDATE`, tr.BountyID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBountyNotFound
	}
	if err != nil {
		return err
	}
	if tr.From != "" && current != tr.From {
		return fmt.Errorf("%w: expected %s, found %s", ErrInvalidState, tr.From, current)
	}
	if !validBountyTransition(current, tr.To) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidState, current, tr.To)
	}
	if _, err := tx.Exec(ctx, `UPDATE bounties SET state = $1, updated_at = now() WHERE id = $2`, tr.To, tr.BountyID); err != nil {
		return err
	}
	var metadata any
	if len(tr.Metadata) > 0 {
		data, err := json.Marshal(tr.Metadata)
		if err != nil {
			return err
		}
		metadata = string(data)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_log (actor, action, subject, metadata)
		VALUES ($1, $2, $3, $4::jsonb)
	`, tr.Actor, tr.Action, "bounty:"+strconv.FormatInt(tr.BountyID, 10), metadata)
	return err
}

// DisputeBounty moves a bounty with reserved funds into disputed state and
// records the dispute, atomically and audited.
func (s *Service) DisputeBounty(ctx context.Context, bountyID, userID int64, reason, evidence string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var state string
	err = tx.QueryRow(ctx, `SELECT state FROM bounties WHERE id = $1 FOR UPDATE`, bountyID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBountyNotFound
	}
	if err != nil {
		return err
	}
	if !canDisputeBounty(state) {
		return ErrInvalidState
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO disputes (bounty_id, opened_by, reason, evidence, previous_state)
		VALUES ($1, $2, $3, $4, $5)
	`, bountyID, userID, reason, nullString(evidence), state); err != nil {
		return err
	}
	if err := TransitionBounty(ctx, tx, Transition{
		BountyID: bountyID, From: state, To: "disputed",
		Actor: "user:" + strconv.FormatInt(userID, 10), Action: "bounty.disputed",
		Metadata: map[string]any{"reason": reason},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func canDisputeBounty(state string) bool {
	return validBountyTransition(state, "disputed")
}

func validDisputeResolution(resolution string) bool {
	switch resolution {
	case "resolved_pay", "resolved_cancel", "resolved_other":
		return true
	default:
		return false
	}
}

// ResolveDispute applies a project owner's decision on an open dispute.
//   - resolved_cancel refunds the reservation and cancels the bounty.
//   - resolved_pay moves the bounty to merged so the payout flow can pay the
//     contributor; it requires a verified merged PR to preserve the
//     no-payout-without-merge invariant.
//   - resolved_other returns the bounty to its pre-dispute state.
func (s *Service) ResolveDispute(ctx context.Context, disputeID, resolverUserID int64, resolution string) error {
	if s.ledger == nil {
		return errors.New("ledger service is not configured")
	}
	if !validDisputeResolution(resolution) {
		return errors.New("unknown dispute resolution")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var disputeBountyID int64
	var disputeStatus string
	var previousState *string
	err = tx.QueryRow(ctx, `SELECT bounty_id, status, previous_state FROM disputes WHERE id = $1 FOR UPDATE`, disputeID).Scan(&disputeBountyID, &disputeStatus, &previousState)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDisputeNotFound
	}
	if err != nil {
		return err
	}
	if disputeStatus != "open" {
		return ErrDisputeClosed
	}
	var bounty Bounty
	err = tx.QueryRow(ctx, `SELECT id, project_id, reward_sats, state FROM bounties WHERE id = $1 FOR UPDATE`, disputeBountyID).Scan(
		&bounty.ID, &bounty.ProjectID, &bounty.RewardSats, &bounty.State,
	)
	if err != nil {
		return err
	}
	if bounty.State != "disputed" {
		return ErrInvalidState
	}
	var ownerUserID int64
	if err := tx.QueryRow(ctx, `SELECT owner_user_id FROM projects WHERE id = $1`, bounty.ProjectID).Scan(&ownerUserID); err != nil {
		return err
	}
	if ownerUserID != resolverUserID {
		return ErrNotAuthorized
	}

	switch resolution {
	case "resolved_cancel":
		result, err := tx.Exec(ctx, `UPDATE wallets SET available_sats = available_sats + $1, reserved_sats = reserved_sats - $1, updated_at = now() WHERE project_id = $2 AND reserved_sats >= $1`, bounty.RewardSats, bounty.ProjectID)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return errors.New("project wallet is missing or has insufficient reserved funds for dispute resolution")
		}
		if err := TransitionBounty(ctx, tx, Transition{
			BountyID: bounty.ID, From: "disputed", To: "cancelled",
			Actor: "user:" + strconv.FormatInt(resolverUserID, 10), Action: "bounty.dispute_resolved",
			Metadata: map[string]any{"resolution": resolution, "refund_sats": bounty.RewardSats},
		}); err != nil {
			return err
		}
		if err := s.ledger.RefundBountyFunds(ctx, tx, bounty.ProjectID, bounty.ID, bounty.RewardSats); err != nil {
			return err
		}
	case "resolved_pay":
		var merged bool
		err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pull_requests pr
				JOIN claims c ON pr.claim_id = c.id
				WHERE c.bounty_id = $1 AND pr.merged
			)`, bounty.ID).Scan(&merged)
		if err != nil {
			return err
		}
		if !merged {
			return ErrDisputePayNotReady
		}
		if err := TransitionBounty(ctx, tx, Transition{
			BountyID: bounty.ID, From: "disputed", To: "merged",
			Actor: "user:" + strconv.FormatInt(resolverUserID, 10), Action: "bounty.dispute_resolved",
			Metadata: map[string]any{"resolution": resolution},
		}); err != nil {
			return err
		}
		if s.queue == nil {
			return errors.New("payout queue is not configured")
		}
		if err := s.queue.InsertTx(ctx, tx, &queue.ProcessPayoutArgs{BountyID: bounty.ID}, river.InsertOpts{Queue: "payouts"}); err != nil {
			return err
		}
	case "resolved_other":
		prev := "open"
		if previousState != nil && *previousState != "" {
			prev = *previousState
		}
		if err := TransitionBounty(ctx, tx, Transition{
			BountyID: bounty.ID, From: "disputed", To: prev,
			Actor: "user:" + strconv.FormatInt(resolverUserID, 10), Action: "bounty.dispute_resolved",
			Metadata: map[string]any{"resolution": resolution},
		}); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE disputes SET status = $1, resolved_at = now() WHERE id = $2`, resolution, disputeID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_log (actor, action, subject, metadata)
		VALUES ($1, 'dispute.resolved', $2, jsonb_build_object('resolution', $3::text))
	`, "user:"+strconv.FormatInt(resolverUserID, 10), "dispute:"+strconv.FormatInt(disputeID, 10), resolution); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) GetBountyWithProject(ctx context.Context, bountyID int64) (*Bounty, *Project, error) {
	var bounty Bounty
	var project Project
	err := s.pool.QueryRow(ctx, `
		SELECT b.id, b.project_id, b.github_issue_number, b.title, b.description, b.difficulty, b.reward_sats, b.fee_sats, b.state, b.expires_at, b.created_at,
		       p.id, p.owner_user_id, p.github_installation_id, p.repo_full_name, p.status, p.created_at
		FROM bounties b
		JOIN projects p ON b.project_id = p.id
		WHERE b.id = $1
	`, bountyID).Scan(
		&bounty.ID, &bounty.ProjectID, &bounty.GitHubIssueNumber, &bounty.Title, &bounty.Description,
		&bounty.Difficulty, &bounty.RewardSats, &bounty.FeeSats, &bounty.State, &bounty.ExpiresAt, &bounty.CreatedAt,
		&project.ID, &project.OwnerUserID, &project.GitHubInstallationID, &project.RepoFullName, &project.Status, &project.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrBountyNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	bounty.NetPayoutSats = bounty.RewardSats - bounty.FeeSats
	return &bounty, &project, nil
}

type Project struct {
	ID                   int64     `json:"id"`
	OwnerUserID          int64     `json:"owner_user_id"`
	GitHubInstallationID int64     `json:"github_installation_id"`
	RepoFullName         string    `json:"repo_full_name"`
	Status               string    `json:"status"`
	CreatedAt            time.Time `json:"created_at"`
}

func (s *Service) GetProjectByID(ctx context.Context, projectID int64) (*Project, error) {
	var project Project
	err := s.pool.QueryRow(ctx, `
		SELECT id, owner_user_id, github_installation_id, repo_full_name, status, created_at
		FROM projects WHERE id = $1
	`, projectID).Scan(&project.ID, &project.OwnerUserID, &project.GitHubInstallationID, &project.RepoFullName, &project.Status, &project.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("project not found")
	}
	if err != nil {
		return nil, err
	}
	return &project, nil
}

func (s *Service) GetWallet(ctx context.Context, projectID int64) (*Wallet, error) {
	var wallet Wallet
	err := s.pool.QueryRow(ctx, `
		SELECT available_sats, reserved_sats FROM wallets WHERE project_id = $1
	`, projectID).Scan(&wallet.AvailableSats, &wallet.ReservedSats)
	if errors.Is(err, sql.ErrNoRows) {
		return &Wallet{AvailableSats: 0, ReservedSats: 0}, nil
	}
	if err != nil {
		return nil, err
	}
	return &wallet, nil
}

type Wallet struct {
	AvailableSats int64 `json:"available_sats"`
	ReservedSats  int64 `json:"reserved_sats"`
}

func (s *Service) GetDepositByIdempotencyKey(ctx context.Context, projectID int64, idempotencyKey string) (*Deposit, error) {
	deposit, err := scanDeposit(s.pool.QueryRow(ctx, `
		SELECT id, project_id, bolt11, payment_hash, amount_sats, status, expires_at, settled_at, created_at, received_sats, settled_late
		FROM deposits WHERE project_id = $1 AND idempotency_key = $2
	`, projectID, idempotencyKey))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &deposit, nil
}

func (s *Service) CreateDeposit(ctx context.Context, projectID int64, idempotencyKey, bolt11 string, paymentHash []byte, amountSats int64, expiresAt time.Time) (*Deposit, error) {
	var deposit Deposit
	err := s.pool.QueryRow(ctx, `
		INSERT INTO deposits (project_id, bolt11, payment_hash, amount_sats, status, idempotency_key, expires_at)
		VALUES ($1, $2, $3, $4, 'pending', $5, $6)
		RETURNING id, project_id, bolt11, payment_hash, amount_sats, status, expires_at, created_at
	`, projectID, bolt11, paymentHash, amountSats, idempotencyKey, expiresAt).Scan(
		&deposit.ID, &deposit.ProjectID, &deposit.Bolt11, &deposit.PaymentHash, &deposit.AmountSats, &deposit.Status, &deposit.ExpiresAt, &deposit.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &deposit, nil
}

// GetOrCreateDeposit serializes invoice creation for a project idempotency key.
// The callback runs while holding a PostgreSQL transaction-scoped advisory lock.
func (s *Service) GetOrCreateDeposit(ctx context.Context, projectID int64, idempotencyKey string, amountSats int64, createInvoice func(context.Context) (string, []byte, time.Time, error)) (*Deposit, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, strconv.FormatInt(projectID, 10)+":"+idempotencyKey); err != nil {
		return nil, false, err
	}
	deposit, err := scanDeposit(tx.QueryRow(ctx, `
		SELECT id, project_id, bolt11, payment_hash, amount_sats, status, expires_at, settled_at, created_at, received_sats, settled_late
		FROM deposits WHERE project_id = $1 AND idempotency_key = $2
	`, projectID, idempotencyKey))
	if err == nil {
		if deposit.AmountSats != amountSats {
			return nil, false, ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, false, err
		}
		return &deposit, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	if err := requireFundingEnabled(ctx, tx); err != nil {
		return nil, false, err
	}
	bolt11, paymentHash, expiresAt, err := createInvoice(ctx)
	if err != nil {
		return nil, false, err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO deposits (project_id, bolt11, payment_hash, amount_sats, status, idempotency_key, expires_at)
		VALUES ($1, $2, $3, $4, 'pending', $5, $6)
		RETURNING id, project_id, bolt11, payment_hash, amount_sats, status, expires_at, settled_at, created_at
	`, projectID, bolt11, paymentHash, amountSats, idempotencyKey, expiresAt).Scan(
		&deposit.ID, &deposit.ProjectID, &deposit.Bolt11, &deposit.PaymentHash, &deposit.AmountSats, &deposit.Status, &deposit.ExpiresAt, &deposit.SettledAt, &deposit.CreatedAt,
	)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return &deposit, true, nil
}

func requireFundingEnabled(ctx context.Context, tx interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) error {
	var paused bool
	if err := tx.QueryRow(ctx, `SELECT enabled FROM system_flags WHERE name = 'funding_paused' FOR SHARE`).Scan(&paused); err != nil {
		return err
	}
	if paused {
		return ErrFundingPaused
	}
	return nil
}

// requireProjectActive blocks money movement on frozen or closed projects.
func requireProjectActive(ctx context.Context, tx pgx.Tx, projectID int64) error {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM projects WHERE id = $1`, projectID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrProjectNotFound
	}
	if err != nil {
		return err
	}
	if status != "active" {
		return ErrProjectFrozen
	}
	return nil
}

func scanDeposit(row interface{ Scan(...any) error }) (Deposit, error) {
	var deposit Deposit
	err := row.Scan(&deposit.ID, &deposit.ProjectID, &deposit.Bolt11, &deposit.PaymentHash, &deposit.AmountSats, &deposit.Status, &deposit.ExpiresAt, &deposit.SettledAt, &deposit.CreatedAt, &deposit.ReceivedSats, &deposit.SettledLate)
	return deposit, err
}

// SettleDeposit credits a wallet and its ledger atomically. It is safe to call
// repeatedly when LND replays an already processed invoice update.
func (s *Service) SettleDeposit(ctx context.Context, paymentHash []byte, amountPaidSats int64) error {
	if amountPaidSats <= 0 {
		return errors.New("settled invoice amount must be positive")
	}
	if s.ledger == nil {
		return errors.New("ledger service is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var deposit Deposit
	err = tx.QueryRow(ctx, `SELECT id, project_id, amount_sats, status, expires_at FROM deposits WHERE payment_hash = $1 FOR UPDATE`, paymentHash).Scan(&deposit.ID, &deposit.ProjectID, &deposit.AmountSats, &deposit.Status, &deposit.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDepositNotFound
	}
	if err != nil {
		return err
	}
	if deposit.Status == "settled" {
		return tx.Commit(ctx)
	}
	if deposit.Status != "pending" && deposit.Status != "expired" {
		return errors.New("deposit is not pending")
	}
	late := deposit.Status == "expired" || time.Now().After(deposit.ExpiresAt)
	if _, err := tx.Exec(ctx, `
		UPDATE deposits
		SET status = 'settled', received_sats = $2,
		    settled_late = (status = 'expired' OR expires_at < now()),
		    settled_at = now()
		WHERE id = $1
	`, deposit.ID, amountPaidSats); err != nil {
		return err
	}
	walletUpdate, err := tx.Exec(ctx, `UPDATE wallets SET available_sats = available_sats + $1, updated_at = now() WHERE project_id = $2`, amountPaidSats, deposit.ProjectID)
	if err != nil {
		return err
	}
	if walletUpdate.RowsAffected() != 1 {
		return errors.New("project wallet is missing for settled deposit")
	}
	if err := s.ledger.CreditDeposit(ctx, tx, deposit.ProjectID, deposit.ID, amountPaidSats); err != nil {
		return err
	}
	if late {
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_log (actor, action, subject, metadata)
			VALUES ('system', 'deposit.settled_late', $1, jsonb_build_object('deposit_id', $2::bigint, 'amount_sats', $3::bigint))
		`, strconv.FormatInt(deposit.ProjectID, 10), deposit.ID, amountPaidSats); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if late {
		var lateCount int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM deposits WHERE settled_late AND settled_at >= now() - interval '24 hours'`).Scan(&lateCount); err == nil && lateCount > 5 {
			s.log.Error().Int("late_deposits_24h", lateCount).Msg("late Lightning deposits exceeded alert threshold")
		} else {
			s.log.Warn().Int64("deposit_id", deposit.ID).Msg("late Lightning deposit credited to project wallet")
		}
	}
	return nil
}

// RecordUnmatchedReceipt quarantines a settled payment with no matching
// deposit. Replayed invoice updates are idempotent on payment_hash.
func (s *Service) RecordUnmatchedReceipt(ctx context.Context, paymentHash []byte, amountPaidSats int64) error {
	if len(paymentHash) == 0 || amountPaidSats <= 0 {
		return errors.New("invalid unmatched Lightning receipt")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var receiptID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO unmatched_receipts (payment_hash, amount_sats)
		VALUES ($1, $2)
		ON CONFLICT (payment_hash) DO NOTHING
		RETURNING id
	`, paymentHash, amountPaidSats).Scan(&receiptID)
	if err == pgx.ErrNoRows {
		var existingAmount int64
		if err := tx.QueryRow(ctx, `SELECT amount_sats FROM unmatched_receipts WHERE payment_hash = $1`, paymentHash).Scan(&existingAmount); err != nil {
			return err
		}
		if existingAmount != amountPaidSats {
			return errors.New("replayed unmatched receipt amount changed")
		}
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_log (actor, action, subject, metadata)
		VALUES ('system', 'deposit.unmatched_receipt', $1, jsonb_build_object('receipt_id', $2::bigint, 'amount_sats', $3::bigint, 'payment_hash', encode($4::bytea, 'hex')))
	`, fmt.Sprintf("receipt:%d", receiptID), receiptID, amountPaidSats, paymentHash); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.log.Error().Int64("receipt_id", receiptID).Int64("amount_sats", amountPaidSats).Msg("unmatched Lightning receipt quarantined; staff review required")
	return nil
}

// MarkDepositExpired records expiry only after LND reports cancellation.
func (s *Service) MarkDepositExpired(ctx context.Context, paymentHash []byte) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE deposits
		SET status = 'expired', node_expired_at = now(), node_cancelled_at = now()
		WHERE payment_hash = $1 AND status = 'pending'
	`, paymentHash)
	return err
}

type Deposit struct {
	ID           int64      `json:"id"`
	ProjectID    int64      `json:"project_id"`
	Bolt11       string     `json:"bolt11"`
	PaymentHash  []byte     `json:"payment_hash"`
	AmountSats   int64      `json:"amount_sats"`
	ReceivedSats *int64     `json:"received_sats,omitempty"`
	Status       string     `json:"status"`
	SettledLate  bool       `json:"settled_late"`
	ExpiresAt    time.Time  `json:"expires_at"`
	SettledAt    *time.Time `json:"settled_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}
