# Satlight: Complete Project Documentation

> A Bitcoin-native marketplace for open-source contributions.
> This file consolidates every document the project needs, ordered by the milestones (M1-M4) in the proposal.
> Items marked **[ASSUMPTION]** are defaults I chose to close gaps in the proposal. Confirm or change them.

## Table of Contents

1. [Product and Strategy](#1-product-and-strategy)
2. [Architecture and Engineering](#2-architecture-and-engineering)
3. [Security and Risk](#3-security-and-risk)
4. [Legal, Compliance and Policy](#4-legal-compliance-and-policy)
5. [Design](#5-design)
6. [Development Process](#6-development-process)
7. [User-Facing Documentation](#7-user-facing-documentation)
8. [Launch and Community](#8-launch-and-community)
9. [Open Decisions Register](#9-open-decisions-register)

---

# 1. Product and Strategy

## 1.1 Product Requirements Document (PRD) [M1]

### Vision
Every merged pull request deserves instant Bitcoin. Maintainers fund repositories, publish small tasks, and contributors are paid over Lightning within seconds of a merge.

### Users
| User | Goal | Pain today |
|---|---|---|
| Maintainer | Get small tasks done (docs, minor bugs, tests, translations) | Payment overhead exceeds task value; relies on volunteers |
| Contributor | Earn for small verifiable work, build a portfolio | No access to small paid tasks; payment delays and fees |

### Goals (v1)
- Maintainer can register a repository, fund it with Bitcoin, and publish bounties.
- Contributor can browse, claim, submit a PR, and be paid automatically on merge.
- Median time from merge to payout under 30 seconds.

### Non-goals (v1)
- Fiat on/off ramps, credit cards, bank transfers.
- Non-GitHub forges (GitLab, Gitea).
- Large milestone-based contracts or hourly work.
- Native mobile apps.

### Functional requirements
| ID | Requirement | Milestone |
|---|---|---|
| FR-1 | GitHub sign-in for maintainers and contributors | M1/M3 |
| FR-2 | Project registration and repository ownership verification | M1 |
| FR-3 | Contributor waitlist | M1 |
| FR-4 | Project wallet funding via Lightning invoice | M2 |
| FR-5 | Bounty creation (title, description, difficulty, reward, repo/issue link) | M2 |
| FR-6 | Bounty browsing, filtering, and search | M2 |
| FR-7 | Claim a bounty (with claim expiry) | M3 |
| FR-8 | Link a PR to a bounty and verify merge | M3 |
| FR-9 | Automatic Lightning payout after merge | M4 |
| FR-10 | 2% platform fee deducted from the reward | M4 |
| FR-11 | Refund of unspent or expired bounty funds | M2/M4 |
| FR-12 | Contributor payout destination setup (Lightning address / LNURL / node pubkey) | M3 |
| FR-13 | Notifications (email/webhook) for claim, merge, payout, failure | M4 |

### Non-functional requirements
- **Correctness:** no double payouts; ledger always reconciles with node balance.
- **Availability:** 99.5% for API; payouts may queue but never silently drop.
- **Latency:** payout attempt begins within 10 seconds of verified merge.
- **Privacy:** minimal personal data; no KYC in v1 **[ASSUMPTION, see legal memo 4.3]**.
- **Accessibility:** WCAG 2.1 AA.

### Success metrics
See [1.4](#14-success-metrics-specification-m2).

## 1.2 Personas and Journeys [M1]

**Maintainer (Amara, 34, runs a Lightning library).** Has 10 doc issues she will not pay a freelancer for. Wants: fund once, tag issues, forget about payments.

**Contributor (Diego, 22, Buenos Aires).** Wants to build a portfolio and earn small amounts without a bank account. Wants: clear tasks, fast pay, no negotiation.

### Maintainer journey
1. Sign in with GitHub and register repo (ownership verified via GitHub App install).
2. Deposit sats into the project wallet (QR invoice).
3. Create a bounty (or convert an existing issue).
4. Review a contributor's PR; merge.
5. See the payout confirmation and updated balance.

### Contributor journey
1. Browse bounties; filter by language, difficulty, reward.
2. Sign in with GitHub; set a Lightning address.
3. Claim the bounty.
4. Open a PR referencing the issue (`Closes #123`).
5. PR merged; sats arrive within seconds; public profile updates.

## 1.3 Glossary [M1]

| Term | Definition |
|---|---|
| Sat | 1/100,000,000 of a bitcoin. |
| Lightning | Layer-2 payment network on Bitcoin for instant low-fee payments. |
| Bounty | A funded task with description, difficulty, reward, and repo link. |
| Project wallet | Per-project balance held in platform escrow. |
| Escrow | Funds reserved for a specific bounty until payout or refund. |
| Claim | A contributor's time-limited reservation of a bounty. |
| Payout | Lightning payment of the reward (minus fee) to the contributor. |
| Platform fee | 2% of the reward, retained on completed bounties. |
| Lightning address | Email-like payment identifier (`user@domain`) resolved via LNURL-pay. |
| LNURL-withdraw | Link a recipient can redeem to pull funds; used as payout fallback. |
| Ledger | Double-entry record of every balance movement. |

## 1.4 Success Metrics Specification [M2]

| Metric | Definition | Source |
|---|---|---|
| Funded repository | Repo with at least one confirmed deposit | `deposits` table |
| Published bounty | Bounty that reached state `open` | `bounties` state log |
| Completed contribution | Bounty reaching `paid` | `payouts` table |
| Contributor retention | % of contributors with a paid bounty who complete another within 30 days | derived |
| Average payout time | Mean of `payout.settled_at - pr.merged_at` | events |
| Total BTC distributed | Sum of settled payout amounts (sats) | ledger |
| Payout success rate | Settled payouts / payout attempts on first try | events |

Targets (first 6 months): 5 funded projects, 100 funded bounties before public launch, payout success rate above 98%, median payout time below 30s.

---

# 2. Architecture and Engineering

## 2.1 System Architecture Overview [M1]

```
                        +------------------+
   Browser (web app) -->|   Go API server   |<--- GitHub webhooks
                        +---+---------+----+
                            |         |
                 +----------+         +-----------+
                 v                                v
          PostgreSQL                         Job queue / workers
      (state + ledger)                  (payouts, retries, expiry)
                                                  |
                                                  v
                                       Lightning node (LND/CLN)
                                       invoices, payments, liquidity
```

**Components**
- **Web frontend:** Vite-served TypeScript app with static HTML; use htmx for progressive server-driven interactions where useful.
- **Go API:** REST/JSON, OpenAPI-described; auth, bounty CRUD, webhooks.
- **Workers:** durable job processing for payouts, retries, claim expiry, reconciliation.
- **PostgreSQL:** system of record; ledger entries are append-only.
- **Lightning node:** one node with channels sized for expected payout volume.
- **GitHub integration:** GitHub App (installation tokens + webhooks).

## 2.2 Architecture Decision Records (ADRs) [Ongoing]

Template:
```
# ADR-NNN: Title
Status: Proposed | Accepted | Superseded by ADR-XXX
Date:
Context:
Decision:
Consequences:
```

Initial ADRs to write:

| ADR | Decision | Recommended default |
|---|---|---|
| 001 | Escrow model | **[ASSUMPTION]** Custodial escrow with internal ledger for v1 (simplest; hold invoices have timeout limits unsuited to multi-day bounties). Revisit non-custodial later. |
| 002 | Lightning implementation | LND (mature gRPC API, macaroons) unless team prefers CLN. |
| 003 | Database | PostgreSQL. |
| 004 | Job queue | Postgres-backed queue (e.g. River) to avoid extra infrastructure. |
| 005 | Frontend stack | Pick one and record why. |
| 006 | GitHub integration | GitHub App, not OAuth App. |
| 007 | Payout destination | Lightning address (LNURL-pay) primary; keysend secondary. |
| 008 | Hosting | Single region VPS/cloud with node on dedicated volume; encrypted backups. |

## 2.3 Data Model [M2]

```sql
users(id, github_id, github_login, lightning_address, created_at)
projects(id, owner_user_id, github_installation_id, repo_full_name, status, created_at)
wallets(id, project_id, balance_sats, reserved_sats, updated_at)   -- derived from ledger; cached
deposits(id, project_id, bolt11, payment_hash, amount_sats, status, settled_at)
bounties(id, project_id, github_issue_number, title, description, difficulty,
         reward_sats, fee_sats, state, expires_at, created_at)
claims(id, bounty_id, user_id, status, expires_at, created_at)
pull_requests(id, claim_id, github_pr_number, head_sha, merged, merged_at, merge_commit_sha)
payouts(id, bounty_id, user_id, amount_sats, fee_sats, routing_fee_sats,
        destination, status, attempts, payment_hash, settled_at, last_error)
ledger_entries(id, tx_id, account, direction, amount_sats, ref_type, ref_id, created_at)
webhook_events(id, delivery_id UNIQUE, event, payload, processed_at)
audit_log(id, actor, action, subject, metadata, created_at)
```

Rules:
- `ledger_entries` is append-only; balances are derived.
- `webhook_events.delivery_id` is unique to guarantee idempotency.
- `payouts` has a unique constraint on `bounty_id` to prevent double payment.

## 2.4 Bounty Lifecycle State Machine [M2]

```
draft --fund--> open --claim--> claimed --PR opened--> in_review --merge--> merged --payout ok--> paid
  |               |                |                       |                    |
  |               |                +--claim expires--> open |                    +--payout fails--> payout_failed --retry--> merged
  |               |                                         +--PR closed--> claimed
  +--cancel--> cancelled <--cancel/refund-- open
open/claimed/in_review --maintainer dispute--> disputed --resolve--> (paid | cancelled)
open --expiry--> expired --refund--> cancelled
```

| State | Meaning | Escrow |
|---|---|---|
| draft | Created, unfunded | none |
| open | Funded and claimable | reward reserved |
| claimed | Reserved for one contributor | reserved |
| in_review | PR linked, awaiting merge | reserved |
| merged | PR merged, payout pending | reserved |
| paid | Payout settled | released + fee booked |
| payout_failed | Payment failed, retrying | reserved |
| expired / cancelled | Ended without payout | refunded to wallet |
| disputed | Needs manual resolution | reserved |

Transitions are enforced in a single Go function; every transition writes an audit log entry.

## 2.5 API Specification [M2]

REST/JSON, versioned `/api/v1`, documented in `openapi.yaml`.

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | /bounties | List/filter open bounties | public |
| GET | /bounties/{id} | Bounty detail | public |
| POST | /projects | Register project | maintainer |
| POST | /projects/{id}/deposits | Create funding invoice | maintainer |
| POST | /projects/{id}/bounties | Create bounty | maintainer |
| POST | /bounties/{id}/fund | Reserve wallet funds | maintainer |
| POST | /bounties/{id}/cancel | Cancel and refund | maintainer |
| POST | /bounties/{id}/claim | Claim | contributor |
| DELETE | /bounties/{id}/claim | Release claim | contributor |
| PUT | /me/payout-destination | Set Lightning address | user |
| GET | /me/payouts | Payout history | user |
| POST | /webhooks/github | GitHub events | signature |

Conventions: cursor pagination, `Idempotency-Key` header on all POSTs that move money, error format `{ "error": { "code": "...", "message": "..." } }`, rate limits per IP and per user (return `429` with `Retry-After`).

## 2.6 Lightning Integration Spec [M2-M4]

**Deposits:** create invoice via node; store `payment_hash`; on settled event, credit wallet via ledger transaction. Invoice expiry 1 hour; underpayment ignored, overpayment credited.

**Payouts:**
1. Resolve contributor Lightning address (LNURL-pay) to a BOLT11 invoice for `reward - fee`.
2. Pay with a fee limit (default max 1% or 10 sats, whichever is greater **[ASSUMPTION]**).
3. On success, mark `paid`, book fee, record routing fee.
4. On failure, retry with exponential backoff (1m, 5m, 30m, 2h, 12h); after final failure, generate an LNURL-withdraw/claim link and email the contributor.

**Liquidity:** monitor outbound capacity; alert below 2x the largest 24h payout total; documented rebalancing procedure (see ops runbook).

**Rule:** always persist the payout attempt (with payment hash) before sending, to be safe across crashes.

## 2.7 GitHub Integration Spec [M3]

- **GitHub App permissions:** Issues (read), Pull requests (read), Metadata (read), Contents (read, optional).
- **Webhooks:** `pull_request` (opened, closed, merged), `issues`, `installation`.
- **Verification:** validate `X-Hub-Signature-256`; dedupe on `X-GitHub-Delivery`.
- **Linking PR to bounty:** PR body/commit must contain `Closes #N` (or `Fixes`), and the PR author must hold the active claim.
- **Merge verification:** require `merged == true`; fetch PR via API to confirm (do not trust webhook payload alone); capture `merge_commit_sha`.
- **Identity mapping:** payout goes to the Lightning address of the user whose GitHub ID matches the PR author and claim holder.
- **Repo ownership:** only users with admin access on the installation may register a project.

## 2.8 Ledger and Accounting Design [M2]

Double-entry, in sats, integers only. Accounts: `user:{id}:external`, `project:{id}:available`, `project:{id}:reserved`, `platform:fees`, `platform:routing_costs`, `node:liquidity`.

Example: 100,000 sat bounty paid.
| Step | Debit | Credit | Amount |
|---|---|---|---|
| Deposit | node:liquidity | project:available | 100,000 |
| Reserve | project:available | project:reserved | 100,000 |
| Payout | project:reserved | user:external | 98,000 |
| Fee | project:reserved | platform:fees | 2,000 |

**[ASSUMPTION]** Fee is deducted from the reward (contributor receives 98%), matching the proposal text. Routing fees are paid by the platform out of fee revenue.

**Reconciliation:** hourly job compares sum of ledger liabilities with Lightning node balance (channel + on-chain); any mismatch alerts and pauses payouts.

---

# 3. Security and Risk

## 3.1 Threat Model [M1, revisit each milestone]

| Threat | Impact | Mitigation |
|---|---|---|
| Node key / macaroon theft | Total loss of funds | Encrypted secrets, least-privilege macaroons, restricted network, hardware-backed backups |
| Forged GitHub webhook | Fake merge triggers payout | Signature verification, re-fetch PR from GitHub API |
| Self-merge / collusion (maintainer pays own sockpuppet) | Fee farming, fake reputation | Detect same-person patterns, minimum account age, cap early payouts, flag reciprocal payments |
| Double claim / double payout | Loss | DB unique constraints, idempotency keys, ledger transactions |
| Malicious PR (supply chain) | Harm to repo | Out of scope for platform, advise maintainers to review normally |
| Invoice spam / DoS | Cost, downtime | Rate limits, auth required, invoice expiry |
| Bad-faith maintainer (refuses to merge good work) | Contributor unpaid | Dispute process, reputation signals |
| Phishing for Lightning address change | Payout redirection | Re-auth + cooldown + email confirmation on destination change |
| Account takeover | Funds/identity abuse | GitHub-only auth, optional 2FA enforcement checks |

## 3.2 Key and Secrets Management [M2]
- Node seed: offline backup, never on the application server.
- Macaroons: separate `invoice` (receive) and `payment` (send) macaroons; app uses the minimum needed.
- Secrets in a secrets manager or encrypted env store; never in git; scanned in CI.
- Rotation schedule: GitHub App key and webhook secret every 90 days; documented procedure.
- Hot wallet policy: keep only operational liquidity in the node; sweep excess to cold storage.

## 3.3 Fraud and Abuse Prevention [M3]
- Account age and activity minimums for claims above a reward threshold.
- Daily payout cap per new contributor and per project.
- Flag: same IPs/devices across maintainer and contributor, repeated reciprocal bounties, bounties on trivial self-authored issues.
- Manual review queue; ability to freeze a project or user.

## 3.4 Incident Response Runbook [M4]
1. **Detect:** reconciliation alert, payout failure spike, or user report.
2. **Contain:** flip the global "pause payouts" switch; revoke macaroons if compromise is suspected.
3. **Assess:** identify affected payouts/ledger entries; snapshot DB and logs.
4. **Remediate:** fix, rotate secrets, replay safe payouts after reconciliation.
5. **Communicate:** notify affected users within 24h; public postmortem.
6. **Review:** blameless postmortem; add tests and ADR updates.

## 3.5 Pre-Beta Security Checklist
- [ ] Payout path reviewed by a second engineer and an external reviewer
- [ ] Idempotency tests for every money-moving endpoint
- [ ] Webhook signature and replay tests
- [ ] Secrets scan clean; dependencies audited (`govulncheck`)
- [ ] Rate limiting and auth tests
- [ ] Backup restore drill completed
- [ ] Kill switch tested
- [ ] Reconciliation job verified against testnet

---

# 4. Legal, Compliance and Policy

> These are templates and outlines, not legal advice. **Have a qualified lawyer review all of this, especially 4.3, before M2.** Holding user funds in escrow can trigger money-transmission or similar licensing in many jurisdictions.

## 4.1 Terms of Service (outline) [M2]
1. Acceptance and eligibility
2. Service description (marketplace, escrow, payouts)
3. Accounts and GitHub authorization
4. Funding, escrow, and the 2% fee (disclosed up front)
5. Bounty rules: claims, merge, payout timing
6. Refunds, expiry, and cancellation (see 4.4)
7. Prohibited conduct (fraud, collusion, spam, sanctions evasion)
8. Contributor IP: contributions are governed by the repository's license; platform claims no ownership
9. Bitcoin risks: irreversible payments, volatility, network failure
10. Disclaimers, limitation of liability, indemnity
11. Suspension and termination
12. Governing law and dispute resolution
13. Changes to terms

## 4.2 Privacy Policy (outline) [M1]
- Data collected: GitHub ID/login/email, Lightning address, payment history, IP/logs.
- Purposes: operate the service, prevent fraud, legal compliance.
- Public data: contributor profile, paid bounties (opt-out where feasible).
- Retention, deletion requests, subprocessors, cookies, children, international transfers (GDPR/CCPA rights).
- Contact for privacy requests.

## 4.3 Regulatory Analysis Memo (template) [M1-M2]
Questions for counsel:
1. Does custodial escrow of bitcoin make the platform a money transmitter/VASP in target jurisdictions (US federal + states, EU MiCA, Kenya, Argentina, Brazil, Japan)?
2. Would a non-custodial design (e.g. hold invoices, direct maintainer-to-contributor payment with platform only verifying) avoid licensing?
3. KYC/AML thresholds, sanctions screening, and travel-rule obligations.
4. Tax: platform reporting duties (e.g. 1099-type forms) and VAT/GST on the fee.
5. Entity structure and where to incorporate.

Output of memo: go/no-go on custodial model, required controls, and jurisdiction restrictions.

## 4.4 Dispute and Refund Policy [M2]
- **Expiry:** bounties have an owner-set expiry (default 90 days); unclaimed funds return to the project wallet.
- **Claim timeout:** claims lapse after 7 days without a linked PR (extendable).
- **Rejected PR:** claim returns to `claimed`/`open`; no payout.
- **Merged PR:** payout is automatic; maintainers cannot claw back after merge.
- **Maintainer bad faith:** contributor may open a dispute with evidence; staff review within 7 days; outcomes: pay, cancel, or mutual resolution.
- **Maintainer disappears:** funds remain in escrow for the stated expiry, then refund to the maintainer's withdrawal destination.
- **Withdrawals:** project owners can withdraw unreserved balance at any time, subject to fraud holds.

## 4.5 Tax and Reporting Notes [M4]
Contributors and maintainers are responsible for their own tax obligations. Provide a downloadable payout history (CSV) with timestamps, sat amounts, and the BTC/fiat reference rate at settlement. Have counsel confirm platform-side reporting duties.

## 4.6 Open Source License Decision [M1]
Options: AGPL-3.0 (keeps hosted forks open), MIT/Apache-2.0 (permissive), or source-available. **[ASSUMPTION]** AGPL-3.0 for the platform, with trademarks reserved. Record as ADR-009.

---

# 5. Design

## 5.1 Design System and Brand Guide [M1]
- **Principles:** clear, fast, trustworthy, builder-friendly.
- **Color:** a primary accent (bitcoin-orange family), neutral grays, semantic success/warning/error. Verify 4.5:1 contrast.
- **Typography:** one sans for UI, one monospace for sats/hashes/code.
- **Components:** bounty card, difficulty badge, reward chip (sats + approximate fiat), status timeline, empty states, toasts, QR invoice panel.
- **Tone:** plain, encouraging, no jargon without explanation.

## 5.2 Wireframes and User Flows [M1]
Screens to wireframe:
1. Landing page + waitlist
2. Project registration (GitHub install)
3. Fund wallet (QR + invoice copy)
4. Create bounty form
5. Bounty list with filters
6. Bounty detail with claim button
7. Contributor dashboard (claims, earnings)
8. Maintainer dashboard (wallet, bounties, review queue)
9. Payout receipt/confirmation
10. Settings (payout destination)

Step budget goals: create bounty in 3 steps or fewer; claim in 2 clicks after sign-in; payout requires zero steps after merge.

## 5.3 UX Principles and Accessibility Checklist [M1]
- [ ] Every money amount shows sats first, fiat second
- [ ] Keyboard navigable; visible focus
- [ ] Screen reader labels for status and QR codes (with copyable text fallback)
- [ ] Color is never the only status indicator
- [ ] Mobile-first layout
- [ ] Clear error states for failed payments with next action

## 5.4 Copy and Microcopy Guide [M1]
- Say "sats" with a first-use tooltip: "Sats are small units of Bitcoin (100 million per BTC)."
- Say "Lightning address" and show an example (`you@wallet.com`) with wallet recommendations.
- Explain escrow plainly: "Your funds are reserved for this bounty and paid when the work is merged."
- Error copy pattern: what happened, what it means for funds, what to do next.

---

# 6. Development Process

## 6.1 README Template [M1]
```
# Satlight
One-line pitch.
## Quick start
## Architecture
## Running tests
## Contributing (see CONTRIBUTING.md)
## Security (see SECURITY.md)
## License
```

## 6.2 CONTRIBUTING.md Template [M1]
- Setup, branch naming (`feat/`, `fix/`, `docs/`), Conventional Commits.
- PR checklist: tests added, docs updated, linked issue, no secrets.
- Review SLA: first response within 2 business days.
- The project uses its own bounties; contributors are paid per merged PR.
- Code of conduct and `SECURITY.md` for private vulnerability reports.

## 6.3 Local Development Guide [M1]
- Prereqs: Go 1.22+, Docker, PostgreSQL, Node (for frontend), `just` or `make`.
- Run Bitcoin regtest + two LND nodes (via `docker compose`, or Polar) to open channels and simulate payments.
- Mock GitHub with recorded webhook fixtures and `smee.io`/`ngrok` for live webhook testing.
- `make dev` starts API, workers, DB, and regtest nodes; `make seed` loads sample projects and bounties.
- Environment variables documented in `.env.example` (never commit real values).

## 6.4 Coding Standards and Git Workflow [M1]
- Go: `gofmt`, `golangci-lint`, errors wrapped with context, `context.Context` everywhere, no global state.
- Money: integer sats (`int64`), never floats.
- SQL via migrations only (`goose` or `atlas`); no schema edits by hand.
- Trunk-based with short-lived branches; squash merge; protected `main`; at least one review (two for payout/ledger code).

## 6.5 Testing Strategy [M2]
| Layer | Scope | Tools |
|---|---|---|
| Unit | State machine, fee math, ledger | `go test`, table tests |
| Integration | DB, queue, GitHub webhook handling | testcontainers |
| Lightning | Deposits, payouts, failures on regtest | LND regtest, Polar |
| E2E | Full flow: fund, claim, merge, pay | Playwright + mocked GitHub |
| Property/fuzz | Ledger invariants (sum of debits = credits) | `testing/fuzz` |
| Chaos | Crash mid-payout, node offline, duplicate webhooks | custom harness |

Must-pass invariants: no payout without verified merge; at most one payout per bounty; ledger balances to zero; node balance reconciles.

## 6.6 CI/CD and Deployment [M2]
- CI: lint, vet, unit + integration tests, `govulncheck`, secrets scan, OpenAPI validation, migration dry run.
- Environments: local, staging (testnet/signet), production (mainnet).
- CD: tagged releases; migrations run before deploy; blue/green or rolling; one-click rollback.
- Production deploys of payout code require manual approval.

## 6.7 Monitoring and Observability [M3]
Metrics: payout success rate, payout latency, queue depth, webhook failures, node sync status, channel outbound liquidity, reconciliation drift.
Alerts: payout failure rate above 5% (15m), liquidity below threshold, reconciliation mismatch (critical), webhook signature failures spike.
Tools: structured logs, Prometheus + Grafana, error tracking, uptime checks.

## 6.8 Operations Runbook [M4]
- **Backups:** Postgres PITR; node static channel backups (SCB) after every channel change; quarterly restore drill.
- **Node recovery:** restore from seed + SCB; force-close procedure documented.
- **Liquidity:** how to open channels, rebalance (loop/circular), and when to top up.
- **Pause switch:** how to halt payouts safely and resume.
- **On-call:** rotation, escalation, contact list.

---

# 7. User-Facing Documentation

## 7.1 Maintainer Guide [M2-M3]
1. Install the GitHub App on your repository.
2. Fund your project wallet (scan the QR with any Lightning wallet).
3. Create bounties: write a clear title, acceptance criteria, and difficulty; set a fair reward.
4. Review PRs as usual; merge when satisfied. Payout is automatic.
5. Withdraw unused funds anytime.

Tips: split large work into small tasks; add "good first bounty" labels; reward by effort (see table below).

| Difficulty | Typical task | Suggested reward |
|---|---|---|
| Trivial | Typo, link fix | 500-2,000 sats |
| Easy | Docs section, simple test | 2,000-10,000 sats |
| Medium | Small bug fix, translation | 10,000-50,000 sats |

(Ranges are illustrative; maintainers choose.)

## 7.2 Contributor Guide [M3-M4]
1. Create or choose a Lightning wallet and copy your Lightning address.
2. Sign in with GitHub; add your address under Settings.
3. Claim a bounty (you have 7 days to open a PR).
4. Open a PR with `Closes #N` in the description.
5. When it is merged, sats arrive in seconds.

## 7.3 FAQ and Troubleshooting [M4]
- **My payout failed.** We retry automatically, then email a claim link you can redeem with any Lightning wallet.
- **Which wallets work?** Any wallet with a Lightning address or LNURL-pay support.
- **What are the fees?** 2% platform fee deducted from the reward; you pay no routing fee.
- **My claim expired.** The bounty reopened; you can claim it again if available.
- **The maintainer won't merge.** Open a dispute from the bounty page.
- **Is this taxable?** Likely yes in many countries; export your history and consult local guidance.

## 7.4 Public Roadmap and Changelog [M1+]
Maintain `ROADMAP.md` (M1-M4 status) and `CHANGELOG.md` (Keep a Changelog format, semantic versioning).

---

# 8. Launch and Community

## 8.1 Launch Campaign Plan [M1-M4]
| Phase | Goal | Actions |
|---|---|---|
| Pre-launch (M1) | Waitlists | Landing page, build-in-public posts, outreach to Bitcoin dev communities |
| Seeding (M2-M3) | 5 anchor projects, 100 bounties | Direct outreach, onboarding calls, founder-funded starter bounties |
| Closed beta (M4) | Prove payouts | Public payout feed, contributor stories |
| Public launch | Growth | Case studies, partnerships, conference talks |

Core message: **Every merged pull request deserves instant Bitcoin.**

Channels: X, Nostr, GitHub, Bitcoin dev mailing lists/forums, conferences.

## 8.2 Content Calendar [M1-M4]
Weekly cadence: 2 short posts, 1 educational piece, 1 progress update. Themes: "How Lightning payouts work," "Anatomy of a good bounty," contributor spotlights, maintainer interviews, transparency reports (sats paid).

Seed posts from the proposal:
- "You shouldn't wait weeks to get paid for a twenty-minute contribution. Submit a pull request. Get it merged. Receive sats instantly over Lightning."
- "Every typo fixed. Every test written. Every bug resolved. Open source deserves rewards measured in seconds, not invoices."
- "Bitcoin isn't just digital money. It's infrastructure for rewarding work the internet couldn't economically pay for before."

## 8.3 Beta Onboarding Kit [M4]
- Selection criteria: active repo, responsive maintainer, 10+ suitable small tasks, willingness to fund at least 500,000 sats.
- Beta agreement: fee terms, feedback commitment, permission to publish success stories, risk acknowledgment (early software, funds at risk).
- Onboarding checklist: install app, fund wallet, create first 10 bounties, test payout, join feedback channel.
- Success story template: project, problem, bounties completed, sats distributed, time-to-merge change, quote.

## 8.4 Brand Voice and Messaging Guide [M1]
Voice: direct, practical, optimistic, never hype-y. Avoid price speculation and "get rich" language. Explain Bitcoin concepts in one sentence, link to deeper detail. Pillars: instant, global, fair, open.

## 8.5 Beta Program Plan [M4]
Targets: 5 projects, 100 funded bounties, 50 unique contributors, median payout under 30s. Feedback loops: weekly office hours, in-app feedback, post-payout survey (NPS), bug triage board. Exit criteria for public launch: payout success rate above 98%, zero unreconciled funds, legal sign-off complete.

---

# 9. Open Decisions Register

| # | Decision | Default used in this document | Owner | Needed by |
|---|---|---|---|---|
| 1 | Custodial vs non-custodial escrow | Custodial ledger | Founder + counsel | Before M2 |
| 2 | Payout failure handling | Retry then LNURL-withdraw link | Dev | M4 |
| 3 | Refund policy for abandoned bounties | Expiry (90d) then return to wallet | Product | M2 |
| 4 | Merge disputes | Staff-reviewed dispute process | Product | M3 |
| 5 | Fee mechanics | 2% deducted from reward; platform pays routing | Founder | M2 |
| 6 | KYC requirements | None in v1 pending counsel | Counsel | M2 |
| 7 | Platform license | AGPL-3.0 | Founder | M1 |
| 8 | Lightning implementation | LND | Dev | M1 |
| 9 | Frontend stack | TBD | Dev + Designer | M1 |
