# ADR-007: Payout destination

- **Status:** Proposed
- **Date:** 2026-10-21

## Context
Contributors need a simple way to receive sats, ideally without running a node.

## Decision
Support Lightning addresses (LNURL-pay) as the primary destination. Add keysend to a node pubkey as secondary. Failed payouts fall back to an LNURL-withdraw claim link.

## Consequences
Works with most custodial and non-custodial wallets. Depends on the recipient's LNURL server being online at payout time.
