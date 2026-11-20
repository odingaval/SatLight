# Kill-switch split (env vs DB flags); no two-person resume or paging

Labels: ops,bug

Priority: High (incident response untested and ambiguous).

## Problem

The runbook kill switch is the `PAYOUTS_PAUSED` env flag, but the code enforces
DB `system_flags` (`funding_paused`/`outflows_paused`, seeded paused). Compose
sets `PAYOUTS_PAUSED=false` while migrations keep flags paused, so operators get
conflicting signals. ADR-013 also requires paging, a user banner, and audited
two-person resume; findings are only logged.

## Evidence

- [operations.md](/home/bocal/SatLight/docs/runbooks/operations.md:31) vs
  [service.go](/home/bocal/SatLight/internal/payout/service.go:153) DB-flag check.
- [000002 migration](/home/bocal/SatLight/migrations/000002_deposit_and_reconciliation_safety.up.sql:39)
  seeds both flags paused.
- [013-reconciliation-response.md](/home/bocal/SatLight/docs/013-reconciliation-response.md:51)
  lists paging, resume tooling, and on-call names as outstanding.

## Acceptance criteria

- [ ] One documented kill-switch mechanism (env and DB semantics reconciled).
- [ ] Audited two-person resume path; no silent auto-resume.
- [ ] Critical/warning paging wired; on-call names filled; paused-payouts user
      banner shipped; game-day drill run and recorded.
