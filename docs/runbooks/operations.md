# Operations Runbook

## Backups
- **PostgreSQL:** continuous WAL archiving (PITR), daily base backup, retain 30 days; encrypted off-site copy.
- **LND:** static channel backup (SCB) after every channel change, encrypted and stored off-host. Seed phrase stored offline in two locations.
- **Secrets:** stored in a secrets manager; recovery procedure documented and tested.
- **Drill:** restore to staging quarterly; record time taken.

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
Set `PAYOUTS_PAUSED=true` (config flag, no deploy required). Deposits and browsing continue; payouts queue. Resume by setting to `false` after reconciliation passes.

## On-call
Primary and secondary rotation; escalation to founder for SEV1; contact list kept in the password manager.
