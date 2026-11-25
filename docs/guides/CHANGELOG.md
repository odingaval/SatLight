# Changelog

All notable changes are documented here. Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versioning: [Semantic Versioning](https://semver.org/).

## [Unreleased]
### Added
- Initial documentation set, OpenAPI draft, database schema, ADRs.
- Single-origin deployment: the API serves the embedded Vite build with an SPA fallback when `SERVE_STATIC=true`, so one URL hosts the UI and `/api/*` and session cookies stay same-origin (off by default; API 404s unchanged).
- Render blueprint (`render.yaml`): Docker web service + background worker + managed Postgres with pre-deploy migrations; `BASE_URL`/`WEB_URL` default to `RENDER_EXTERNAL_URL` when unset.
- Dockerfile now builds the web UI into the image and ships the `satlight-migrate` binary.
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
- Payment tracking waits for a terminal LND update (bounded 30s) instead of reading the first `TrackPaymentV2` message, so settled payouts book the final routing fee; inconclusive lookups after a successful send settle with a zero fee for reconciliation to true up rather than booking a partial in-flight fee.
- Payment sends are bounded client-side (90s): LND's `TimeoutSeconds` does not end the `SendPaymentV2` stream when the only peer disconnects (the regtest suite caught a 10-minute wedged stream), so `PayInvoice` now reports `ErrPaymentTimedOut` and both payout paths verify the outcome by hash — succeeded payments settle, in-flight or unverifiable ones retry without burning an attempt, and only a definitive `FAILED` joins the normal failure path, so a claim link is never issued while a payment could still land.
- Claim links expire after 30 days by default (`CLAIM_LINK_EXPIRY_DAYS`): redemption rejects expired links, and a worker returns the reserved reward to the project wallet with balanced ledger entries, an audit record, and a contributor notice.
- Funding invoices carry a machine-readable project memo, and an hourly worker sweep (also run at startup) re-attaches settled node invoices that lost their deposit rows, crediting them through normal settlement and closing any quarantined receipt for the hash.
- Deposit settlement has a poll backup: an hourly job looks every pending deposit up at the node (jittered, batched, with settlement-lag metrics), crediting stream-missed settlements and expiring node-cancelled invoices; settled arrivals without attribution (keysend, legacy memos) are quarantined for staff review instead of going unobserved.
- Automatic payouts wait out a 10-minute force-revert grace window after the merge (`PAYOUT_MERGE_GRACE_MINUTES`, 0 disables); jobs inside the window reschedule for its end and the merge is re-verified just before sending, so a revert pushed minutes after merge holds the payout instead of racing it.
- Kill switch reconciled: outflows stop when the `PAYOUTS_PAUSED` env override OR the database flag says so (previously the env flag was read nowhere); database flags clear only through an audited two-person resume (`cmd/opsctl` propose/approve), `GET /api/v1/status` feeds a frontend pause banner, and loadable Prometheus alert rules ship in `monitoring/alerts.yml` (Alertmanager receiver and game-day drill remain team TODOs).
- LND regtest harness: `make test-ln` boots bitcoind + 2 LND nodes, funds and channels them, and proves deposit settlement, successful pays with fee accounting, no-route/offline/expiry/duplicate failures, and post-restart hash recovery against real nodes; runs nightly and on payout-code changes via CI. The harness caught two live send-path bugs: invoice decoding now runs on the payment connection (the invoice macaroon lacks `offchain:read`), and sends no longer set `Amt` on fixed-amount invoices (rejected by LND), with zero-amount invoices refused explicitly.
- CI workflows (`.github/workflows/ci.yml`, `ln.yml`) run vet, unit + integration tests, OpenAPI validation, and the web build on every push, plus the regtest suite nightly; `make openapi` validates the real spec path with Redocly, and 11 pre-existing spec errors (unquoted descriptions, a dangling `Unauthorized` ref) are fixed.
- Browse filters fixed: `sort=newest|reward` is honored server-side with sort-specific cursors (highest-reward is no longer a page-local client sort), and `language` matches the repository's real primary language from GitHub instead of substring-matching repo names.
- Stale docs refreshed: ADR index statuses, documentation index links, backend limitations, and the ADR-011 implementation notes now match the code.
- Least-privilege LND access: balance reads moved to the payment connection so the invoice macaroon needs only `invoices:read+write`; dev bakes a constrained payment macaroon (`offchain:read+write`, `onchain:read`) via `scripts/dev-macaroon.sh` instead of using admin, dev secrets generate per checkout via `scripts/dev-init.sh`, and the secrets-manager path for staging/prod is documented in the runbook. `queue.NewClient` returns construction errors instead of panicking.
- Funding invoices render a scannable QR code (SVG, `lightning:` URI) above the copyable text for mobile wallets.
- Project registration suggests repositories: right after sign-in the form offers every admin repository across App installations from `GET /api/v1/github/registrable-repositories` (picking one fills the installation ID), with manual entry kept as a fallback.
- Registration has a Connect button that opens the App install page; GitHub redirects back to the site with the installation ID (App Setup URL), which the form auto-fills while narrowing suggestions to the new installation.
- Ledger balance gate gains a fuzz test (`FuzzValidateEntries`) proving it accepts exactly the balanced, positive, overflow-free transactions.
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

### Security
- LND router calls (payment sends, payment tracking) now use the payment macaroon on a separate connection; the invoice macaroon can no longer send funds.
- LNURL-pay endpoints are fetched through an SSRF guard: https-only, no credentials, no private/link-local targets, redirect and DNS-rebinding protection.
- Self-merged payouts are held for manual review with no claim link issued (previously auto-releasable via an emailed link). Manual release tooling is still to come.
- Payout destination changes now require a valid Lightning address, a session signed in within the last 15 minutes, and email confirmation via `POST /me/payout-destination/confirm` (24-hour single-use links, 48-hour payout cooldown from confirmation).
- Abuse-prone endpoints (claims, deposit invoices, GitHub webhooks, login, waitlist) are rate limited per user when signed in and per IP otherwise; denials answer 429 with `Retry-After` per the API contract, and limits are tunable via `RATE_LIMIT_*`.
