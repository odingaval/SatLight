# Settlement has no poll backup; keysend HTLCs are unobserved

Labels: bug,ops

Priority: High (missed credits, ledger/node drift).

## Problem

Deposit settlement relies solely on the `SubscribeInvoices` stream. The spec
requires a periodic poll backup, and ADR-011 notes non-invoice/keysend HTLCs
are not observed at all, so some arriving sats never reach the ledger.

## Evidence

- [lightning-integration.md](/home/bocal/SatLight/docs/specs/lightning-integration.md:17):
  "Invoice subscription (plus a periodic poll as backup)".
- [011-late-deposits.md](/home/bocal/SatLight/docs/011-late-deposits.md:27):
  "non-invoice/keysend HTLC observation" outstanding.
- Only watcher is [server.go](/home/bocal/SatLight/internal/api/server.go:131)
  `WatchLightningInvoices`.

## Acceptance criteria

- [ ] Periodic poller backs the subscription (pending-deposit sweep + invoice
      lookup) with jitter and lag metrics.
- [ ] Keysend/unknown HTLC observation added or explicitly scoped out with
      counsel/ops sign-off and a documented residual-risk note.
- [ ] Tests cover stream-drop recovery and poller-discovered settlements.
