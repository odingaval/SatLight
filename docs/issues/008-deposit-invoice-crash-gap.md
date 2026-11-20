# Crash between LND AddInvoice and deposit persistence loses the mapping

Labels: bug

Priority: High (paid invoice with no credit path except quarantine).

## Problem

If the process crashes after LND creates the invoice but before the deposit row
is persisted, the payment hash mapping is lost. A later settlement lands in
`unmatched_receipts` instead of crediting the project automatically.

## Evidence

- `GetOrCreateDeposit` ([service.go](/home/bocal/SatLight/internal/bounty/service.go:1208))
  creates the invoice via callback before persisting.
- [BACKEND_IMPLEMENTATION.md](/home/bocal/SatLight/docs/guides/BACKEND_IMPLEMENTATION.md:734)
  lists this crash gap as outstanding.

## Acceptance criteria

- [ ] Invoice creation and deposit persistence are crash-safe (e.g. reserve the
      idempotency record first, reconcile widowed invoices on startup, or adopt
      LND idempotency keyed by our deposit id).
- [ ] Startup/periodic job re-attaches widowed invoices to their deposits.
- [ ] Test kills the process between AddInvoice and commit and proves recovery.
