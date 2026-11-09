# Roadmap

| Milestone | Goal | Key deliverables | Status |
|---|---|---|---|
| M1 | Foundations | Landing page, project registration, contributor waitlist, PRD, architecture, threat model, legal review started | In progress (audit outstanding) |
| M2 | Funding | Bounty creation, project wallet funding (Lightning invoices), ledger, state machine, ToS/Privacy ([completion guide](M2_COMPLETION_GUIDE.md)) | In progress |
| M3 | GitHub | GitHub App, claim flow, PR linking, merge verification, fraud controls v1 | Implemented (code + integration tests; LND regtest harness and public-activity fraud checks outstanding) |
| M4 | Payouts and beta | Automated Lightning payouts, retries and claim links, monitoring, closed beta with 5 projects, 100 funded bounties | Implemented (code + integration tests; withdrawals stay 501-gated and funding/outflows paused until legal/ops gates clear; closed beta not started) |

## After beta
- Public launch, analytics for maintainers, reputation system, organization dashboards
- Non-custodial escrow research
- GitLab/Gitea support, localization

## Exit criteria for public launch
Payout success rate above 98%, zero unreconciled funds, legal sign-off, completed security review.
