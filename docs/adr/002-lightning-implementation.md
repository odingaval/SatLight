# ADR-002: Lightning implementation

- **Status:** Proposed
- **Date:** 2026-10-21

## Context
Options are LND, Core Lightning (CLN), or a hosted provider (LNbits, Voltage). The backend is in Go.

## Decision
Use LND with separate invoice-only and payment-only macaroons, accessed over gRPC from Go. Run self-hosted for control of keys; consider a managed host for staging.

## Consequences
Mature Go client and tooling (Polar, lncli). Requires channel and liquidity management. Switching later needs an abstraction layer: define a `LightningClient` interface in `internal/lightning`.
