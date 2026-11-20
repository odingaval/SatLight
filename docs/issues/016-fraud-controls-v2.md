# Fraud controls v2 outstanding

Labels: feature,security

Priority: Medium (required before public launch).

## Problem

Fraud v1 shipped (bans, freezes, self-claim block, account age, claim limits,
reciprocal flags, project caps), but the spec still requires public-activity
checks, new-contributor payout caps, a manual review queue UI, and the full
address-change confirmation flow.

## Evidence

- [fraud-and-abuse.md](/home/bocal/SatLight/docs/specs/fraud-and-abuse.md:27)
  implementation notes list the missing controls.

## Acceptance criteria

- [ ] Public-activity check for large claims; new-contributor daily caps.
- [ ] Manual review queue UI (hold/clear/forfeit/ban with audit + appeal path).
- [ ] Review-hold threshold payouts (>100k sats or flagged) actually held.
- [ ] Metrics: flag rate, false positives, time-to-review, funds held.
