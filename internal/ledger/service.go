package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

var (
	ErrLedgerImbalance    = errors.New("ledger entries do not balance")
	ErrInvalidLedgerEntry = errors.New("invalid ledger entry")
	ErrTooFewEntries      = errors.New("at least two entries required")
)

type Service struct {
	pool *pgxpool.Pool
	log  zerolog.Logger
}

func NewService(cfg interface{}, pool *pgxpool.Pool) *Service {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: gin.DefaultWriter, TimeFormat: zerolog.TimeFormatUnix}).With().Timestamp().Logger()
	return &Service{
		pool: pool,
		log:  logger,
	}
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
	return s.RecordTransaction(ctx, tx, []Entry{
		{Account: projectAccount(projectID, "reserved"), Direction: "debit", AmountSats: rewardSats, RefType: "payout", RefID: bountyID},
		{Account: "user:" + strconv.FormatInt(userID, 10) + ":external", Direction: "credit", AmountSats: netPayout, RefType: "payout", RefID: bountyID},
		{Account: "platform:fees", Direction: "credit", AmountSats: feeSats, RefType: "payout", RefID: bountyID},
		{Account: "platform:routing_costs", Direction: "credit", AmountSats: routingFeeSats, RefType: "payout", RefID: bountyID},
	})
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
	if err == sql.ErrNoRows {
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

type ReconciliationReport struct {
	CheckedAt        time.Time        `json:"checked_at"`
	AccountBalances  map[string]int64 `json:"account_balances"`
	WalletMismatches []WalletMismatch `json:"wallet_mismatches"`
	UnbalancedTxIDs  []string         `json:"unbalanced_transaction_ids"`
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

	report.Healthy = len(report.WalletMismatches) == 0 && len(report.UnbalancedTxIDs) == 0
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
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO audit_log (actor, action, subject, metadata)
		VALUES ('system', 'ledger.reconciliation', 'platform', $1::jsonb)
	`, string(metadata)); err != nil {
		return err
	}
	if !report.Healthy {
		return fmt.Errorf("ledger reconciliation found %d wallet mismatches and %d unbalanced transactions", len(report.WalletMismatches), len(report.UnbalancedTxIDs))
	}
	return nil
}
