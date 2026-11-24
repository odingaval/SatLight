# Fraud and Abuse Prevention

## Threats
Wash bounties (maintainer pays own sockpuppet to farm reputation), spam PRs, claim squatting, fake merges, payout address hijacking, multi-account abuse.

## Controls
| Control | Detail |
|---|---|
| Account minimums | GitHub account age (default 30 days) and some public activity to claim bounties over 20,000 sats |
| Caps | Daily payout cap for new contributors (default 100,000 sats) and per project per day |
| Self-dealing checks | Block payout if merger equals author without another write-access collaborator; flag reciprocal payments |
| Claim limits | Max 3 active claims per contributor; 7-day expiry |
| Address changes | Re-auth, email confirmation, 48-hour cooldown before it applies to payouts |
| Review queue | Payouts over 100,000 sats or flagged patterns are held for manual review |
| Freeze | Admins can freeze a user, project, or all payouts |
| Rate limits | Per IP and user on claims, invoices, and API |

## Review process
1. Automated flags create a case with evidence.
2. Staff reviews within 2 business days.
3. Outcomes: clear, hold, forfeit payout (funds return to project), ban.
4. Decisions are logged; users may appeal via support.

## Metrics
Flag rate, false-positive rate, time to review, funds held.

## Implementation notes (fraud controls v1)
Implemented: frozen/banned enforcement, owner self-claim block, minimum GitHub account age (applies to all claims when enabled, default off), max active claims per contributor (default 3), reciprocal-pattern flags in reconciliation, per-project trailing-24h payout cap (default 1M sats).
Implemented: payout address changes require format validation, a fresh sign-in (15 minutes), and email confirmation before the 48-hour cooldown starts.
Implemented: per-IP/per-user token-bucket rate limits on claims, deposit invoices, webhooks, login, and waitlist (429 with Retry-After, configurable via RATE_LIMIT_*).
Not yet implemented: public-activity checks, new-contributor caps, manual review queue UI. These remain required before public launch.
