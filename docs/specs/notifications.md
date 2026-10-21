# Notifications Spec

| Event | Recipient | Channel | Subject / summary |
|---|---|---|---|
| Waitlist joined | Subscriber | Email | Welcome to Satlight |
| Deposit settled | Maintainer | Email, in-app | Your wallet was funded: {amount} sats |
| Bounty published | Maintainer | In-app | Bounty is live |
| Bounty claimed | Maintainer | Email, in-app | {user} claimed "{title}" |
| Claim expiring (24h) | Contributor | Email | Your claim expires tomorrow |
| Claim expired | Contributor | Email | Claim released |
| PR linked | Maintainer | Email | PR #{n} linked to "{title}" |
| PR merged, payout started | Contributor | Email | Payout in progress |
| Payout settled | Contributor, Maintainer | Email, in-app | You received {net} sats for "{title}" |
| Payout failed (retrying) | Contributor | Email (after 2nd failure) | We are retrying your payout |
| Claim link issued | Contributor | Email | Redeem your {net} sats |
| Dispute opened / resolved | Both | Email | Dispute update |
| Low wallet balance | Maintainer | Email | Wallet below {threshold} |
| Destination changed | Contributor | Email | Payout address changed (confirm) |

## Rules
- Plain-language copy: what happened, what it means for funds, what to do next.
- Show sats first, approximate fiat second.
- Never include full invoices, tokens, or secrets in emails (claim links are single-use and expire).
- Users can mute non-critical notifications; security and payout emails cannot be muted.
- Optional outgoing webhooks for maintainers (`bounty.claimed`, `bounty.paid`) signed with HMAC.
