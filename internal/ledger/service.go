package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/satlight/satlight/internal/metrics"
)

var (
	ErrLedgerImbalance        = errors.New("ledger entries do not balance")
	ErrInvalidLedgerEntry     = errors.New("invalid ledger entry")
	ErrTooFewEntries          = errors.New("at least two entries required")
	ErrDailyPayoutCapExceeded = errors.New("project daily payout cap exceeded")
)

// reciprocalFlagThreshold is the number of merged bounties between one owner
// and one contributor that raises a collusion flag for manual review.
const reciprocalFlagThreshold = 2

type Service struct {
	pool        *pgxpool.Pool
	log         zerolog.Logger
	nodeBalance NodeBalanceProvider
}

type NodeBalanceProvider interface {
	GetTotalBalance(context.Context) (int64, error)
}

func NewService(cfg interface{}, pool *pgxpool.Pool) *Service {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: gin.DefaultWriter, TimeFormat: zerolog.TimeFormatUnix}).With().Timestamp().Logger()
	return &Service{
		pool: pool,
		log:  logger,
	}
}

func (s *Service) SetNodeBalanceProvider(provider NodeBalanceProvider) {
	s.nodeBalance = provider
}

type Entry struct {
	Account    string
	Direction  string
	AmountSats int64
	RefType    string
	RefID      int64
}

func (s *Service) RecordTransaction(ctx context.Context, tx pgx.Tx, entries []Entry) error {
	if err := validateEntries(entries); err != nil {
		return err
	}

	txID := uuid.New()
	for _, e := range entries {
		_, err := tx.Exec(ctx, `
			INSERT INTO ledger_entries (tx_id, account, direction, amount_sats, ref_type, ref_id)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, txID, e.Account, e.Direction, e.AmountSats, e.RefType, e.RefID)
		if err != nil {
			return err
		}
	}

	return nil
}

func validateEntries(entries []Entry) error {
	if len(entries) < 2 {
		return ErrTooFewEntries
	}

	var totalDebits, totalCredits int64
	for _, entry := range entries {
		if entry.AmountSats <= 0 {
			return ErrInvalidLedgerEntry
		}
		switch entry.Direction {
		case "debit":
			if totalDebits > math.MaxInt64-entry.AmountSats {
				return fmt.Errorf("%w: debit total overflow", ErrInvalidLedgerEntry)
			}
			totalDebits += entry.AmountSats
		case "credit":
			if totalCredits > math.MaxInt64-entry.AmountSats {
				return fmt.Errorf("%w: credit total overflow", ErrInvalidLedgerEntry)
			}
			totalCredits += entry.AmountSats
		default:
			return fmt.Errorf("%w: unknown direction %q", ErrInvalidLedgerEntry, entry.Direction)
		}
	}

	if totalDebits != totalCredits {
		return ErrLedgerImbalance
	}
	return nil
}

func (s *Service) ReserveBountyFunds(ctx context.Context, tx pgx.Tx, projectID, bountyID int64, rewardSats, feeSats int64) error {
	return s.RecordTransaction(ctx, tx, []Entry{
		{Account: projectAccount(projectID, "available"), Direction: "debit", AmountSats: rewardSats, RefType: "bounty_reserve", RefID: bountyID},
		{Account: projectAccount(projectID, "reserved"), Direction: "credit", AmountSats: rewardSats, RefType: "bounty_reserve", RefID: bountyID},
	})
}

func (s *Service) RefundBountyFunds(ctx context.Context, tx pgx.Tx, projectID, bountyID int64, rewardSats int64) error {
	return s.RecordTransaction(ctx, tx, []Entry{
		{Account: projectAccount(projectID, "reserved"), Direction: "debit", AmountSats: rewardSats, RefType: "bounty_refund", RefID: bountyID},
		{Account: projectAccount(projectID, "available"), Direction: "credit", AmountSats: rewardSats, RefType: "bounty_refund", RefID: bountyID},
	})
}

func (s *Service) ProcessPayout(ctx context.Context, tx pgx.Tx, projectID, bountyID, userID int64, rewardSats, feeSats, routingFeeSats int64) error {
	netPayout := rewardSats - feeSats
	entries := []Entry{
		{Account: projectAccount(projectID, "reserved"), Direction: "debit", AmountSats: rewardSats, RefType: "payout", RefID: bountyID},
		{Account: "user:" + strconv.FormatInt(userID, 10) + ":external", Direction: "credit", AmountSats: netPayout, RefType: "payout", RefID: bountyID},
		{Account: "platform:fees", Direction: "credit", AmountSats: feeSats, RefType: "payout", RefID: bountyID},
	}
	if routingFeeSats > 0 {
		entries = append(entries,
			Entry{Account: "node:liquidity", Direction: "debit", AmountSats: routingFeeSats, RefType: "payout", RefID: bountyID},
			Entry{Account: "platform:routing_costs", Direction: "credit", AmountSats: routingFeeSats, RefType: "payout", RefID: bountyID},
		)
	}
	return s.RecordTransaction(ctx, tx, entries)
}

func (s *Service) CreditDeposit(ctx context.Context, tx pgx.Tx, projectID, depositID int64, amountSats int64) error {
	return s.RecordTransaction(ctx, tx, []Entry{
		{Account: "node:liquidity", Direction: "debit", AmountSats: amountSats, RefType: "deposit", RefID: depositID},
		{Account: projectAccount(projectID, "available"), Direction: "credit", AmountSats: amountSats, RefType: "deposit", RefID: depositID},
	})
}

func projectAccount(projectID int64, bucket string) string {
	return "project:" + strconv.FormatInt(projectID, 10) + ":" + bucket
}

func (s *Service) GetAccountBalance(ctx context.Context, account string) (int64, error) {
	var balance int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN direction = 'credit' THEN amount_sats ELSE -amount_sats END), 0)
		FROM ledger_entries WHERE account = $1
	`, account).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return balance, err
}

type WalletMismatch struct {
	ProjectID       int64 `json:"project_id"`
	AvailableCached int64 `json:"available_cached_sats"`
	AvailableLedger int64 `json:"available_ledger_sats"`
	ReservedCached  int64 `json:"reserved_cached_sats"`
	ReservedLedger  int64 `json:"reserved_ledger_sats"`
}

// ReciprocalFlag describes an owner/contributor pair with repeat merged work.
// Mutual is set when the pair funds each other in both directions.
type ReciprocalFlag struct {
	OwnerUserID       int64 `json:"owner_user_id"`
	ContributorUserID int64 `json:"contributor_user_id"`
	MergedBounties    int   `json:"merged_bounties"`
	Mutual            bool  `json:"mutual"`
}

// DetectReciprocalPatterns finds owner/contributor pairs with unusually
// frequent merged work. Flags are for manual review, never automatic action.
func (s *Service) DetectReciprocalPatterns(ctx context.Context) ([]ReciprocalFlag, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.owner_user_id, c.user_id, COUNT(DISTINCT b.id)
		FROM bounties b
		JOIN claims c ON c.bounty_id = b.id
		JOIN projects p ON b.project_id = p.id
		JOIN pull_requests pr ON pr.claim_id = c.id AND pr.merged
		WHERE b.state IN ('merged', 'paid')
		GROUP BY 1, 2 HAVING COUNT(DISTINCT b.id) >= $1
		ORDER BY 3 DESC
	`, reciprocalFlagThreshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flags []ReciprocalFlag
	for rows.Next() {
		var flag ReciprocalFlag
		if err := rows.Scan(&flag.OwnerUserID, &flag.ContributorUserID, &flag.MergedBounties); err != nil {
			return nil, err
		}
		flags = append(flags, flag)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	pairs := make(map[[2]int64]bool, len(flags))
	for _, flag := range flags {
		pairs[[2]int64{flag.OwnerUserID, flag.ContributorUserID}] = true
	}
	for i := range flags {
		if pairs[[2]int64{flags[i].ContributorUserID, flags[i].OwnerUserID}] {
			flags[i].Mutual = true
		}
	}
	return flags, nil
}

// ProjectPaidLast24h totals settled payouts for a project over the trailing
// 24 hours.
func (s *Service) ProjectPaidLast24h(ctx context.Context, projectID int64) (int64, error) {
	var total int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(p.amount_sats), 0) FROM payouts p
		JOIN bounties b ON p.bounty_id = b.id
		WHERE b.project_id = $1 AND p.status IN ('settled', 'claimed_via_link') AND p.settled_at > now() - interval '24 hours'
	`, projectID).Scan(&total)
	return total, err
}

// CheckProjectPayoutCap rejects a payout that would push a project over its
// trailing-24h cap. A non-positive cap disables the check. The payout flow
// calls this before sending.
func (s *Service) CheckProjectPayoutCap(ctx context.Context, projectID, additionalSats, capSats int64) error {
	if capSats <= 0 {
		return nil
	}
	paid, err := s.ProjectPaidLast24h(ctx, projectID)
	if err != nil {
		return err
	}
	if paid+additionalSats > capSats {
		return ErrDailyPayoutCapExceeded
	}
	return nil
}

type ReconciliationReport struct {
	CheckedAt        time.Time        `json:"checked_at"`
	AccountBalances  map[string]int64 `json:"account_balances"`
	WalletMismatches []WalletMismatch `json:"wallet_mismatches"`
	UnbalancedTxIDs  []string         `json:"unbalanced_transaction_ids"`
	FraudFlags       []ReciprocalFlag `json:"fraud_flags,omitempty"`
	NodeBalanceSats  int64            `json:"node_balance_sats"`
	LiabilitiesSats  int64            `json:"liabilities_sats"`
	EquitySats       int64            `json:"equity_sats"`
	InFlightSats     int64            `json:"in_flight_sats"`
	DriftSats        int64            `json:"drift_sats"`
	Severity         string           `json:"severity"`
	NodeBalanceError string           `json:"node_balance_error,omitempty"`
	UnmatchedOpen    int              `json:"unmatched_open_receipts"`
	Healthy          bool             `json:"healthy"`
}

func (s *Service) Reconcile(ctx context.Context) (*ReconciliationReport, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT account, SUM(CASE WHEN direction = 'credit' THEN amount_sats ELSE -amount_sats END) as balance
		FROM ledger_entries
		GROUP BY account
	`)
	if err != nil {
		return nil, err
	}

	report := &ReconciliationReport{
		CheckedAt:        time.Now().UTC(),
		AccountBalances:  make(map[string]int64),
		WalletMismatches: []WalletMismatch{},
		UnbalancedTxIDs:  []string{},
		Healthy:          true,
	}
	for rows.Next() {
		var account string
		var balance int64
		if err := rows.Scan(&account, &balance); err != nil {
			rows.Close()
			return nil, err
		}
		report.AccountBalances[account] = balance
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	walletRows, err := s.pool.Query(ctx, `
		WITH ledger_balances AS (
			SELECT account, SUM(CASE WHEN direction = 'credit' THEN amount_sats ELSE -amount_sats END) AS balance
			FROM ledger_entries GROUP BY account
		)
		SELECT w.project_id, w.available_sats, COALESCE(a.balance, 0),
		       w.reserved_sats, COALESCE(r.balance, 0)
		FROM wallets w
		LEFT JOIN ledger_balances a ON a.account = 'project:' || w.project_id::text || ':available'
		LEFT JOIN ledger_balances r ON r.account = 'project:' || w.project_id::text || ':reserved'
		WHERE w.available_sats <> COALESCE(a.balance, 0)
		   OR w.reserved_sats <> COALESCE(r.balance, 0)
		ORDER BY w.project_id
	`)
	if err != nil {
		return nil, err
	}
	for walletRows.Next() {
		var mismatch WalletMismatch
		if err := walletRows.Scan(&mismatch.ProjectID, &mismatch.AvailableCached, &mismatch.AvailableLedger, &mismatch.ReservedCached, &mismatch.ReservedLedger); err != nil {
			walletRows.Close()
			return nil, err
		}
		report.WalletMismatches = append(report.WalletMismatches, mismatch)
	}
	if err := walletRows.Err(); err != nil {
		walletRows.Close()
		return nil, err
	}
	walletRows.Close()

	txRows, err := s.pool.Query(ctx, `
		SELECT tx_id::text
		FROM ledger_entries
		GROUP BY tx_id
		HAVING SUM(CASE WHEN direction = 'credit' THEN amount_sats ELSE -amount_sats END) <> 0
		ORDER BY tx_id
	`)
	if err != nil {
		return nil, err
	}
	for txRows.Next() {
		var txID string
		if err := txRows.Scan(&txID); err != nil {
			txRows.Close()
			return nil, err
		}
		report.UnbalancedTxIDs = append(report.UnbalancedTxIDs, txID)
	}
	if err := txRows.Err(); err != nil {
		txRows.Close()
		return nil, err
	}
	txRows.Close()

	for account, balance := range report.AccountBalances {
		if strings.HasPrefix(account, "project:") || strings.HasPrefix(account, "user:") {
			// User external accounts represent already-paid amounts, not funds
			// still owed by the platform. Only project wallet buckets are liabilities.
			if strings.HasPrefix(account, "project:") && balance > 0 {
				report.LiabilitiesSats += balance
			}
		}
	}
	report.EquitySats = report.AccountBalances["platform:fees"] - report.AccountBalances["platform:routing_costs"]
	if s.nodeBalance == nil {
		report.Severity = "error"
		report.NodeBalanceError = "node balance provider is not configured"
	} else if balance, err := s.nodeBalance.GetTotalBalance(ctx); err != nil {
		report.Severity = "error"
		report.NodeBalanceError = err.Error()
	} else {
		report.NodeBalanceSats = balance
	}
	if report.Severity != "error" {
		if err := s.pool.QueryRow(ctx, `SELECT COALESCE(SUM(amount_sats), 0) FROM payouts WHERE status = 'sending'`).Scan(&report.InFlightSats); err != nil {
			return nil, err
		}
		report.DriftSats = report.NodeBalanceSats - (report.LiabilitiesSats + report.EquitySats)
		switch {
		case report.DriftSats < -100 && -report.DriftSats <= report.InFlightSats+100:
			report.Severity = "info"
		case report.DriftSats < -100:
			report.Severity = "critical"
		case report.DriftSats > 100:
			report.Severity = "warning"
		default:
			report.Severity = "ok"
		}
	}
	if flags, err := s.DetectReciprocalPatterns(ctx); err != nil {
		s.log.Warn().Err(err).Msg("reciprocal-pattern detection failed; continuing reconciliation")
	} else if len(flags) > 0 {
		report.FraudFlags = flags
		s.log.Warn().Int("flagged_pairs", len(flags)).Msg("reciprocal payment patterns flagged for manual review")
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM unmatched_receipts WHERE status = 'open'`).Scan(&report.UnmatchedOpen); err != nil {
		return nil, err
	}
	if report.UnmatchedOpen > 0 {
		s.log.Warn().Int("open_receipts", report.UnmatchedOpen).Msg("unmatched Lightning receipts await staff review")
	}
	metrics.ReconciliationDrift.Set(float64(report.DriftSats))
	metrics.ReconciliationRuns.WithLabelValues(report.Severity).Inc()
	report.Healthy = len(report.WalletMismatches) == 0 && len(report.UnbalancedTxIDs) == 0 && (report.Severity == "ok" || report.Severity == "info")
	return report, nil
}

func (s *Service) RunReconciliation(ctx context.Context) error {
	report, err := s.Reconcile(ctx)
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(report)
	if err != nil {
		return err
	}
	var nodeBalance any
	if report.NodeBalanceError == "" {
		nodeBalance = report.NodeBalanceSats
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO reconciliation_runs (checked_at, node_balance_sats, liabilities_sats, equity_sats, in_flight_sats, drift_sats, severity, report)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
	`, report.CheckedAt, nodeBalance, report.LiabilitiesSats, report.EquitySats, report.InFlightSats, report.DriftSats, report.Severity, string(metadata)); err != nil {
		return err
	}
	if report.Severity == "critical" || report.Severity == "error" || len(report.WalletMismatches) > 0 || len(report.UnbalancedTxIDs) > 0 {
		reason, _ := json.Marshal(map[string]any{"severity": report.Severity, "drift_sats": report.DriftSats})
		if _, err := s.pool.Exec(ctx, `
			UPDATE system_flags SET enabled = TRUE, reason = $1, changed_at = now(), changed_by = 'reconciliation'
			WHERE name IN ('funding_paused','outflows_paused')
		`, string(reason)); err != nil {
			return err
		}
	}
	if report.Severity == "warning" {
		s.log.Warn().Int64("drift_sats", report.DriftSats).Msg("reconciliation found node funds not explained by ledger")
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO audit_log (actor, action, subject, metadata)
		VALUES ('system', 'ledger.reconciliation', 'platform', $1::jsonb)
	`, string(metadata)); err != nil {
		return err
	}
	if !report.Healthy {
		return fmt.Errorf("reconciliation severity=%s drift=%d sats, %d wallet mismatches, %d unbalanced transactions", report.Severity, report.DriftSats, len(report.WalletMismatches), len(report.UnbalancedTxIDs))
	}
	return nil
}
