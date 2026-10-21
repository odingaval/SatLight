# ADR-010: Fee and routing-fee mechanics

- **Status:** Proposed
- **Date:** 2026-10-21

## Context
The proposal states a 2% fee deducted from the funded reward. Routing fees on payouts must also be borne by someone.

## Decision
Deduct a 2% platform fee (rounded down, minimum 1 sat) from the reward. Contributor receives reward minus fee. The platform pays routing fees from fee revenue, capped by max fee limits.

## Consequences
Contributors know exactly what they receive. Fee revenue must cover routing costs; see `docs/business/economics-model.md`.
