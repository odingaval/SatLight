# Disaster Recovery Plan

| Objective | Target |
|---|---|
| RPO (data loss) | 5 minutes (WAL archiving) |
| RTO (API) | 4 hours |
| RTO (payouts, after node recovery and reconciliation) | 24 hours |

## Scenarios
| Scenario | Plan |
|---|---|
| Database loss | Restore from latest base backup + WAL to point before failure; replay; reconcile with node |
| Lightning node host loss | Restore seed + SCB on new host; recover channels (force-close); sweep funds; reconcile |
| Cloud region outage | Redeploy API and workers in alternate region from IaC; restore DB; reconnect node |
| Key compromise | Follow incident-response; migrate funds to new node/seed; rotate all secrets |
| GitHub outage | Queue webhooks; payouts wait for verification; status banner |
| Operator unavailable | Documented break-glass procedure with two trusted holders of recovery material [define] |

## Requirements
- Infrastructure as code for repeatable rebuilds.
- Quarterly DR test with timing recorded.
- Runbook access independent of the primary infrastructure.
