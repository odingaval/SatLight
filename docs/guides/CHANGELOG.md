# Changelog

All notable changes are documented here. Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versioning: [Semantic Versioning](https://semver.org/).

## [Unreleased]
### Added
- Initial documentation set, OpenAPI draft, database schema, ADRs.
- Idempotent bounty creation and claims: `Idempotency-Key` is now enforced, retries replay the original result, and key reuse with different data returns HTTP 409.
- Dispute lifecycle: opening a dispute moves the bounty to `disputed`; project owners can resolve via `POST /disputes/{id}/resolve` (`resolved_pay` requires a verified merge, `resolved_cancel` refunds, `resolved_other` restores the pre-dispute state).
- River queue schema migrations are now applied by `cmd/migrate` (previously missing, which broke bounty creation and claims).
- Integration coverage for idempotent bounty/claim/dispute flows and concurrent funding.

### Fixed
- Project registration no longer fails on a response scan mismatch.
- `GET /me/payouts` no longer queries a non-existent column.
- Claim without a payout destination returns HTTP 422 instead of failing on a NULL scan.
- Duplicate-issue bounties return HTTP 409 instead of HTTP 500.
- `make migrate-down` now rolls back instead of migrating up.
- Removed the raw `./migrations` postgres init mount that applied SQL outside versioning.
- `make seed` writes ledger-backed demo data (deposit + reservation entries) and is safe to rerun.
- CORS now allows the configured web origin instead of `*`.
- The API server no longer panics on startup (the insert-only River client now registers the required worker set).
- All bounty state changes go through a single audited transition function enforcing the lifecycle table.
- GitHub webhooks are recorded and processed asynchronously with retries; merge state is always re-fetched from the GitHub API, never trusted from the payload.
- PR opened/push moves bounties to `in_review`; unmerged close reverts to `claimed`; foreign-author PRs are audited and ignored.
- GitHub App removal/suspension freezes projects (escrow kept, money movement blocked); unsuspend reactivates. Repository removal freezes the project; deleted issues with active bounties are flagged.
- Fraud controls v1: banned users, frozen projects, owner self-claims, minimum GitHub account age, and max active claims per contributor block claims; reciprocal-payment patterns are flagged in reconciliation; per-project trailing-24h payout caps are enforced by the payout flow.
- Merges require the PR base to match the repository default branch; self-merges are flagged for review and held from automatic payout.
- Automated Lightning payouts: merged bounties pay the claimant's Lightning address (LNURL-pay) with fee limits, pay-time merge re-verification, per-project daily caps, and persist-before-pay crash recovery that always re-checks the stored payment hash before resending.
- Failed payouts retry on a 1m/5m/30m/2h/12h schedule, then fall back to single-use LNURL-withdraw claim links served by `GET /payouts/claim/{token}` and redeemed via `GET /payouts/claim/callback`.
- Email notifications for payouts, claim links, reverted merges, and destination changes, plus signed maintainer webhooks (`PUT /projects/{id}/notifications`) for claim/merge/payout events.
- New destinations wait out a 48-hour cooldown before automatic payouts resume.
- Prometheus metrics at `/metrics` (payouts, reconciliation drift, webhooks); reconciliation reports unmatched receipts and fraud flags, and runs on Lightning reconnect.
- Fixed ledger payout entries to balance when routing fees are non-zero, and fixed `ErrNoRows` comparisons that misreported missing rows across the API.
- `GET /me/claims` lists the signed-in contributor's active claims; the frontend gains `/my-work` (claims and payouts) and `/settings` (Lightning destination) pages.
- API handler integration tests cover project authz, bounty idempotency/402/409 paths, claim/release, dispute resolve, webhook dedupe and signature rejection.
- Documented the outstanding LND regtest harness in the testing strategy (`make test-ln` currently runs zero tests).
