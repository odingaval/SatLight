# API rate limiting missing (429 documented but unenforced)

Labels: security,bug

Priority: Critical (invoice spam / DoS / claim abuse).

## Problem

No rate-limit middleware exists, but the API contract documents `429` responses,
the frontend handles 429s, and the fraud spec requires per-IP and per-user
limits on claims, invoices, and the API. Invoice creation and claim endpoints
are exposed to spam without throttling.

## Evidence

- No middleware in [server.go](/home/bocal/SatLight/internal/api/server.go:185);
  `RateLimit` appears only in OpenAPI response docs.
- [openapi.yaml](/home/bocal/SatLight/docs/specs/openapi.yaml:38) documents `RateLimited`.
- [fraud-and-abuse.md](/home/bocal/SatLight/docs/specs/fraud-and-abuse.md:16)
  requires rate limits; implementation notes list it as not implemented.
- [BACKEND_IMPLEMENTATION.md](/home/bocal/SatLight/docs/guides/BACKEND_IMPLEMENTATION.md:584)
  notes it as scaffolded-but-missing.

## Acceptance criteria

- [ ] Per-IP and per-user limits on claims, deposit invoices, webhooks, and login;
      `429` with `Retry-After` per the contract.
- [ ] Limits are configuration, with safe test/dev defaults.
- [ ] Tests cover limit, burst, key partitioning, and `Retry-After` headers.
