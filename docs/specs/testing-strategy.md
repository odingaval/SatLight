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

## LND regtest harness
`make test-ln` boots an isolated regtest network (`docker-compose.ln.yml`: bitcoind + 2 LND nodes), creates wallets, mines, funds, peers, and opens one channel per direction (`scripts/ln-setup.sh`), then runs `internal/lightning/regtest_test.go` against the real nodes: deposit settlement via `SubscribeInvoices`, successful `PayInvoice` with terminal fee accounting, no-route and offline-recipient failures, invoice expiry, duplicate-payment rejection, and post-restart payment-hash recovery. The suite runs nightly and on payout/lightning-code changes (`.github/workflows/ln.yml`). The harness has already paid for itself: it caught `DecodePayReq` on the wrong macaroon connection and an illegal `Amt` on fixed-amount sends, both fixed before they reached a funded node.
