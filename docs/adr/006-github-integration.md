# ADR-006: GitHub App instead of OAuth App

- **Status:** Accepted
- **Date:** 2026-10-21

## Context
Satlight must read issues and PRs, receive webhooks, and verify repository admin access.

## Decision
Use a GitHub App with fine-grained permissions (Issues read, Pull requests read, Metadata read) and installation tokens. Use GitHub OAuth (via the App) for user sign-in.

## Consequences
Least privilege and per-repo installation. Webhook secret and App private key become critical secrets.
