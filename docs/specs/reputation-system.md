# Contributor Reputation Spec (post-beta, premium feature)

## Goals
Help maintainers trust contributors and help new contributors build a verifiable record, without making the platform easy to farm.

## Signals
| Signal | Weight (initial) | Notes |
|---|---|---|
| Completed bounties (paid) | High | Diminishing returns per project |
| Unique projects paid by | High | Reduces single-maintainer farming |
| Merge rate on claims | Medium | Claims abandoned reduce score |
| Maintainer ratings (optional) | Medium | One rating per paid bounty |
| Account age and GitHub history | Low | Helps cold start |
| Disputes lost | Negative | |
| Fraud flags | Strong negative | |

## Outputs
- Public profile: bounties completed, total sats earned, projects, languages (opt-out available).
- Tiers (e.g. New, Trusted, Expert) used for eligibility, such as higher-reward bounty claims or fewer review checks.
- Maintainers can require a minimum tier per bounty.

## Anti-gaming
- Ignore reciprocal payments between the same two accounts above a threshold.
- Cap reputation gained from one project per time window.
- Recompute nightly; keep an audit trail of score changes.

## Privacy
Score derives only from platform activity and public GitHub data. Provide data export and deletion.
