# Operations Runbook

## Backups
- **PostgreSQL:** continuous WAL archiving (PITR), daily base backup, retain 30 days; encrypted off-site copy.
- **LND:** static channel backup (SCB) after every channel change, encrypted and stored off-host. Seed phrase stored offline in two locations.
- **Secrets:** stored in a secrets manager; recovery procedure documented and tested.
- **Drill:** restore to staging quarterly; record time taken.

## Secrets management
- **Development:** `scripts/dev-init.sh` generates random `SESSION_SECRET`/`ENCRYPTION_KEY` into gitignored `.env` (compose refuses to start without them). LND macaroons come from the dev node: default `invoice.macaroon` for receive calls, `scripts/dev-macaroon.sh` bakes the constrained `payment.macaroon` (`offchain:read+write`, `onchain:read`).
- **Staging/production:** all secrets live in the secrets manager, injected as environment at deploy time — never baked into images, never committed. Required: `SESSION_SECRET`, `ENCRYPTION_KEY`, `DATABASE_URL`, `LND_TLS_CERT` + both macaroons, `GITHUB_APP_PRIVATE_KEY_PATH` + webhook/OAuth secrets, `SMTP_URL`, `SENTRY_DSN`.
- **Rotation:** session/encryption keys rotate by dual-deploy (new value accepts old sessions until expiry); macaroons re-bake per node with least privilege and restart API/worker; database credentials rotate with a staged rollout. Record every rotation date and operator here.
- **Recovery:** secrets-manager break-glass procedure is tested with the quarterly restore drill; loss of the LND seed is unrecoverable — it stays offline in two locations per Backups above.

## Node recovery
1. Provision host; restore seed and SCB.
2. Start LND in recovery mode; request force-close of channels via SCB if needed.
3. Verify on-chain balance and pending sweeps.
4. Reconcile with ledger before resuming payouts.

## Liquidity management
- Dashboard: outbound/inbound capacity per channel.
- Targets: outbound >= 2x largest 24h payout total; inbound sized to expected deposits.
- Opening channels: choose well-connected peers; keep some channels private-peering options documented.
- Rebalance with circular payments or swaps; log all actions.
- On-chain reserve for channel opens and fee bumps.

## Routine tasks
| Frequency | Task |
|---|---|
| Daily | Review payout failures, reconciliation results, queue depth |
| Weekly | Liquidity review, dependency updates, backup verification |
| Monthly | Access review, secrets rotation check, cost review |
| Quarterly | Restore drill, incident game day, threat model review |

## Kill switch
Two switches guard outflows, and EITHER ONE pauses them:
- `PAYOUTS_PAUSED=true` (env): the emergency override. It needs no database access, so it works when the DB is sick — but lifting it requires a process restart.
- `outflows_paused` / `funding_paused` (database `system_flags`): the granular, instant switches. Reconciliation sets both automatically on critical findings. They clear ONLY through the two-person resume below — never by hand-editing the row, and never automatically.

Fresh installs boot paused (`launch legal and operational review required`) until counsel and operations approve launch.

To pause now: `UPDATE system_flags SET enabled = TRUE, reason = '<why>', changed_by = '<you>' WHERE name IN ('funding_paused','outflows_paused');` (or set the env override and restart). Deposits already in flight still credit; browsing continues; payouts stop before sending. `GET /api/v1/status` reports the effective state and the frontend shows a banner while paused.

To resume (two people, no exceptions):
1. Confirm reconciliation is green and the incident is understood.
2. Operator A: `go run ./cmd/opsctl propose --flag outflows_paused --as alice@example.com --reason "..."` (repeat for `funding_paused`).
3. Operator B (a different human): `go run ./cmd/opsctl approve --id <proposal> --as bob@example.com`.
4. If the env override is set, unset it and restart the API/worker too — the database resume alone will not lift it.
5. Watch `/metrics` and the next reconciliation run before declaring the incident over.

Every proposal, approval, and rejection lands in `audit_log` (`flag.resume_*`).

## Alerting
Prometheus rules live in `monitoring/alerts.yml` (critical: reconciliation findings, ledger drift; warning: payout hold rate, deposit settlement lag). TEAM TODO: wire the Alertmanager receiver to the paging provider and record the game-day drill below.

## On-call
Primary and secondary rotation; escalation to founder for SEV1; contact list kept in the password manager.

## Game-day drill (TEAM TODO)
Run the kill-switch drill quarterly and record the date, participants, pause/resume timings, and lessons here. Drill script: pause outflows via SQL, confirm `/api/v1/status` flips and the banner appears, confirm a test payout does not send, run the two-person resume, confirm payouts flow. Last run: _not yet run_.
