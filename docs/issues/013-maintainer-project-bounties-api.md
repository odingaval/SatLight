# Maintainer project bounties and paid-out totals missing from API

Labels: feature

Priority: Medium (dashboard shows gaps and scrapes the public feed).

## Problem

The maintainer dashboard cannot list its own bounties or paid-out totals: no
endpoint exposes them, so the UI filters the public feed (`limit=100`, open
only) and renders paid-out as "—". Non-open history, contributors, and working
project scope are all missing.

## Evidence

- [pages.ts](/home/bocal/SatLight/web/src/pages.ts:98) filters the public bounty
  feed for dashboard rows.
- Paid-out cell hardcodes "—" ([pages.ts](/home/bocal/SatLight/web/src/pages.ts:79)).
- [openapi.yaml](/home/bocal/SatLight/docs/specs/openapi.yaml:124) has wallet only.

## Acceptance criteria

- [ ] `GET /projects/{id}/bounties` (owner-authorized, all states, paginated) and
      paid-out totals added to the wallet response or a summary endpoint.
- [ ] OpenAPI, dashboard, and docs updated; dashboard stops scraping the feed.
- [ ] Authz tests: owner sees, others get 403.
