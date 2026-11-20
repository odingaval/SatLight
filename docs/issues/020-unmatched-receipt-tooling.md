# Unmatched-receipt review tooling and late-credit alerts

Labels: ops,feature

Priority: Medium (received money waits on a manual process that has no UI).

## Problem

ADR-011 quarantines unknown receipts for staff review within 2 business days and
alerts on any unmatched receipt or >5 late credits/24h, but there is no review
tooling, no paging, and no maintainer surfacing beyond logs.

## Evidence

- [011-late-deposits.md](/home/bocal/SatLight/docs/011-late-deposits.md:27)
  implementation status lists review workflow and paging as outstanding.
- `RecordUnmatchedReceipt` ([service.go](/home/bocal/SatLight/internal/bounty/service.go:1364))
  persists + audit-logs only.

## Acceptance criteria

- [ ] Staff review flow (credit-to-project / hold / refund-against-payer-invoice)
      with two-person control and audit trail.
- [ ] Alerts wired: any unmatched receipt, >5 late credits/24h.
- [ ] Maintainer-visible late-credit notice.
