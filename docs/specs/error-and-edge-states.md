# UX Spec: Error and Edge States

Pattern for every message: **what happened, what it means for funds, what to do next.**

| State | Where | Message (draft) | Action |
|---|---|---|---|
| Payout failed, retrying | Contributor dashboard | "We could not reach your wallet yet. We will keep trying. Your sats are safe." | Check Lightning address; update |
| Claim link issued | Email, dashboard | "Your {net} sats are ready. Redeem them with any Lightning wallet." | Redeem button |
| Claim expired | Bounty detail | "Your claim expired. The bounty is open again." | Reclaim if available |
| No Lightning address | Claim button | "Add a Lightning address so we can pay you." | Settings link, wallet suggestions |
| Invoice expired | Fund wallet | "This invoice expired. Create a new one." | New invoice |
| Insufficient balance | Create bounty | "Your wallet has {x} sats; this bounty needs {y}." | Fund wallet |
| Wallet empty | Dashboard | Empty state with fund CTA | Fund wallet |
| Dispute open | Bounty detail | "A dispute is open. Funds stay reserved until it is resolved." | View dispute |
| Payouts paused | Global banner | "Payouts are briefly paused for maintenance. Funds are safe and will be sent." | Status page |
| GitHub app uninstalled | Maintainer dashboard | "Reinstall the GitHub App to publish bounties. You can still withdraw funds." | Reinstall, withdraw |
| Rate limited | Any | "Too many requests. Try again in {n} seconds." | Retry |
| Already claimed | Bounty detail | "Someone else claimed this bounty." | Browse similar |
