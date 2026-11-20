# Notification coverage gaps (deposit/claim/PR/dispute/low-balance)

Labels: feature

Priority: Medium (users blind to money-state changes).

## Problem

Only four emails exist (payout settled, claim link, merge reverted, destination
changed). The notifications spec lists eighteen events; deposit settled, bounty
claimed, claim expiring/expired, PR linked, payout started/retrying, disputes,
and low wallet balance are all missing. There are no mute preferences, no
in-app surface, SMTP has no auth (local relay only), and amounts show sats
without the required fiat second.

## Evidence

- Implemented senders in [service.go](/home/bocal/SatLight/internal/notify/service.go:107);
  full matrix in [notifications.md](/home/bocal/SatLight/docs/specs/notifications.md:3).
- `sendMail` uses `smtp.SendMail` with nil auth
  ([service.go](/home/bocal/SatLight/internal/notify/service.go:47)).

## Acceptance criteria

- [ ] Missing money-critical events sent (deposit, claim lifecycle, PR linked,
      payout started/retrying, disputes, low balance) with sats-first copy.
- [ ] Mute preferences for non-critical mail; payout/security mail unmutable.
- [ ] Authenticated SMTP (or provider) configured; secrets out of git.
