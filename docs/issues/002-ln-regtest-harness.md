# LND regtest harness missing; `make test-ln` runs zero tests

Labels: testing

Priority: Critical (payout paths unproven against a real node).

## Problem

`internal/lightning/` has no test files and compose ships `bitcoind` + `lnd`
with no setup automation (no mining, wallet init, peering, channel opens).
Payout code is covered only by Postgres integration tests with a stubbed
Lightning service, so fee accounting, failure modes, and crash recovery are
unverified. `funding_paused` / `outflows_paused` must stay on until this lands.

## Evidence

- [testing-strategy.md](/home/bocal/SatLight/docs/specs/testing-strategy.md:26).
- [Makefile](/home/bocal/SatLight/Makefile:43) `test-ln` target runs an empty package.
- [docker-compose.yml](/home/bocal/SatLight/docker-compose.yml:24) has no setup scripts.

## Acceptance criteria

- [ ] Harness brings up regtest bitcoind + 2 LND nodes, mines, funds, opens channels.
- [ ] Tests cover: deposit settlement via `SubscribeInvoices`, `PayInvoice` success
      with fee accounting, no-route/offline failures, invoice expiry, duplicate
      payment jobs, LND restart mid-payment with payment-hash recovery.
- [ ] Suite runs in CI nightly and on payout-code changes per the testing strategy.
- [ ] `docs/specs/testing-strategy.md` outstanding-harness note is resolved.
