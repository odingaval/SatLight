# Testing Strategy

| Layer | Scope | Tools |
|---|---|---|
| Unit | State machine, fee math, ledger invariants, parsers | `go test`, table tests |
| Integration | DB, queue, webhook processing, API handlers | testcontainers-go |
| Lightning | Deposits, payouts, failures on regtest | LND + Polar / docker compose |
| End to end | Fund, claim, merge, pay through UI | Playwright with mocked GitHub |
| Property / fuzz | Ledger balance, fee rounding, idempotency | `testing/fuzz`, rapid |
| Chaos | Crash mid-payout, duplicate webhooks, node offline | custom harness |
| Security | Authz, webhook signatures, rate limits | unit + manual review |

## Must-pass invariants
1. No payout without a verified merge.
2. At most one settled payout per bounty.
3. Ledger debits equal credits; wallet cache equals ledger.
4. Node balance reconciles with ledger liabilities.
5. Idempotent money endpoints return the same result for repeated keys.

## Payout failure matrix
Success; no route; recipient offline; invoice expired; amount below recipient minimum; crash after send before commit; crash before send after commit; duplicate job; node restart in flight; kill switch on.

## CI gates
Lint, vet, unit and integration tests, `govulncheck`, secrets scan, OpenAPI validation, migration dry run. Lightning regtest suite runs nightly and on payout-code changes.
