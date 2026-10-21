# ADR-003: Primary database

- **Status:** Accepted
- **Date:** 2026-10-21

## Context
The system needs transactions, row locks, unique constraints, and append-only ledger guarantees.

## Decision
Use PostgreSQL 15+ with forward-only SQL migrations.

## Consequences
Strong consistency and constraint support. Single primary; plan backups with point-in-time recovery.
