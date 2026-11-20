# Test coverage gaps: fuzz, chaos, E2E, cursor/filter DB tests

Labels: testing

Priority: Medium (invariants unproven beyond unit/integration happy paths).

## Problem

The testing strategy requires property/fuzz tests (ledger balance, fee
rounding, idempotency), a chaos harness (crash mid-payout, node offline,
duplicate webhooks), and Playwright E2E (fund/claim/merge/pay); none exist.
DB-backed coverage is also missing for filter/cursor combinations, rollback
paths, deposit crash recovery, and every reconciliation severity.

## Evidence

- [testing-strategy.md](/home/bocal/SatLight/docs/specs/testing-strategy.md:5)
  layers table; no fuzz/chaos/E2E targets in the repo.
- [M2_COMPLETION_GUIDE.md](/home/bocal/SatLight/docs/guides/M2_COMPLETION_GUIDE.md:95)
  lists the same DB-coverage gaps.

## Acceptance criteria

- [ ] Fuzz targets for ledger balance, fee math, idempotency-key handling.
- [ ] Chaos harness for crash-mid-payout, node-offline, duplicate-webhook cases.
- [ ] E2E fund/claim/merge/pay flow; DB tests for cursor/filter combos and all
      reconciliation severities.
