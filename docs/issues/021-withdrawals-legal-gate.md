# Withdrawals legal gate tracker (counsel decision, Option A/B)

Labels: legal

Priority: Blocker (endpoint is 501 until counsel rules).

## Problem

Withdrawals return HTTP 501 pending counsel approval of the custody model per
jurisdiction. The engineering proposal (Option A: controlled self-service with
24h deposit hold, 48h destination cooldown, 1M-sat daily cap, idempotent
attempts, ledger accounting, kill switch) or fallback (Option B: manual return)
cannot start until counsel decides, and open questions (invoice/routing-fee
treatment, queue-vs-reject when paused) are unresolved.

## Evidence

- [server.go](/home/bocal/SatLight/internal/api/server.go:842) 501 handler.
- [012-withdrawal-policy.md](/home/bocal/SatLight/docs/012-withdrawal-policy.md:17)
  gate and conditions; email-confirmation flow noted as not implementable yet.

## Acceptance criteria

- [ ] Counsel records per-jurisdiction go/no-go with licensing/AML conditions.
- [ ] Option A or B selected; Terms updated before any funds accepted.
- [ ] If A: deposit aging, destination controls, caps, idempotent ledger-backed
      flow, and outflow kill-switch enforcement implemented and regtest-tested.
