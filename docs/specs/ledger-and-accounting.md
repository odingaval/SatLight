# Ledger and Accounting Spec

## Principles
- Integer sats only. Double-entry. Append-only (enforced by a DB trigger).
- Corrections are reversing entries, never edits.
- Every transaction has a `tx_id` whose debits equal credits.

## Accounts
| Account | Meaning |
|---|---|
| `node:liquidity` | Platform's Lightning node funds |
| `project:{id}:available` | Project funds not yet reserved |
| `project:{id}:reserved` | Funds escrowed to open bounties |
| `user:{id}:external` | Funds paid out to a contributor |
| `platform:fees` | Platform fee revenue |
| `platform:routing_costs` | Routing fees paid on payouts |

## Transactions
| Event | Debit | Credit |
|---|---|---|
| Deposit settled | node:liquidity | project:available |
| Bounty funded | project:available | project:reserved |
| Payout | project:reserved | user:external (reward - fee) |
| Fee | project:reserved | platform:fees |
| Routing cost | platform:fees | node:liquidity (expense tracked via platform:routing_costs) |
| Cancel/expire | project:reserved | project:available |
| Withdrawal | project:available | node:liquidity (outflow) |

## Fee calculation
`fee = floor(reward * PLATFORM_FEE_BPS / 10000)`, minimum 1 sat. Contributor receives `reward - fee`.

## Reconciliation (hourly)
1. Sum liabilities: all `project:*:available` + `project:*:reserved` + pending payouts.
2. Compare with node balance (channel local balance + on-chain), minus platform equity.
3. If the difference exceeds tolerance (default 0 sats unresolved beyond in-flight payments), alert, set `PAYOUTS_PAUSED`, and open an incident.
4. Store results in `audit_log`.

## Invariants (tested)
- Debits equal credits per `tx_id`.
- Wallet cache equals ledger-derived balance.
- At most one settled payout per bounty.
- No negative balances.
