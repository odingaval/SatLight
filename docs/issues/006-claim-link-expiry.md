# Claim links never expire; unclaimed funds never return

Labels: bug

Priority: High (reserved funds strand permanently).

## Problem

The Lightning spec gives claim links a 30-day expiry after which unclaimed funds
return to the project wallet with notice. Nothing implements expiry: payouts in
`claim_link_issued` stay redeemable forever and reserved sats never return.

## Evidence

- [lightning-integration.md](/home/bocal/SatLight/docs/specs/lightning-integration.md:37)
  requires 30-day expiry plus return-to-wallet.
- `RedeemClaimLink` ([service.go](/home/bocal/SatLight/internal/payout/service.go:625))
  checks only status; no expiry column, job, or return flow exists.

## Acceptance criteria

- [ ] Claim links carry an expiry (default 30d, configurable); redemption rejects
      expired links.
- [ ] Expiry worker returns reserved funds to the project wallet with balanced
      ledger entries, audit log, and contributor notice.
- [ ] Tests cover redeem-before-expiry, redeem-after-expiry, and fund return.
