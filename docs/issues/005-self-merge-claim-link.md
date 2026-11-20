# Self-merged payouts auto-release via claim link instead of manual hold

**Status: fixed and verified in working tree (`make test`, `make test-int` green). Do not file. Remaining follow-up: staff release tooling for held payouts.**

Labels: bug,security

Priority: Critical (self-dealing control is bypassed).

## Problem

Self-merged PRs are supposed to be held for manual release, but the payout flow
immediately issues a redeemable LNURL-withdraw claim link and emails it to the
claimant, so a maintainer paying their own sockpuppet still gets paid with no
review step.

## Evidence

- [service.go](/home/bocal/SatLight/internal/payout/service.go:319) calls
  `holdWithClaimLink` for self-merges, which mints a token and emails it
  ([service.go](/home/bocal/SatLight/internal/payout/service.go:542)).
- [github-integration.md](/home/bocal/SatLight/docs/specs/github-integration.md:43)
  says automatic payouts for self-merges are "held for manual release".

## Acceptance criteria

- [ ] Self-merged payouts enter a held state that no automatic path can release.
- [ ] Staff release action is audited; release criteria documented.
- [ ] Tests cover self-merge hold, no-link-issued, and audited release.
