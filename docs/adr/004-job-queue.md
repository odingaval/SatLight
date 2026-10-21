# ADR-004: Job queue

- **Status:** Proposed
- **Date:** 2026-10-21

## Context
Payouts, retries, claim expiry, reconciliation, and emails need durable background jobs.

## Decision
Use a Postgres-backed queue (e.g. River) so jobs can be enqueued in the same transaction as state changes.

## Consequences
No extra infrastructure; transactional enqueue avoids lost jobs. Throughput is sufficient for expected volume.
