# Testing Strategy

| Layer | Scope | Tools |
|---|---|---|
| Unit | State machine, fee math, ledger invariants, parsers | `go test`, table tests |
| Integration | PostgreSQL settlement, concurrent bounty reservation, reconciliation, then queue/webhook/API handlers | isolated PostgreSQL service through `make test-int` |
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

## Outstanding: LND regtest harness
`make test-ln` currently runs zero tests: `internal/lightning/` has no test files, and `docker-compose.yml` ships `bitcoind` + `lnd` images with no setup scripts (no mining, wallet init, peering, or channel-open automation). Before closed beta, add a harness that brings up a regtest network, funds and opens channels, and covers: deposit invoice settlement via `SubscribeInvoices`, successful `PayInvoice` with fee accounting, no-route and offline-recipient failures, invoice expiry, duplicate payment jobs, and an LND restart mid-payment proving payment-hash recovery. Until then, payout paths are covered only by Postgres integration tests with a stubbed Lightning service, and `funding_paused` / `outflows_paused` must stay on.
