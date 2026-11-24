# Draft issues for Satlight fixes

Remote: `https://github.com/odingaval/SatLight.git`

Each file in this directory is one issue draft: title (first heading), suggested
labels, problem, evidence, and acceptance criteria. They were derived from the
docs (`docs/SATLIGHT-DOCS.md`, specs, ADRs, M2 guide, testing strategy) and a
code read of `internal/`, `cmd/`, `web/`, `migrations/`, and compose files.

Already fixed and verified in the working tree (`make test`,
`make test-int`, `make test-ln` green); skip filing those: 001, 002, 003,
004, 005, 006, 007, 008, 009, 010, 011, 012, 014, 017, 018, 025, 027.
Partially done (file with a note, or finish first): 023 (alert rules and
settlement-lag metrics shipped; dashboards and remaining series outstanding),
026 (ledger fuzz seed shipped; chaos harness and Playwright E2E outstanding).
Still open: 013, 015, 016, 019, 020, 021 (legal), 022 (legal), 024 (product
decision needed).

File them with (requires `gh` auth against the repo above):

```bash
for f in docs/issues/0*.md; do
  title="$(head -n1 "$f" | sed 's/^# //')"
  labels="$(grep '^Labels:' "$f" | sed 's/^Labels: //')"
  body="$(tail -n +3 "$f")"
  gh issue create --title "$title" --label "$labels" --body "$body"
done
```

Or file individually, e.g.:

```bash
gh issue create --title "$(head -n1 docs/issues/001-lnd-payment-macaroon-unused.md | sed 's/^# //')" \
  --label "bug,security" --body-file docs/issues/001-lnd-payment-macaroon-unused.md
```

| # | Issue | Severity |
|---|---|---|
| 001 | LND payments use the invoice macaroon; payment macaroon discarded | Critical |
| 002 | LND regtest harness missing; `make test-ln` runs zero tests | Critical |
| 003 | API rate limiting missing (429 documented but unenforced) | Critical |
| 004 | LNURL-pay callback URL is fetched without SSRF validation | Critical |
| 005 | Self-merged payouts auto-release via claim link instead of manual hold | Critical |
| 006 | Claim links never expire; unclaimed funds never return | High |
| 007 | Destination change lacks validation, re-auth, email confirmation | High |
| 008 | Crash between LND AddInvoice and deposit persistence loses mapping | High |
| 009 | Settlement has no poll backup; keysend HTLCs unobserved | High |
| 010 | Kill-switch split (env vs DB flags); no two-person resume or paging | High |
| 011 | Payment status/fee read from first TrackPaymentV2 message | High |
| 012 | Force-revert grace window not implemented; payout is immediate | High |
| 013 | Maintainer project bounties and paid-out totals missing from API | Medium |
| 014 | No QR rendering for funding invoices | Medium |
| 015 | Notification coverage gaps (deposit/claim/PR/dispute/low-balance) | Medium |
| 016 | Fraud controls v2 outstanding | Medium |
| 017 | No CI workflow; `make openapi` points at the wrong path | Medium |
| 018 | Browse filters: sort ignored, language is a substring match | Medium |
| 019 | GitHub polish: suggest-claim comment, ETag caching, backoff | Medium |
| 020 | Unmatched-receipt review tooling and late-credit alerts | Medium |
| 021 | Withdrawals legal gate tracker (counsel decision, Option A/B) | Blocker |
| 022 | Legal docs sign-off, SECURITY placeholders, M1 audit | Blocker |
| 023 | Monitoring gaps: metrics, alerts, dashboards | Medium |
| 024 | Fee/routing-cost policy inconsistency (product decision) | Medium |
| 025 | Stale docs: ADR index, INDEX paths, limitations, spec notes | Low |
| 026 | Test coverage gaps: fuzz, chaos, E2E, cursor/filter DB tests | Medium |
| 027 | Dev secrets and startup hardening (admin macaroon, panic) | Medium |
