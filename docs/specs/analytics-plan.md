# Analytics and Tracking Plan

Principle: measure the business, not the person. No third-party ad trackers; prefer privacy-friendly or self-hosted analytics.

| Event | Properties | Purpose |
|---|---|---|
| `waitlist_joined` | role | Demand |
| `project_registered` | none (hashed repo id) | Supply |
| `wallet_funded` | amount bucket | Liquidity |
| `bounty_published` | difficulty, reward bucket | Supply |
| `bounty_claimed` | difficulty | Demand |
| `pr_linked` / `pr_merged` | time since claim | Funnel |
| `payout_settled` | amount bucket, duration | Core promise |
| `payout_failed` | reason code | Reliability |
| `claim_expired` / `dispute_opened` | | Quality |

Metrics derive from the DB per `SATLIGHT-DOCS.md` section 1.4. Funnel: visit, sign-in, claim, PR, merge, paid. Retention: contributors with a second paid bounty within 30 days.

Privacy: no personal data in event properties, IP truncation, honor Do Not Track, document in the privacy policy, allow opt-out.
