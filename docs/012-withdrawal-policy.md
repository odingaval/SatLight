# ADR-012: Project withdrawal policy

- **Status:** Conditional product gate (legal review required before activation)
- **Date:** 2026-10-23

## Context
Under custodial escrow (ADR-001), maintainers deposit sats and must be able to get unused funds back, or Satlight is holding their money with no exit. Withdrawals also create risk: they can be used to move funds with no real bounty activity (a money transmission and laundering concern), to drain a project after account takeover, or to exploit a payout bug.

## Options
| Option | Summary | Verdict |
|---|---|---|
| A | Maintainers may withdraw **available (unreserved)** funds, with controls | Recommended |
| B | Refunds only when a project closes or a bounty is cancelled, handled manually | Fallback if counsel objects to A |
| C | No withdrawals | Rejected: traps users' funds |

## Decision
The endpoint remains unavailable until qualified counsel confirms that the custody model and withdrawal service may operate in each launch jurisdiction and identifies any licensing, AML, consumer-protection, and operational conditions. The current handler returns HTTP 501 and must remain unavailable until that release gate is satisfied.

If counsel approves the service, Option A below is the engineering proposal. It is not authorization to accept funds or activate withdrawals. If counsel does not approve Option A, use Option B and revise the Terms before any funds are accepted.

### Proposed Option A (conditional)
A maintainer may withdraw funds that are `available`. Reserved funds (open bounties) are never withdrawable; cancel the bounty first.

Conditions, all enforced server-side if approved:
1. **Who:** the project owner, with GitHub admin access to the repository re-verified at request time.
2. **Project status:** not frozen, no open fraud case, withdrawals not paused.
3. **Destination:** a Lightning address or a BOLT11 invoice for the exact amount. A new or changed destination needs verified email confirmation and a **48 hour cooldown** before first use. The current application has no email confirmation flow, so this check is not implementable yet.
4. **Minimum:** 1,000 sats.
5. **Deposit hold (beta):** a deposit becomes withdrawable **24 hours** after it settles. This slows deposit-and-withdraw abuse and gives fraud checks time to run.
6. **Daily cap (beta):** 1,000,000 sats per project per rolling 24 hours. Larger requests go to manual review.
7. **Fees:** the Lightning routing fee is deducted from the withdrawn amount (the maintainer bears it), using the same max-fee limits as payouts. Satlight takes no withdrawal fee.
8. **Safe flow:** the request moves funds `available` to `project:{id}:withdrawing`, the attempt is persisted before sending, then settles or returns to `available` on failure. The final fee treatment must be stated consistently: an exact-amount invoice requires the project to cover the invoice amount plus the actual routing fee unless an approved amountless-invoice flow is used.
9. **Closure:** if the GitHub App is uninstalled or the project closes, withdrawals stay open after a fraud check.
10. **Kill switch:** withdrawals stop when outflows are paused (ADR-013). Requests queue and are not lost only if that behavior is specifically approved and implemented; otherwise reject before reserving funds.

All limits are configuration, not code constants (see `.env.example`).

## Consequences
- Maintainers keep control of their money, which supports trust and the Terms.
- Counsel must confirm Option A is acceptable; if not, select Option B and update the Terms.
- Open decisions include the launch jurisdictions, exact invoice/routing fee treatment, email verification capability, and whether paused requests queue or are rejected.
