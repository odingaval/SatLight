# Contributing to Satlight

Thank you for helping. Satlight pays for merged work through its own bounties, so check the open bounties before starting.

## Before you start
1. Read `README.md` and `SKILLS.md` (rules for money-handling code).
2. Find or open an issue. For bounties, claim it on Satlight first so work is not duplicated.
3. For large changes, open a discussion or ADR proposal before coding.

## Development setup
See "Quick start" in `README.md`. Run `make dev` and `make test` before submitting.

## Branches and commits
- Branch names: `feat/<topic>`, `fix/<topic>`, `docs/<topic>`, `chore/<topic>`.
- Commits follow Conventional Commits: `feat(payout): retry with backoff`.
- Keep PRs small and focused. One concern per PR.

## Pull request checklist
- [ ] Linked issue, with `Closes #N` in the description (required for bounty payout)
- [ ] Tests added or updated, including failure paths
- [ ] `make lint test` passes
- [ ] Docs, `openapi.yaml`, and `CHANGELOG.md` updated if behavior changed
- [ ] No secrets, keys, macaroons, or real invoices in code, tests, or logs
- [ ] Money handled as integer sats

## Review
- First response within 2 business days.
- Ledger, payout, auth, and secrets code needs two approving reviews.
- Maintainers may ask for changes; bounties pay when the PR is merged.

## Bounties
- Rewards are set by maintainers and paid in sats over Lightning after merge.
- Set a Lightning address in your Satlight settings before your PR is merged.
- Claims expire after 7 days without a linked PR. Release a claim if you cannot finish.
- Collusion, self-dealing, or spam PRs forfeit payouts and may lead to a ban (see `docs/specs/fraud-and-abuse.md`).

## Reporting security issues
Do not open a public issue. See `SECURITY.md`.

## Code of conduct
By participating you agree to `CODE_OF_CONDUCT.md`.

## License
Contributions are licensed under the project license (AGPL-3.0, pending confirmation).
