package payout

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog"
	"github.com/satlight/satlight/internal/bounty"
	"github.com/satlight/satlight/internal/config"
	"github.com/satlight/satlight/internal/ledger"
	"github.com/satlight/satlight/internal/metrics"
	"github.com/satlight/satlight/internal/notify"
	"github.com/satlight/satlight/internal/queue"
)

// maxSendAttempts bounds Lightning send attempts before a payout falls back
// to a claim link. Delays between attempts follow payoutRetryBackoff.
const maxSendAttempts = 5

var payoutRetryBackoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

// destinationCooldown is how long a new or changed payout destination must
// wait before automatic payouts resume.
const destinationCooldown = 48 * time.Hour

var (
	ErrOutflowsPaused        = errors.New("payouts are paused")
	ErrInvalidDestination    = errors.New("invalid payout destination")
	ErrNoPayoutClaimant      = errors.New("merged bounty has no active claim")
	ErrClaimLinkUsed         = errors.New("claim link already used")
	ErrInvoiceAmountMismatch = errors.New("invoice amount mismatch")
)

// LightningSender abstracts the node calls payouts need (mocked in tests).
type LightningSender interface {
	PayInvoice(ctx context.Context, invoice string, maxFeeSats int64) ([]byte, error)
	GetPaymentResult(ctx context.Context, paymentHash []byte) (string, int64, error)
	DecodePayReq(ctx context.Context, payReq string) (*lnrpc.PayReq, error)
}

// MergeVerifier re-checks a merge just before paying.
type MergeVerifier interface {
	VerifyMergeFresh(ctx context.Context, bountyID int64) (bool, error)
}

type Service struct {
	cfg       *config.Config
	pool      *pgxpool.Pool
	log       zerolog.Logger
	ledger    *ledger.Service
	lightning LightningSender
	github    MergeVerifier
	notify    *notify.Service
	queue     *queue.Client
	http      *http.Client
}

func NewService(cfg *config.Config, pool *pgxpool.Pool) *Service {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: gin.DefaultWriter, TimeFormat: zerolog.TimeFormatUnix}).With().Timestamp().Logger()
	return &Service{cfg: cfg, pool: pool, log: logger, http: &http.Client{Timeout: 10 * time.Second}}
}

func (s *Service) SetDependencies(ledger *ledger.Service, lightning LightningSender, github MergeVerifier, notify *notify.Service, queue *queue.Client) {
	s.ledger = ledger
	s.lightning = lightning
	s.github = github
	s.notify = notify
	s.queue = queue
}

type payoutContext struct {
	bountyID    int64
	projectID   int64
	ownerID     int64
	rewardSats  int64
	feeSats     int64
	userID      int64
	destination string
	changedAt   *time.Time
	selfMerged  bool
}

type storedPayout struct {
	id          int64
	status      string
	paymentHash []byte
	attempts    int
}

func (s *Service) bountyState(ctx context.Context, bountyID int64) (string, error) {
	var state string
	err := s.pool.QueryRow(ctx, `SELECT state FROM bounties WHERE id = $1`, bountyID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return state, err
}

func (s *Service) loadPayout(ctx context.Context, bountyID int64) (*storedPayout, error) {
	var p storedPayout
	err := s.pool.QueryRow(ctx, `SELECT id, status, payment_hash, attempts FROM payouts WHERE bounty_id = $1`, bountyID).Scan(
		&p.id, &p.status, &p.paymentHash, &p.attempts,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Service) loadPayoutContext(ctx context.Context, bountyID int64) (*payoutContext, error) {
	var pctx payoutContext
	err := s.pool.QueryRow(ctx, `
		SELECT b.project_id, p.owner_user_id, b.reward_sats, b.fee_sats, c.user_id,
		       COALESCE(u.lightning_address, ''), u.payout_changed_at,
		       EXISTS(SELECT 1 FROM pull_requests pr WHERE pr.claim_id = c.id AND pr.merged
		              AND pr.merged_by_github_id IS NOT NULL AND pr.merged_by_github_id = pr.author_github_id)
		FROM bounties b
		JOIN projects p ON b.project_id = p.id
		JOIN claims c ON c.bounty_id = b.id AND c.status = 'active'
		JOIN users u ON c.user_id = u.id
		WHERE b.id = $1
	`, bountyID).Scan(&pctx.projectID, &pctx.ownerID, &pctx.rewardSats, &pctx.feeSats,
		&pctx.userID, &pctx.destination, &pctx.changedAt, &pctx.selfMerged)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoPayoutClaimant
	}
	if err != nil {
		return nil, err
	}
	pctx.bountyID = bountyID
	return &pctx, nil
}

func (s *Service) outflowsPaused(ctx context.Context) (bool, error) {
	var paused bool
	if err := s.pool.QueryRow(ctx, `SELECT enabled FROM system_flags WHERE name = 'outflows_paused'`).Scan(&paused); err != nil {
		return true, err
	}
	return paused, nil
}

func (s *Service) maxRoutingFeeSats(amountSats int64) int64 {
	fee := amountSats * int64(s.cfg.PayoutMaxFeePPM) / 1000000
	if floor := int64(s.cfg.PayoutMaxFeeFloorSats); fee < floor {
		return floor
	}
	return fee
}

func parseLightningAddress(address string) (user, domain string, err error) {
	address = strings.TrimSpace(address)
	parts := strings.Split(address, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(address, " \t\n/") {
		return "", "", fmt.Errorf("%w: %q", ErrInvalidDestination, address)
	}
	return parts[0], parts[1], nil
}

// invoiceForDestination resolves a Lightning address via LNURL-pay and
// returns a BOLT11 invoice for the exact amount with its payment hash.
func (s *Service) invoiceForDestination(ctx context.Context, address string, amountSats int64) (string, []byte, error) {
	user, domain, err := parseLightningAddress(address)
	if err != nil {
		return "", nil, err
	}
	var params struct {
		Callback    string `json:"callback"`
		MinSendable int64  `json:"minSendable"`
		MaxSendable int64  `json:"maxSendable"`
		Tag         string `json:"tag"`
	}
	if err := s.getJSON(ctx, "https://"+domain+"/.well-known/lnurlp/"+user, &params); err != nil {
		return "", nil, err
	}
	if params.Tag != "payRequest" || params.Callback == "" {
		return "", nil, fmt.Errorf("unsupported LNURL response for %q", address)
	}
	msats := amountSats * 1000
	if msats < params.MinSendable || msats > params.MaxSendable {
		return "", nil, fmt.Errorf("amount %d sats outside recipient range", amountSats)
	}
	callbackURL, err := url.Parse(params.Callback)
	if err != nil {
		return "", nil, err
	}
	query := callbackURL.Query()
	query.Set("amount", strconv.FormatInt(msats, 10))
	callbackURL.RawQuery = query.Encode()
	var invoiceResp struct {
		PR string `json:"pr"`
	}
	if err := s.getJSON(ctx, callbackURL.String(), &invoiceResp); err != nil {
		return "", nil, err
	}
	if invoiceResp.PR == "" {
		return "", nil, errors.New("LNURL callback returned no invoice")
	}
	payReq, err := s.lightning.DecodePayReq(ctx, invoiceResp.PR)
	if err != nil {
		return "", nil, err
	}
	if payReq.NumSatoshis != amountSats {
		return "", nil, fmt.Errorf("invoice amount %d sats, want %d", payReq.NumSatoshis, amountSats)
	}
	hash, err := hex.DecodeString(payReq.PaymentHash)
	if err != nil || len(hash) == 0 {
		return "", nil, errors.New("invoice has no usable payment hash")
	}
	return invoiceResp.PR, hash, nil
}

func (s *Service) getJSON(ctx context.Context, rawURL string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("LNURL request failed: %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

func newClaimToken() (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(token), nil
}

// ProcessBountyPayout pays one merged bounty. It is idempotent and
// crash-safe: every send is preceded by a persisted attempt row, and every
// retry first checks the stored payment hash before sending again.
func (s *Service) ProcessBountyPayout(ctx context.Context, bountyID int64) error {
	start := time.Now()
	defer func() { metrics.PayoutDuration.Observe(time.Since(start).Seconds()) }()
	if s.ledger == nil || s.lightning == nil || s.github == nil || s.notify == nil || s.queue == nil {
		return errors.New("payout dependencies are not configured")
	}
	state, err := s.bountyState(ctx, bountyID)
	if err != nil {
		return err
	}
	if state != "merged" {
		return nil
	}
	existing, err := s.loadPayout(ctx, bountyID)
	if err != nil {
		return err
	}
	if existing != nil {
		switch existing.status {
		case "settled", "claimed_via_link", "claim_link_issued":
			return nil
		}
		if len(existing.paymentHash) > 0 {
			status, fee, err := s.lightning.GetPaymentResult(ctx, existing.paymentHash)
			if err == nil {
				switch status {
				case "SUCCEEDED":
					return s.settlePayout(ctx, existing.id, existing.paymentHash, fee, "settled")
				case "IN_FLIGHT":
					return errors.New("previous payment still in flight; will retry")
				}
			}
		}
	}
	if paused, err := s.outflowsPaused(ctx); err != nil {
		return err
	} else if paused {
		return ErrOutflowsPaused
	}
	pctx, err := s.loadPayoutContext(ctx, bountyID)
	if err != nil {
		if errors.Is(err, ErrNoPayoutClaimant) {
			s.log.Error().Int64("bounty_id", bountyID).Msg("merged bounty has no active claim; payout held for manual review")
			return nil
		}
		return err
	}
	if pctx.destination == "" {
		s.log.Error().Int64("bounty_id", bountyID).Msg("claimant has no payout destination; payout held for manual review")
		return nil
	}
	if err := s.ledger.CheckProjectPayoutCap(ctx, pctx.projectID, pctx.rewardSats-pctx.feeSats, s.cfg.DailyPayoutCapSats); err != nil {
		return err
	}
	fresh, err := s.github.VerifyMergeFresh(ctx, bountyID)
	if err != nil {
		return err
	}
	if !fresh {
		return s.holdForRevertedMerge(ctx, pctx)
	}
	if pctx.selfMerged {
		return s.holdWithClaimLink(ctx, pctx, "self-merged pull request")
	}
	if pctx.changedAt != nil && time.Since(*pctx.changedAt) < destinationCooldown {
		return s.delayForCooldown(ctx, pctx)
	}
	netAmount := pctx.rewardSats - pctx.feeSats
	invoice, invHash, err := s.invoiceForDestination(ctx, pctx.destination, netAmount)
	if err != nil {
		return s.recordSendFailure(ctx, pctx, "invoice resolution failed: "+err.Error())
	}
	payoutID, err := s.persistSendingAttempt(ctx, pctx, invHash)
	if err != nil {
		if errors.Is(err, errBountyLeftMerged) {
			return nil
		}
		return err
	}
	sentHash, err := s.lightning.PayInvoice(ctx, invoice, s.maxRoutingFeeSats(netAmount))
	if err != nil {
		return s.recordSendFailure(ctx, pctx, err.Error())
	}
	_, fee, trackErr := s.lightning.GetPaymentResult(ctx, sentHash)
	if trackErr != nil {
		fee = 0
	}
	return s.settlePayout(ctx, payoutID, sentHash, fee, "settled")
}

var errBountyLeftMerged = errors.New("bounty is no longer merged")

// persistSendingAttempt stores the payout row and attempt (with payment hash)
// before any Lightning call, so crashes always recover by hash.
func (s *Service) persistSendingAttempt(ctx context.Context, pctx *payoutContext, paymentHash []byte) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM bounties WHERE id = $1 FOR UPDATE`, pctx.bountyID).Scan(&state); err != nil {
		return 0, err
	}
	if state != "merged" {
		return 0, errBountyLeftMerged
	}
	var paused bool
	if err := tx.QueryRow(ctx, `SELECT enabled FROM system_flags WHERE name = 'outflows_paused' FOR SHARE`).Scan(&paused); err != nil {
		return 0, err
	}
	if paused {
		return 0, ErrOutflowsPaused
	}
	var payoutID int64
	err = tx.QueryRow(ctx, `SELECT id FROM payouts WHERE bounty_id = $1 FOR UPDATE`, pctx.bountyID).Scan(&payoutID)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			INSERT INTO payouts (bounty_id, user_id, amount_sats, fee_sats, destination, status, payment_hash)
			VALUES ($1, $2, $3, $4, $5, 'sending', $6) RETURNING id
		`, pctx.bountyID, pctx.userID, pctx.rewardSats-pctx.feeSats, pctx.feeSats, pctx.destination, paymentHash).Scan(&payoutID)
		if err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	} else if _, err := tx.Exec(ctx, `UPDATE payouts SET status = 'sending', payment_hash = $1 WHERE id = $2`, paymentHash, payoutID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO payout_attempts (payout_id, payment_hash, status) VALUES ($1, $2, 'sending')`, payoutID, paymentHash); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return payoutID, nil
}

// recordSendFailure counts a failed attempt, then either schedules the next
// retry with backoff or falls back to a claim link after the final attempt.
func (s *Service) recordSendFailure(ctx context.Context, pctx *payoutContext, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var payoutID, attempts int64
	err = tx.QueryRow(ctx, `SELECT id, attempts FROM payouts WHERE bounty_id = $1 FOR UPDATE`, pctx.bountyID).Scan(&payoutID, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			INSERT INTO payouts (bounty_id, user_id, amount_sats, fee_sats, destination, status, attempts, last_error)
			VALUES ($1, $2, $3, $4, $5, 'failed', 1, $6) RETURNING id
		`, pctx.bountyID, pctx.userID, pctx.rewardSats-pctx.feeSats, pctx.feeSats, pctx.destination, reason).Scan(&payoutID)
		if err != nil {
			return err
		}
		attempts = 1
	} else if err != nil {
		return err
	} else {
		attempts++
		if _, err := tx.Exec(ctx, `UPDATE payouts SET status = 'failed', attempts = $1, last_error = $2 WHERE id = $3`, attempts, reason, payoutID); err != nil {
			return err
		}
	}
	if attempts >= maxSendAttempts {
		token, err := newClaimToken()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE payouts SET status = 'claim_link_issued', claim_link_token = $1 WHERE id = $2`, token, payoutID); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		metrics.PayoutsCompleted.WithLabelValues("claim_link_issued").Inc()
		s.log.Error().Int64("bounty_id", pctx.bountyID).Msg("payout failed permanently; claim link issued")
		if err := s.notify.NotifyClaimLink(ctx, pctx.userID, payoutID, token, pctx.rewardSats-pctx.feeSats); err != nil {
			s.log.Error().Err(err).Msg("failed to send claim-link notification")
		}
		return nil
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	delay := payoutRetryBackoff[0]
	if int(attempts-1) < len(payoutRetryBackoff) {
		delay = payoutRetryBackoff[attempts-1]
	}
	s.log.Warn().Int64("bounty_id", pctx.bountyID).Int64("attempt", attempts).Dur("retry_in", delay).Msg("payout attempt failed; retry scheduled")
	return s.queue.Insert(ctx, queue.ProcessPayoutArgs{BountyID: pctx.bountyID}, river.InsertOpts{
		Queue:       "payouts",
		ScheduledAt: time.Now().Add(delay),
	})
}

// settlePayout records a completed payment: payout row, ledger entries,
// bounty and claim state, all atomically. It is idempotent on the payout row.
func (s *Service) settlePayout(ctx context.Context, payoutID int64, paymentHash []byte, routingFeeSats int64, finalStatus string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var payout struct {
		bountyID int64
		userID   int64
		status   string
	}
	var projectID, rewardSats, feeSats int64
	var bountyState string
	err = tx.QueryRow(ctx, `
		SELECT p.bounty_id, p.user_id, p.status, b.project_id, b.reward_sats, b.fee_sats, b.state
		FROM payouts p JOIN bounties b ON p.bounty_id = b.id
		WHERE p.id = $1 FOR UPDATE OF p, b
	`, payoutID).Scan(&payout.bountyID, &payout.userID, &payout.status, &projectID, &rewardSats, &feeSats, &bountyState)
	if err != nil {
		return err
	}
	if payout.status == "settled" || payout.status == "claimed_via_link" {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE payouts SET status = $1, settled_at = now(), routing_fee_sats = $2, payment_hash = $3 WHERE id = $4`,
		finalStatus, routingFeeSats, paymentHash, payoutID); err != nil {
		return err
	}
	if err := s.ledger.ProcessPayout(ctx, tx, projectID, payout.bountyID, payout.userID, rewardSats, feeSats, routingFeeSats); err != nil {
		return err
	}
	if bountyState == "merged" {
		if err := bounty.TransitionBounty(ctx, tx, bounty.Transition{
			BountyID: payout.bountyID, From: "merged", To: "paid",
			Actor: "system", Action: "bounty.paid",
			Metadata: map[string]any{"payout_id": payoutID},
		}); err != nil {
			return err
		}
	} else {
		s.log.Error().Int64("bounty_id", payout.bountyID).Str("state", bountyState).Msg("payment settled while bounty not merged; funds recorded, state left alone")
	}
	if _, err := tx.Exec(ctx, `UPDATE claims SET status = 'completed' WHERE bounty_id = $1 AND status = 'active'`, payout.bountyID); err != nil {
		return err
	}
	if err := s.notify.EnqueueProjectEvent(ctx, tx, s.queue, projectID, "payout.settled",
		map[string]any{"bounty_id": payout.bountyID, "payout_id": payoutID}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	metrics.PayoutsCompleted.WithLabelValues(finalStatus).Inc()
	if err := s.notify.NotifyPayoutSettled(ctx, payout.userID, payout.bountyID, rewardSats-feeSats); err != nil {
		s.log.Error().Err(err).Msg("failed to send payout-settled notification")
	}
	return nil
}

func (s *Service) holdForRevertedMerge(ctx context.Context, pctx *payoutContext) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_log (actor, action, subject, metadata)
		VALUES ('system', 'payout.held', $1, '{"reason":"merge could not be re-verified"}')
	`, "bounty:"+strconv.FormatInt(pctx.bountyID, 10)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	metrics.PayoutsCompleted.WithLabelValues("held").Inc()
	s.log.Error().Int64("bounty_id", pctx.bountyID).Msg("merge re-verification failed; payout held for manual review")
	if err := s.notify.NotifyMergeReverted(ctx, pctx.ownerID, pctx.bountyID); err != nil {
		s.log.Error().Err(err).Msg("failed to send merge-reverted notification")
	}
	return nil
}

func (s *Service) holdWithClaimLink(ctx context.Context, pctx *payoutContext, reason string) error {
	token, err := newClaimToken()
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var payoutID int64
	err = tx.QueryRow(ctx, `SELECT id FROM payouts WHERE bounty_id = $1 FOR UPDATE`, pctx.bountyID).Scan(&payoutID)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			INSERT INTO payouts (bounty_id, user_id, amount_sats, fee_sats, destination, status, claim_link_token, last_error)
			VALUES ($1, $2, $3, $4, $5, 'claim_link_issued', $6, $7) RETURNING id
		`, pctx.bountyID, pctx.userID, pctx.rewardSats-pctx.feeSats, pctx.feeSats, pctx.destination, token, "held: "+reason).Scan(&payoutID)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if _, err := tx.Exec(ctx, `UPDATE payouts SET status = 'claim_link_issued', claim_link_token = $1, last_error = $2 WHERE id = $3`,
		token, "held: "+reason, payoutID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_log (actor, action, subject, metadata)
		VALUES ('system', 'payout.held', $1, $2::jsonb)
	`, "bounty:"+strconv.FormatInt(pctx.bountyID, 10), fmt.Sprintf(`{"reason":%q}`, reason)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	metrics.PayoutsCompleted.WithLabelValues("claim_link_issued").Inc()
	s.log.Warn().Int64("bounty_id", pctx.bountyID).Str("reason", reason).Msg("automatic payout held; claim link issued")
	if err := s.notify.NotifyClaimLink(ctx, pctx.userID, payoutID, token, pctx.rewardSats-pctx.feeSats); err != nil {
		s.log.Error().Err(err).Msg("failed to send claim-link notification")
	}
	return nil
}

func (s *Service) delayForCooldown(ctx context.Context, pctx *payoutContext) error {
	resumeAt := pctx.changedAt.Add(destinationCooldown)
	s.log.Info().Int64("bounty_id", pctx.bountyID).Time("resume_at", resumeAt).Msg("payout destination in cooldown; payout delayed")
	return s.queue.Insert(ctx, queue.ProcessPayoutArgs{BountyID: pctx.bountyID}, river.InsertOpts{
		Queue:       "payouts",
		ScheduledAt: resumeAt,
	})
}

// WithdrawRequest is the LNURL-withdraw (LUD-03) response for a claim link.
type WithdrawRequest struct {
	CallbackURL        string
	Token              string
	MinWithdrawableMs  int64
	MaxWithdrawableMs  int64
	DefaultDescription string
}

// GetWithdrawRequest loads the claim-link payout behind a token.
func (s *Service) GetWithdrawRequest(ctx context.Context, token string) (*WithdrawRequest, error) {
	var amountSats int64
	err := s.pool.QueryRow(ctx, `SELECT amount_sats FROM payouts WHERE claim_link_token = $1 AND status = 'claim_link_issued'`, token).Scan(&amountSats)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	return &WithdrawRequest{
		CallbackURL:        strings.TrimRight(s.cfg.BaseURL, "/") + "/api/v1/payouts/claim/callback",
		Token:              token,
		MinWithdrawableMs:  amountSats * 1000,
		MaxWithdrawableMs:  amountSats * 1000,
		DefaultDescription: "Satlight bounty payout",
	}, nil
}

// RedeemClaimLink pays the exact-amount invoice a wallet submits for a claim
// link, then settles the payout as claimed_via_link. The link is single-use:
// concurrent redemptions serialize on the payout row.
func (s *Service) RedeemClaimLink(ctx context.Context, token, bolt11 string) error {
	payReq, err := s.lightning.DecodePayReq(ctx, bolt11)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var payoutID, amountSats int64
	var status string
	err = tx.QueryRow(ctx, `SELECT id, amount_sats, status FROM payouts WHERE claim_link_token = $1 FOR UPDATE`, token).Scan(&payoutID, &amountSats, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	if status != "claim_link_issued" {
		return ErrClaimLinkUsed
	}
	if payReq.NumSatoshis != amountSats {
		return fmt.Errorf("%w: invoice has %d sats, want %d", ErrInvoiceAmountMismatch, payReq.NumSatoshis, amountSats)
	}
	hash, err := hex.DecodeString(payReq.PaymentHash)
	if err != nil || len(hash) == 0 {
		return errors.New("invoice has no usable payment hash")
	}
	if _, err := tx.Exec(ctx, `UPDATE payouts SET status = 'sending', payment_hash = $1 WHERE id = $2`, hash, payoutID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO payout_attempts (payout_id, payment_hash, status) VALUES ($1, $2, 'sending')`, payoutID, hash); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	sentHash, err := s.lightning.PayInvoice(ctx, bolt11, s.maxRoutingFeeSats(amountSats))
	if err != nil {
		_, _ = s.pool.Exec(ctx, `UPDATE payouts SET status = 'claim_link_issued', last_error = $1 WHERE id = $2`, err.Error(), payoutID)
		return err
	}
	_, fee, trackErr := s.lightning.GetPaymentResult(ctx, sentHash)
	if trackErr != nil {
		fee = 0
	}
	return s.settlePayout(ctx, payoutID, sentHash, fee, "claimed_via_link")
}
