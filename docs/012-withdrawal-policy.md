# ADR-012: Project withdrawal policy

- **Status:** Proposed (depends on ADR-001 and the legal review of the custody model)
- **Date:** 2026-10-23

## Context
Under custodial escrow (ADR-001), maintainers deposit sats and must be able to get unused funds back, or Satlight is holding their money with no exit. Withdrawals also create risk: they can be used to move funds with no real bounty activity (a money transmission and laundering concern), to drain a project after account takeover, or to exploit a payout bug.

## Options
| Option | Summary | Verdict |
|---|---|---|
| A | Maintainers may withdraw **available (unreserved)** funds, with controls | Recommended |
| B | Refunds only when a project closes or a bounty is cancelled, handled manually | Fallback if counsel objects to A |
| C | No withdrawals | Rejected: traps users' funds |

## Decision (Option A)
A maintainer may withdraw funds that are `available`. Reserved funds (open bounties) are never withdrawable; cancel the bounty first.

Conditions, all enforced server-side:
1. **Who:** the project owner, with GitHub admin access to the repository re-verified at request time.
2. **Project status:** not frozen, no open fraud case, withdrawals not paused.
3. **Destination:** a Lightning address or a BOLT11 invoice for the exact amount. A new or changed destination needs email confirmation and a **48 hour cooldown** before first use (same rule as payout addresses).
4. **Minimum:** 1,000 sats.
5. **Deposit hold (beta):** a deposit becomes withdrawable **24 hours** after it settles. This slows deposit-and-withdraw abuse and gives fraud checks time to run.
6. **Daily cap (beta):** 1,000,000 sats per project per rolling 24 hours. Larger requests go to manual review.
7. **Fees:** the Lightning routing fee is deducted from the withdrawn amount (the maintainer bears it), using the same max-fee limits as payouts. Satlight takes no withdrawal fee.
8. **Safe flow:** the request moves funds `available` to `project:{id}:withdrawing`, the attempt is persisted before sending, then settles or returns to `available` on failure.
9. **Closure:** if the GitHub App is uninstalled or the project closes, withdrawals stay open after a fraud check.
10. **Kill switch:** withdrawals stop when outflows are paused (ADR-013). Requests queue and are not lost.

All limits are configuration, not code constants (see `.env.example`).

## Consequences
- Maintainers keep control of their money, which supports trust and the Terms.
- Adds a `withdrawals` table, a `withdrawing` ledger account, and a policy check (`internal/withdrawal`).
- Counsel must confirm Option A is acceptable; if not, fall back to Option B and update the Terms.
