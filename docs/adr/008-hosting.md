# ADR-008: Hosting and infrastructure

- **Status:** Open
- **Date:** 2026-10-21

## Context
The Lightning node needs stable storage, backups, and restricted network access.

## Decision
TBD. Recommended: single region cloud/VPS, node on an encrypted dedicated volume, private networking between API, DB, and node, managed Postgres with PITR.

## Consequences
Start simple; document upgrade path to multi-region before public launch.
