# Payment status/fee read from first TrackPaymentV2 message

Labels: bug

Priority: High (wrong routing-fee ledger entries).

## Problem

`GetPaymentResult` returns the status and fee from the first `TrackPaymentV2`
stream message, which can be `IN_FLIGHT` with fee 0 rather than the terminal
result. Settle paths then book understated routing costs.

## Evidence

- [service.go](/home/bocal/SatLight/internal/lightning/service.go:232) returns on
  first `Recv` without waiting for a terminal state.
- Callers in [payout/service.go](/home/bocal/SatLight/internal/payout/service.go:341)
  and :669 treat the returned fee as final.

## Acceptance criteria

- [ ] Payment lookup waits for a terminal status (or uses a point lookup) with a
      bounded timeout; in-flight is reported distinctly.
- [ ] Fee booked equals the terminal routing fee; tests cover in-flight-then-
      succeeded and failed sequences.
