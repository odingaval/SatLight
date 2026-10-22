package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"

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

func (s *Service) Reconcile(ctx context.Context) (map[string]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT account, SUM(CASE WHEN direction = 'credit' THEN amount_sats ELSE -amount_sats END) as balance
		FROM ledger_entries
		GROUP BY account
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	balances := make(map[string]int64)
	for rows.Next() {
		var account string
		var balance int64
		if err := rows.Scan(&account, &balance); err != nil {
			return nil, err
		}
		balances[account] = balance
	}
	return balances, rows.Err()
}
