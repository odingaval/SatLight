//go:build integration

package ops

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/satlight/satlight/internal/config"
)

func opsFixture(t *testing.T) (*Service, *pgxpool.Pool) {
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
	if _, err := pool.Exec(ctx, `UPDATE system_flags SET enabled = TRUE, reason = 'test pause' WHERE name IN ('funding_paused','outflows_paused')`); err != nil {
		t.Fatal(err)
	}
	return NewService(&config.Config{}, pool), pool
}

func TestIntegrationTwoPersonResume(t *testing.T) {
	service, pool := opsFixture(t)
	ctx := context.Background()

	id, err := service.ProposeResume(ctx, "outflows_paused", "alice", "reconciliation green")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ProposeResume(ctx, "outflows_paused", "mallory", "second try"); !errors.Is(err, ErrProposalOpen) {
		t.Fatalf("second proposal: err=%v, want ErrProposalOpen", err)
	}
	if err := service.ApproveResume(ctx, id, "alice"); !errors.Is(err, ErrSameApprover) {
		t.Fatalf("self-approve: err=%v, want ErrSameApprover", err)
	}
	if err := service.ApproveResume(ctx, id, "bob"); err != nil {
		t.Fatal(err)
	}
	var enabled bool
	var changedBy string
	if err := pool.QueryRow(ctx, `SELECT enabled, changed_by FROM system_flags WHERE name = 'outflows_paused'`).Scan(&enabled, &changedBy); err != nil {
		t.Fatal(err)
	}
	if enabled || changedBy != "alice+bob" {
		t.Fatalf("flag enabled=%v by=%q, want cleared by alice+bob", enabled, changedBy)
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action IN ('flag.resume_proposed','flag.resume_approved') AND subject = 'flag:outflows_paused'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits < 2 {
		t.Fatalf("resume audit entries = %d, want at least 2", audits)
	}
	if err := service.ApproveResume(ctx, id, "carol"); !errors.Is(err, ErrNoOpenProposal) {
		t.Fatalf("re-approve: err=%v, want ErrNoOpenProposal", err)
	}
	if _, err := service.ProposeResume(ctx, "outflows_paused", "alice", "again"); !errors.Is(err, ErrFlagNotPaused) {
		t.Fatalf("propose on cleared flag: err=%v, want ErrFlagNotPaused", err)
	}

	rejectID, err := service.ProposeResume(ctx, "funding_paused", "alice", "test reject")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RejectResume(ctx, rejectID, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT enabled FROM system_flags WHERE name = 'funding_paused'`).Scan(&enabled); err != nil || !enabled {
		t.Fatalf("flag after reject: enabled=%v err=%v, want still paused", enabled, err)
	}
	if _, err := service.ProposeResume(ctx, "funding_paused", "bob", "fresh try"); err != nil {
		t.Fatalf("propose after reject: err=%v, want nil", err)
	}

	if _, err := service.ProposeResume(ctx, "self_destruct", "alice", "x"); !errors.Is(err, ErrUnknownFlag) {
		t.Fatalf("unknown flag: err=%v, want ErrUnknownFlag", err)
	}
	if _, err := service.ProposeResume(ctx, "funding_paused", "", "x"); !errors.Is(err, ErrMissingActor) {
		t.Fatalf("empty actor: err=%v, want ErrMissingActor", err)
	}
	if _, err := service.ProposeResume(ctx, "funding_paused", "alice", ""); !errors.Is(err, ErrMissingReason) {
		t.Fatalf("empty reason: err=%v, want ErrMissingReason", err)
	}
}

func TestIntegrationStatusCombinesEnvAndDB(t *testing.T) {
	service, pool := opsFixture(t)
	ctx := context.Background()

	status, err := service.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.FundingPaused || !status.OutflowsPaused || status.OutflowsPausedByEnv || status.Message == "" {
		t.Fatalf("status = %+v, want both paused from DB with a message", status)
	}
	if _, err := pool.Exec(ctx, `UPDATE system_flags SET enabled = FALSE WHERE name IN ('funding_paused','outflows_paused')`); err != nil {
		t.Fatal(err)
	}
	if status, err := service.Status(ctx); err != nil || status.FundingPaused || status.OutflowsPaused {
		t.Fatalf("cleared status = %+v err=%v, want all running", status, err)
	}
	service.cfg.PayoutsPaused = true
	if status, err := service.Status(ctx); err != nil || status.FundingPaused || !status.OutflowsPaused || !status.OutflowsPausedByEnv {
		t.Fatalf("env-override status = %+v err=%v, want outflows paused by env only", status, err)
	}
}
