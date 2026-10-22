package bounty

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog"
	"github.com/satlight/satlight/internal/config"
	"github.com/satlight/satlight/internal/ledger"
	"github.com/satlight/satlight/internal/queue"
)

var (
	ErrBountyNotFound       = errors.New("bounty not found")
	ErrBountyNotOpen        = errors.New("bounty is not open for claims")
	ErrBountyAlreadyClaimed = errors.New("bounty already claimed")
	ErrInsufficientFunds    = errors.New("insufficient wallet balance")
	ErrInvalidState         = errors.New("invalid bounty state for this operation")
	ErrNotAuthorized        = errors.New("not authorized")
	ErrDepositNotFound      = errors.New("deposit not found")
	ErrDepositUnderpaid     = errors.New("settled amount is below the invoice amount")
)

type Service struct {
	cfg    *config.Config
	pool   *pgxpool.Pool
	log    zerolog.Logger
	ledger *ledger.Service
	queue  *queue.Client
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
}

func (s *Service) CreateBounty(ctx context.Context, params CreateBountyParams) (*Bounty, error) {
	if params.RewardSats < s.cfg.MinRewardSats {
		return nil, errors.New("reward below minimum")
	}

	expiresAt := params.ExpiresAt
	if expiresAt == nil {
		exp := time.Now().Add(s.cfg.BountyDefaultExpiryDuration())
		expiresAt = &exp
	}

	feeSats := (params.RewardSats * int64(s.cfg.PlatformFeeBPS)) / 10000
	netPayoutSats := params.RewardSats - feeSats

	var bounty Bounty
	err := s.pool.QueryRow(ctx, `
		INSERT INTO bounties (project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'draft', $8)
		RETURNING id, project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at, created_at
	`, params.ProjectID, params.GitHubIssueNumber, params.Title, params.Description, params.Difficulty, params.RewardSats, feeSats, expiresAt).Scan(
		&bounty.ID, &bounty.ProjectID, &bounty.GitHubIssueNumber, &bounty.Title, &bounty.Description,
		&bounty.Difficulty, &bounty.RewardSats, &bounty.FeeSats, &bounty.State, &bounty.ExpiresAt, &bounty.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	bounty.NetPayoutSats = netPayoutSats
	return &bounty, nil
}

func (s *Service) FundBounty(ctx context.Context, bountyID, projectID int64) (*Bounty, error) {
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
	if err == sql.ErrNoRows {
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

	_, err = tx.Exec(ctx, `UPDATE bounties SET state = 'open', updated_at = now() WHERE id = $1`, bountyID)
	if err != nil {
		return nil, err
	}

	bounty.State = "open"

	if s.ledger != nil {
		if err := s.ledger.ReserveBountyFunds(ctx, tx, bounty.ProjectID, bounty.ID, bounty.RewardSats, bounty.FeeSats); err != nil {
			return nil, err
		}
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
	if err == sql.ErrNoRows {
		return nil, ErrBountyNotFound
	}
	if err != nil {
		return nil, err
	}
	bounty.NetPayoutSats = bounty.RewardSats - bounty.FeeSats
	return &bounty, nil
}

func (s *Service) ListBounties(ctx context.Context, filters ListFilters) ([]*Bounty, string, error) {
	query := `
		SELECT b.id, b.project_id, b.github_issue_number, b.title, b.description, b.difficulty, b.reward_sats, b.fee_sats, b.state, b.expires_at, b.created_at, p.repo_full_name
		FROM bounties b
		JOIN projects p ON b.project_id = p.id
		WHERE b.state = 'open' AND b.expires_at > now()
	`
	args := []interface{}{}
	argIdx := 1

	if filters.Query != "" {
		query += ` AND (b.title ILIKE $` + string(rune(argIdx+'0')) + ` OR b.description ILIKE $` + string(rune(argIdx+'0')) + `)`
		args = append(args, "%"+filters.Query+"%")
		argIdx++
	}
	if filters.Language != "" {
		query += ` AND p.repo_full_name ILIKE $` + string(rune(argIdx+'0'))
		args = append(args, "%"+filters.Language+"%")
		argIdx++
	}
	if filters.Difficulty != "" {
		query += ` AND b.difficulty = $` + string(rune(argIdx+'0'))
		args = append(args, filters.Difficulty)
		argIdx++
	}
	if filters.MinReward > 0 {
		query += ` AND b.reward_sats >= $` + string(rune(argIdx+'0'))
		args = append(args, filters.MinReward)
		argIdx++
	}
	if filters.MaxReward > 0 {
		query += ` AND b.reward_sats <= $` + string(rune(argIdx+'0'))
		args = append(args, filters.MaxReward)
		argIdx++
	}

	query += ` ORDER BY b.created_at DESC LIMIT $` + string(rune(argIdx+'0'))
	args = append(args, filters.Limit+1)
	argIdx++

	if filters.Cursor != "" {
		query += ` AND b.created_at < $` + string(rune(argIdx+'0'))
		args = append(args, filters.Cursor)
		argIdx++
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

	var nextCursor string
	if len(bounties) > filters.Limit {
		nextCursor = bounties[filters.Limit-1].CreatedAt.Format(time.RFC3339Nano)
		bounties = bounties[:filters.Limit]
	}

	return bounties, nextCursor, nil
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

func (s *Service) ClaimBounty(ctx context.Context, bountyID, userID int64) (*Claim, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
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
	if err == sql.ErrNoRows {
		return nil, ErrBountyNotFound
	}
	if err != nil {
		return nil, err
	}

	if bounty.State != "open" {
		return nil, ErrBountyNotOpen
	}

	var userLightningAddress string
	err = tx.QueryRow(ctx, `SELECT lightning_address FROM users WHERE id = $1`, userID).Scan(&userLightningAddress)
	if err == sql.ErrNoRows {
		return nil, errors.New("user not found")
	}
	if err != nil {
		return nil, err
	}
	if userLightningAddress == "" {
		return nil, errors.New("payout destination not set")
	}

	expiresAt := time.Now().Add(s.cfg.ClaimExpiryDuration())

	var claim Claim
	err = tx.QueryRow(ctx, `
		INSERT INTO claims (bounty_id, user_id, status, expires_at)
		VALUES ($1, $2, 'active', $3)
		RETURNING id, bounty_id, user_id, status, expires_at, created_at
	`, bountyID, userID, expiresAt).Scan(
		&claim.ID, &claim.BountyID, &claim.UserID, &claim.Status, &claim.ExpiresAt, &claim.CreatedAt,
	)
	if err != nil {
		if pgErr, ok := err.(interface{ Code() string }); ok && pgErr.Code() == "23505" {
			return nil, ErrBountyAlreadyClaimed
		}
		return nil, err
	}

	_, err = tx.Exec(ctx, `UPDATE bounties SET state = 'claimed', updated_at = now() WHERE id = $1`, bountyID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	if s.queue != nil {
		s.queue.Insert(ctx, &queue.ClaimExpiryArgs{ClaimID: claim.ID}, river.InsertOpts{ScheduledAt: expiresAt})
	}

	return &claim, nil
}

func (s *Service) ReleaseClaim(ctx context.Context, bountyID, userID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `
		UPDATE claims SET status = 'released' WHERE bounty_id = $1 AND user_id = $2 AND status = 'active'
	`, bountyID, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("no active claim found")
	}

	_, err = tx.Exec(ctx, `UPDATE bounties SET state = 'open', updated_at = now() WHERE id = $1`, bountyID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Service) CancelBounty(ctx context.Context, bountyID, projectID int64) (*Bounty, error) {
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
	if err == sql.ErrNoRows {
		return nil, ErrBountyNotFound
	}
	if err != nil {
		return nil, err
	}

	if bounty.State != "open" && bounty.State != "claimed" {
		return nil, ErrInvalidState
	}

	_, err = tx.Exec(ctx, `UPDATE wallets SET available_sats = available_sats + $1, reserved_sats = reserved_sats - $1, updated_at = now() WHERE project_id = $2`, bounty.RewardSats, projectID)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx, `UPDATE bounties SET state = 'cancelled', updated_at = now() WHERE id = $1`, bountyID)
	if err != nil {
		return nil, err
	}

	if s.ledger != nil {
		if err := s.ledger.RefundBountyFunds(ctx, tx, bounty.ProjectID, bounty.ID, bounty.RewardSats); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	bounty.State = "cancelled"
	return &bounty, nil
}

func (s *Service) DisputeBounty(ctx context.Context, bountyID, userID int64, reason, evidence string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO disputes (bounty_id, opened_by, reason, evidence)
		VALUES ($1, $2, $3, $4)
	`, bountyID, userID, reason, evidence)
	return err
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
	if err == sql.ErrNoRows {
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
	if err == sql.ErrNoRows {
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
	if err == sql.ErrNoRows {
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
		SELECT id, project_id, bolt11, payment_hash, amount_sats, status, expires_at, settled_at, created_at
		FROM deposits WHERE project_id = $1 AND idempotency_key = $2
	`, projectID, idempotencyKey))
	if err == sql.ErrNoRows {
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

func scanDeposit(row interface{ Scan(...any) error }) (Deposit, error) {
	var deposit Deposit
	err := row.Scan(&deposit.ID, &deposit.ProjectID, &deposit.Bolt11, &deposit.PaymentHash, &deposit.AmountSats, &deposit.Status, &deposit.ExpiresAt, &deposit.SettledAt, &deposit.CreatedAt)
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
	err = tx.QueryRow(ctx, `SELECT id, project_id, amount_sats, status FROM deposits WHERE payment_hash = $1 FOR UPDATE`, paymentHash).Scan(&deposit.ID, &deposit.ProjectID, &deposit.AmountSats, &deposit.Status)
	if err == sql.ErrNoRows {
		return ErrDepositNotFound
	}
	if err != nil {
		return err
	}
	if deposit.Status == "settled" {
		return tx.Commit(ctx)
	}
	if deposit.Status != "pending" {
		return errors.New("deposit is not pending")
	}
	if amountPaidSats < deposit.AmountSats {
		return ErrDepositUnderpaid
	}
	if _, err := tx.Exec(ctx, `UPDATE deposits SET status = 'settled', amount_sats = $2, settled_at = now() WHERE id = $1`, deposit.ID, amountPaidSats); err != nil {
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
	return tx.Commit(ctx)
}

type Deposit struct {
	ID          int64      `json:"id"`
	ProjectID   int64      `json:"project_id"`
	Bolt11      string     `json:"bolt11"`
	PaymentHash []byte     `json:"payment_hash"`
	AmountSats  int64      `json:"amount_sats"`
	Status      string     `json:"status"`
	ExpiresAt   time.Time  `json:"expires_at"`
	SettledAt   *time.Time `json:"settled_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}
