# LND payments use the invoice macaroon; payment macaroon is discarded

**Status: fixed and verified in working tree (`make test`, `make test-int` green). Do not file.**

Labels: bug,security

Priority: Critical (payout path broken or over-privileged).

## Problem

`lightning.Service.Connect` loads the payment macaroon, discards its credential,
and dials a single gRPC connection authenticated only with the invoice macaroon.
The router client used by `PayInvoice`/`SendPaymentV2` shares that connection,
so either LND rejects every payout with permission-denied or the invoice
macaroon is over-privileged, violating least-privilege separation.

## Evidence

- [service.go](/home/bocal/SatLight/internal/lightning/service.go:85) discards the
  payment credential (`_, _ = macaroons.NewMacaroonCredential(paymentMac)`).
- [service.go](/home/bocal/SatLight/internal/lightning/service.go:89) dials with
  `grpc.WithPerRPCCredentials(invoiceCred)` for all calls.
- Spec requires separate receive/send macaroons
  (`docs/SATLIGHT-DOCS.md` 3.2, `docs/guides/SKILLS.md` rule 8).

## Acceptance criteria

- [ ] Payments are authorized with the payment macaroon (separate connection or
      per-RPC credentials); invoice calls keep the invoice macaroon.
- [ ] Regtest test proves a payout succeeds with least-privilege macaroons and
      fails closed when the payment macaroon is missing.
- [ ] Docs updated (`docs/specs/lightning-integration.md`, runbook macaroon recipe).
