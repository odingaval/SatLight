# Release Process

## Versioning
Semantic Versioning (`MAJOR.MINOR.PATCH`). Pre-1.0: minor versions may include breaking changes, documented in `CHANGELOG.md`.

## Checklist
- [ ] All CI checks green on `main` (lint, tests, govulncheck, secrets scan, OpenAPI validation)
- [ ] Migrations reviewed; tested up on a production-like copy; rollback plan written
- [ ] `CHANGELOG.md` updated; docs and `openapi.yaml` current
- [ ] Staging (signet/testnet) deployment verified, including a full fund-claim-merge-payout flow
- [ ] For changes touching ledger, payout, or auth: second reviewer sign-off and extra test evidence
- [ ] Tag `vX.Y.Z`; build artifacts; deploy staging then production with manual approval
- [ ] Post-deploy checks: health, payout success rate, queue depth, reconciliation
- [ ] Announce in changelog and community channels

## Rollback
Redeploy the previous tag. Migrations are forward-only; ship backward-compatible schema changes (expand, migrate, contract) so rollback is safe. For payout bugs, set `PAYOUTS_PAUSED=true` first.

## Hotfixes
Branch from the release tag, fix, test, review, tag a patch release.
