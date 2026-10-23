# ADR-013: Reconciliation mismatch response

- **Status:** Proposed
- **Date:** 2026-10-23

## Context
Satlight holds user funds in a Lightning node and tracks who is owed what in a ledger. They must agree. A mismatch can mean a bug, a missed or duplicate credit, a stuck payment, or theft. The response must fail closed without causing unnecessary outages.

## Definitions
- `L` = ledger liabilities: all `project:*:available`, `reserved`, `withdrawing`, plus unsettled payouts.
- `E` = platform equity: accrued fees minus routing costs paid.
- `N` = node balance: channel local balance plus confirmed on-chain funds.
- `F` = outgoing payments currently in flight.
- `D = N - (L + E)` = drift. `T` = tolerance (default 100 sats).

## Decision
| Condition | Severity | Automatic action |
|---|---|---|
| `abs(D) <= T` | OK | None |
| `D < 0` and `-D <= F + T` | Info | None. Explained by in-flight payments; re-check next run |
| `D < 0` and `-D > F + T` (node holds less than owed) | **Critical (SEV1)** | **Pause all outflows** (payouts and withdrawals) and **stop issuing new deposit invoices and funding new bounties**. Page on-call |
| `D > T` (node holds more than the ledger explains) | Warning (SEV2) | Alert only, no pause. Usually an unmatched receipt or missed credit. Escalate if unresolved in 24h |

Rules:
1. **Fail closed on shortfall.** Paying out more than we can prove we hold is the dangerous direction, so outflows stop immediately.
2. **Never reject incoming money.** Payments to invoices already issued are still received and recorded; only issuing new invoices stops.
3. **Keep tracking in-flight payments** while paused (look up by payment hash), so nothing is lost or paid twice.
4. **No automatic resume.** A Critical state is cleared only by a human after review.
5. **Run cadence:** hourly, after each payout batch, and after any node reconnect. Each run is stored in `reconciliation_runs`.

## Who is alerted
| Severity | Who | How | Target |
|---|---|---|---|
| Critical | Primary on-call (developer) and founder | Pager/phone plus incident channel | Acknowledge within 15 minutes; if not, escalate to the whole team |
| Warning | Primary on-call | Chat plus email | Review next business day |
| Info | Dashboard only | None | n/a |

**Who can resume:** two people must approve, the developer on call and the founder, after a clean reconciliation run and a recorded root cause in `audit_log`.

**Users:** show a neutral banner ("Payouts are briefly paused. Funds are safe and will be sent."). If funds were affected, notify those users within 24 hours, per the incident runbook.

## Consequences
- Small timing differences do not cause outages (in-flight allowance).
- A real shortfall stops money leaving within one reconciliation run.
- Adds `reconciliation_runs`, `system_flags`, and `internal/reconcile` with tests.
- Names and contact methods for on-call must be filled in before beta.
