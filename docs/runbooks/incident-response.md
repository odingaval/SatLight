# Incident Response Runbook

## Severity
| Level | Definition | Example | Response |
|---|---|---|---|
| SEV1 | Funds at risk or lost, key compromise, double payouts | Reconciliation mismatch, leaked macaroon | Immediate, all hands |
| SEV2 | Payouts failing broadly, data exposure | Node offline, webhook outage | Within 30 minutes |
| SEV3 | Degraded feature | Slow API | Business hours |

## First 15 minutes (SEV1/2)
1. **Declare** the incident in the on-call channel; assign an incident lead and a scribe.
2. **Contain:** set `PAYOUTS_PAUSED=true` (kill switch). If key compromise is suspected: revoke macaroons, rotate GitHub App key and webhook secret, block egress, and snapshot the host.
3. **Preserve evidence:** snapshot DB, copy logs, record timeline.
4. **Assess:** which payouts, deposits, and ledger entries are affected? Run reconciliation.

## Remediation
- Fix root cause in a branch with tests; two-reviewer approval.
- Reverse incorrect ledger entries with compensating transactions (never edit).
- Replay or re-issue payouts only after reconciliation passes.
- Resume payouts gradually (small amounts first), watching metrics.

## Communication
- Internal updates every 30 minutes.
- Notify affected users within 24 hours: what happened, what it means for funds, what we are doing.
- Public postmortem within 7 days for SEV1/SEV2.
- Legal counsel and, if required, regulators and affected-user notices per law.

## Postmortem template
Summary, impact, timeline, root cause, what went well, what went poorly, action items with owners and dates. Blameless.
