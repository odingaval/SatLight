# GitHub Integration Spec

## App configuration
- **Permissions:** Metadata (read), Issues (read), Pull requests (read). Contents (read) only if needed later.
- **Events:** `installation`, `installation_repositories`, `issues`, `pull_request`.
- **Auth:** installation access tokens for API calls; GitHub OAuth via the App for user sign-in.

## Webhook handling
1. Verify `X-Hub-Signature-256` (HMAC-SHA256, constant-time compare). Reject otherwise with 401.
2. Insert into `webhook_events` using `X-GitHub-Delivery` as a unique key. Duplicate: return 200, do nothing.
3. Enqueue processing; respond within 10 seconds.
4. In the worker, re-fetch current PR/issue state from the API. Do not trust the payload for money decisions.

## Linking a PR to a bounty
- PR body or a commit message contains `Closes|Fixes|Resolves #N` where N is the bounty's issue.
- PR author's GitHub ID must equal the active claim holder's GitHub ID.
- Claim must be active (not expired).

## Merge verification
A payout is released only when all are true:
- `merged == true` on a freshly fetched PR, with a `merge_commit_sha`.
- PR targets the repository registered to the bounty, and the base branch is the default branch (configurable).
- PR author equals the claim holder; the claim is `active`.
- Merger is not the PR author (no self-merge) unless the project has another collaborator with write access or an explicit policy exception is recorded.
- The PR was not force-reverted within the grace window (default 10 minutes; configurable).

## Edge cases
| Case | Handling |
|---|---|
| PR opened by someone without a claim | Ignored; comment suggests claiming |
| Two PRs for one claim | First merged valid PR wins |
| PR closed without merge | Claim stays active until expiry |
| Issue closed or repo transferred | Bounty paused; maintainer notified |
| App uninstalled | Projects frozen; wallet withdrawals still allowed |
| Squash or rebase merge | Use PR `merged` flag, not commit ancestry |

## Rate limits
Cache issue/PR reads; use conditional requests (ETag) and back off on `403/429` with `Retry-After`.
