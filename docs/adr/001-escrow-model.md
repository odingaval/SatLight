# ADR-001: Escrow model

- **Status:** Proposed (needs legal review)
- **Date:** 2026-10-21

## Context
Maintainers fund bounties and contributors are paid on merge. Escrow can be custodial (platform holds sats and a ledger tracks balances) or non-custodial (e.g. Lightning hold invoices, or direct maintainer-to-contributor payment). Hold invoices expire in days or weeks, which does not fit bounties that live for up to 90 days.

## Decision
Use custodial escrow with an internal double-entry ledger for v1. Funds are received into the platform Lightning node, reserved per bounty, and released on verified merge. Revisit a non-custodial design after beta.

## Consequences
Simplest UX and reliable payout timing. Platform holds user funds, which creates security, accounting, and regulatory exposure (see `docs/legal/regulatory-memo.md`). Requires strong key management, reconciliation, and a kill switch. Lawyer sign-off is required before M2.
