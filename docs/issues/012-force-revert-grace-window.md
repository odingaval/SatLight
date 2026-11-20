# Force-revert grace window not implemented; payout is immediate

Labels: bug

Priority: High (merge-then-revert can still pay).

## Problem

The GitHub spec requires a 10-minute force-revert grace window, but payouts run
immediately after merge with only an instant re-verification, which cannot catch
a revert pushed minutes later.

## Evidence

- [github-integration.md](/home/bocal/SatLight/docs/specs/github-integration.md:25)
  requires the grace window.
- Implementation notes (:42) admit "no artificial delay"; `VerifyMergeFresh` is
  a point check in [service.go](/home/bocal/SatLight/internal/github/service.go:439).

## Acceptance criteria

- [ ] Decide and document: delayed payout vs accept-risk with revert clawback plan.
- [ ] If delayed: payout jobs wait out the window (configurable, default 10m)
      while keeping the sub-30s goal honest in the PRD copy.
- [ ] Tests cover merge-then-revert inside and outside the window.
