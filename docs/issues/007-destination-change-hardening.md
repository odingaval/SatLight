# Destination change lacks validation, re-auth, and email confirmation

**Status: fixed and verified in working tree (`make test`, `make test-int` green). Do not file.**

Labels: security,bug

Priority: High (payout redirection after account takeover).

## Problem

`PUT /me/payout-destination` stores any string with no Lightning-address format
check, no re-authentication, and no email confirmation. The fraud spec requires
re-auth plus email confirmation plus a 48-hour cooldown; only the cooldown is
enforced (at payout time). A hijacked session can silently redirect payouts.

## Evidence

- [server.go](/home/bocal/SatLight/internal/api/server.go:969) updates the address
  unconditionally and only sends an informational email.
- [fraud-and-abuse.md](/home/bocal/SatLight/docs/specs/fraud-and-abuse.md:13)
  requires re-auth, email confirmation, 48h cooldown.
- Threat model (`docs/SATLIGHT-DOCS.md` 3.1) names payout redirection explicitly.

## Acceptance criteria

- [ ] Address format validated (Lightning address / LNURL / supported forms).
- [ ] Change requires fresh re-auth and verified email confirmation before use.
- [ ] Cooldown kept; confirmation and abuse-rendering emails sent; tests cover
      invalid, unconfirmed, and confirmed flows.
