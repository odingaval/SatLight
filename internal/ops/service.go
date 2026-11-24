// Package ops owns the kill-switch resume ledger and the public platform
// status. Pausing is easy (one env var or one auto-pause); resuming is
// deliberately hard: two operators, recorded reasons, and an audit trail.
package ops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/satlight/satlight/internal/config"
)

var (
	ErrUnknownFlag    = errors.New("unknown system flag")
	ErrFlagNotPaused  = errors.New("flag is not paused")
	ErrProposalOpen   = errors.New("a resume proposal is already open for this flag")
	ErrNoOpenProposal = errors.New("no open resume proposal")
	ErrSameApprover   = errors.New("approver must differ from proposer")
	ErrMissingActor   = errors.New("actor is required")
	ErrMissingReason  = errors.New("reason is required")
	resumableFlags    = []string{"funding_paused", "outflows_paused"}
)

type Service struct {
	cfg  *config.Config
	pool *pgxpool.Pool
	log  zerolog.Logger
}

func NewService(cfg *config.Config, pool *pgxpool.Pool) *Service {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: gin.DefaultWriter, TimeFormat: zerolog.TimeFormatUnix}).With().Timestamp().Logger()
	return &Service{cfg: cfg, pool: pool, log: logger}
}

func validFlag(flag string) bool {
	for _, name := range resumableFlags {
		if flag == name {
			return true
		}
	}
	return false
}

// ProposeResume opens a two-person resume for a paused flag. The flag row is
// locked so concurrent proposals serialize; at most one stays open.
func (s *Service) ProposeResume(ctx context.Context, flag, actor, reason string) (int64, error) {
	flag = strings.TrimSpace(flag)
	actor = strings.TrimSpace(actor)
	reason = strings.TrimSpace(reason)
	if !validFlag(flag) {
		return 0, ErrUnknownFlag
	}
	if actor == "" {
		return 0, ErrMissingActor
	}
	if reason == "" {
		return 0, ErrMissingReason
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if err := tx.QueryRow(ctx, `SELECT enabled FROM system_flags WHERE name = $1 FOR UPDATE`, flag).Scan(&enabled); err != nil {
		return 0, err
	}
	if !enabled {
		return 0, ErrFlagNotPaused
	}
	var open int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM flag_resumptions WHERE flag_name = $1 AND status = 'open'`, flag).Scan(&open); err != nil {
		return 0, err
	}
	if open > 0 {
		return 0, ErrProposalOpen
	}
	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO flag_resumptions (flag_name, proposed_by, reason)
		VALUES ($1, $2, $3) RETURNING id
	`, flag, actor, reason).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_log (actor, action, subject, metadata)
		VALUES ($1, 'flag.resume_proposed', $2, $3::jsonb)
	`, actor, "flag:"+flag, fmt.Sprintf(`{"proposal_id":%d,"reason":%q}`, id, reason)); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	s.log.Info().Int64("proposal_id", id).Str("flag", flag).Str("actor", actor).Msg("resume proposed")
	return id, nil
}

// ApproveResume completes a two-person resume: the approver must differ from
// the proposer, and the flag clears in the same transaction as the audit.
func (s *Service) ApproveResume(ctx context.Context, proposalID int64, actor string) error {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return ErrMissingActor
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var flag, proposedBy, reason, proposalStatus string
	err = tx.QueryRow(ctx, `SELECT flag_name, proposed_by, reason, status FROM flag_resumptions WHERE id = $1 FOR UPDATE`, proposalID).
		Scan(&flag, &proposedBy, &reason, &proposalStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoOpenProposal
	}
	if err != nil {
		return err
	}
	if proposalStatus != "open" {
		return ErrNoOpenProposal
	}
	if actor == proposedBy {
		return ErrSameApprover
	}
	if _, err := tx.Exec(ctx, `UPDATE flag_resumptions SET status = 'approved', approved_by = $1, approved_at = now() WHERE id = $2`, actor, proposalID); err != nil {
		return err
	}
	clearedBy := proposedBy + "+" + actor
	if _, err := tx.Exec(ctx, `UPDATE system_flags SET enabled = FALSE, reason = $1, changed_at = now(), changed_by = $2 WHERE name = $3`,
		"resumed: "+reason, clearedBy, flag); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_log (actor, action, subject, metadata)
		VALUES ($1, 'flag.resume_approved', $2, $3::jsonb)
	`, clearedBy, "flag:"+flag, fmt.Sprintf(`{"proposal_id":%d,"reason":%q}`, proposalID, reason)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.log.Info().Int64("proposal_id", proposalID).Str("flag", flag).Str("actor", actor).Msg("resume approved; flag cleared")
	return nil
}

// RejectResume closes an open proposal without touching the flag.
func (s *Service) RejectResume(ctx context.Context, proposalID int64, actor string) error {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return ErrMissingActor
	}
	result, err := s.pool.Exec(ctx, `UPDATE flag_resumptions SET status = 'rejected' WHERE id = $1 AND status = 'open'`, proposalID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrNoOpenProposal
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO audit_log (actor, action, subject, metadata)
		VALUES ($1, 'flag.resume_rejected', $2, $3::jsonb)
	`, actor, fmt.Sprintf("proposal:%d", proposalID), fmt.Sprintf(`{"proposal_id":%d}`, proposalID))
	return err
}

// Status reports the effective kill-switch state. Outflows stop when EITHER
// the emergency env override or the database flag says so; funding follows
// the database flag only. Resume always flows through the database: clearing
// the env override additionally requires a process restart.
type Status struct {
	FundingPaused       bool   `json:"funding_paused"`
	OutflowsPaused      bool   `json:"outflows_paused"`
	OutflowsPausedByEnv bool   `json:"outflows_paused_by_env"`
	Message             string `json:"message,omitempty"`
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	var status Status
	rows, err := s.pool.Query(ctx, `SELECT name, enabled FROM system_flags WHERE name IN ('funding_paused','outflows_paused')`)
	if err != nil {
		return status, err
	}
	dbFlags := map[string]bool{}
	for rows.Next() {
		var name string
		var enabled bool
		if err := rows.Scan(&name, &enabled); err != nil {
			rows.Close()
			return status, err
		}
		dbFlags[name] = enabled
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return status, err
	}
	rows.Close()
	status.FundingPaused = dbFlags["funding_paused"]
	status.OutflowsPausedByEnv = s.cfg != nil && s.cfg.PayoutsPaused
	status.OutflowsPaused = status.OutflowsPausedByEnv || dbFlags["outflows_paused"]
	if status.OutflowsPaused {
		status.Message = "Payouts are temporarily paused. Merges and deposits continue; queued payouts resume automatically."
	} else if status.FundingPaused {
		status.Message = "Project funding is temporarily paused."
	}
	return status, nil
}
