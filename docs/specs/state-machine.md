# Bounty State Machine Spec

## States and transitions
| From | Event | To | Side effects |
|---|---|---|---|
| draft | fund (wallet has balance) | open | Reserve `reward` in escrow (ledger) |
| draft | cancel | cancelled | none |
| open | claim | claimed | Create active claim with expiry |
| open | expiry | expired | Release reserved funds to project wallet and audit |
| open | cancel | cancelled | Release reserved funds |
| claimed | PR linked | in_review | Link PR to claim |
| claimed | claim expires / release | open | Claim marked expired/released; if bounty itself has expired, refund reservation and expire bounty |
| in_review | PR merged and verified | merged | Enqueue payout |
| in_review | PR closed unmerged | claimed | none |
| merged | payout settled | paid | Ledger: payout + fee |
| merged | payout fails | payout_failed | Retry schedule |
| payout_failed | retry succeeds | paid | |
| payout_failed | final failure | merged (claim link issued) | Email claim link |
| open / claimed / in_review | dispute opened | disputed | Funds stay reserved |
| disputed | resolved (pay) | merged | Enqueue payout |
| disputed | resolved (cancel) | cancelled | Release funds |

## Implementation rules
- All transitions go through one `Transition(ctx, tx, bountyID, event)` function that locks the bounty row (`SELECT ... FOR UPDATE`), checks the allowed table, performs side effects, and writes an `audit_log` entry.
- Illegal transitions return a typed error (HTTP 409).
- Ledger effects occur in the same DB transaction as the state change.
- Table-driven tests cover every valid and invalid transition.
