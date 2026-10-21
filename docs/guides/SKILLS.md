# SKILLS.md: Development Skills and Working Conventions

Purpose: a single reference for the skills this project requires and the conventions any developer (or AI coding assistant) must follow when working on Satlight. Read `SATLIGHT-DOCS.md` for full specs.

---

## 1. Project context (read first)

- **What:** Bitcoin-native marketplace. Maintainers fund bounties; contributors are paid over Lightning when a GitHub PR is merged.
- **Stack:** Go backend, PostgreSQL, LND (default), GitHub App, lightweight web frontend.
- **Highest-risk code:** anything that moves money (deposits, ledger, payouts). Treat it with extra care and extra review.

## 2. Core skills required

| Area | Skills | Used for |
|---|---|---|
| Go | Idiomatic Go, concurrency, `context`, error wrapping, `net/http`, testing | API, workers |
| Databases | PostgreSQL, transactions, isolation levels, constraints, migrations, indexes | State + ledger |
| Lightning | BOLT11, LNURL-pay/withdraw, Lightning addresses, keysend, channels, liquidity, LND gRPC and macaroons | Deposits, payouts |
| Bitcoin basics | Sats, on-chain vs off-chain, regtest/testnet/signet | Local dev, testing |
| GitHub | GitHub Apps, installation tokens, webhooks, HMAC verification, REST/GraphQL APIs | PR verification |
| Security | Secrets management, threat modeling, idempotency, rate limiting, OWASP Top 10 | Everywhere |
| Distributed systems | Idempotent jobs, retries/backoff, outbox pattern, exactly-once semantics | Payout reliability |
| Accounting | Double-entry bookkeeping, reconciliation | Ledger |
| Frontend | Accessible HTML/CSS, TypeScript, a component framework, QR rendering | UI |
| DevOps | Docker, CI/CD, Prometheus/Grafana, backups, Linux hardening | Ops |
| Docs/Design | OpenAPI, ADRs, UX writing, WCAG | Docs + UX |

## 3. Non-negotiable rules

1. **Money is integer sats (`int64`).** Never use floats.
2. **Ledger is append-only and double-entry.** Never update or delete ledger rows; correct with a reversing entry.
3. **Every money-moving operation is idempotent.** Require an `Idempotency-Key`; enforce with DB unique constraints.
4. **One payout per bounty.** Unique constraint on `payouts.bounty_id`; check inside a DB transaction.
5. **Persist before you pay.** Write the payout attempt (with payment hash) to the DB before calling the Lightning node.
6. **Never trust webhooks alone.** Verify the signature, dedupe on delivery ID, and re-fetch the PR from the GitHub API before releasing funds.
7. **State changes go through the state machine function only.** No direct `UPDATE bounties SET state=...`.
8. **No secrets in code, logs, or git.** Redact macaroons, keys, tokens, and invoices in logs where sensitive.
9. **Fail closed.** If reconciliation fails or the node is unhealthy, pause payouts and alert.
10. **Test on regtest/testnet/signet.** Never test against mainnet funds you cannot afford to lose.

## 4. Skill playbooks

### 4.1 Adding an API endpoint
1. Update `openapi.yaml` first.
2. Add handler with auth, validation, and rate limiting.
3. If it moves money, require `Idempotency-Key` and wrap in a DB transaction.
4. Add unit + integration tests, including unauthorized and duplicate-request cases.
5. Update user-facing docs if behavior is visible.

### 4.2 Changing the bounty state machine
1. Update the diagram/table in the docs and write/adjust an ADR if semantics change.
2. Modify the single transition function; add the new transition to the allowed map.
3. Write table tests covering valid and invalid transitions.
4. Confirm escrow (reserved/available) effects in ledger tests.

### 4.3 Implementing or modifying payouts
1. Resolve destination, build invoice for `reward - fee`.
2. In one transaction: lock the bounty row, verify state is `merged`, insert `payouts` row (`pending`) and attempt record.
3. Send payment with a fee limit; store payment hash.
4. On success, one transaction: mark `paid`, write ledger entries (payout, fee, routing cost).
5. On failure, record error, schedule retry with backoff; final failure creates a claim link and notifies the contributor.
6. Required tests: crash after send but before DB write, duplicate job execution, node timeout with unknown payment status (always check payment status by hash before retry).

### 4.4 Handling a GitHub webhook
1. Verify `X-Hub-Signature-256` with constant-time compare.
2. Insert into `webhook_events` keyed by `X-GitHub-Delivery`; if it exists, return 200 and stop.
3. Enqueue processing; respond quickly (under 10 seconds).
4. In the worker, fetch current PR state from the API and apply transitions.

### 4.5 Writing a database migration
1. Forward-only migrations, small and reversible where possible.
2. Add constraints and indexes in the same change as the table.
3. Test migrate-up on a copy of realistic data; avoid long locks.
4. Never edit a merged migration.

### 4.6 Writing ADRs
Create `docs/adr/NNN-title.md` using the template in the docs. Required for: escrow model, Lightning implementation, data store, hosting, any security-relevant design change.

## 5. Local environment quick reference

```bash
make dev        # API + workers + Postgres + regtest bitcoind + 2 LND nodes
make seed       # sample projects/bounties
make test       # unit tests
make test-int   # integration tests (containers)
make test-ln    # Lightning tests on regtest
make lint       # golangci-lint + vet + govulncheck
make openapi    # validate API spec
```

Use Polar or `docker compose` to open channels between the app node and a "contributor" node, then fund and pay invoices locally. Use recorded GitHub webhook fixtures in `testdata/github/` for offline work.

## 6. Code review checklist

- [ ] Money handled as integer sats
- [ ] Transactions and locks correct; no race between state check and update
- [ ] Idempotency and unique constraints present
- [ ] Errors wrapped with context; no swallowed errors
- [ ] Logs free of secrets
- [ ] Tests cover failure paths, not only the happy path
- [ ] Docs, OpenAPI, and changelog updated
- [ ] Two reviewers for ledger/payout/auth changes

## 7. Definition of done

A change is done when: tests pass in CI, lint/security checks are clean, docs are updated, metrics/alerts exist for new failure modes, and (for money paths) a second reviewer has verified invariants: **no payout without verified merge, at most one payout per bounty, ledger balances, node reconciles.**

## 8. Guidance for AI coding assistants

- Read this file and the relevant doc section before writing code.
- Ask before changing: the ledger schema, state machine, payout flow, auth, or secrets handling.
- Prefer small, reviewable diffs with tests.
- Do not invent Lightning or GitHub API behavior; check official docs and the project's integration specs.
- Never embed real keys, macaroons, or tokens; use placeholders and `.env.example`.
- Flag any assumption marked **[ASSUMPTION]** in the docs when it affects your work.

## 9. Learning resources

- Lightning: BOLT specs, LND docs (`docs.lightning.engineering`), LNURL specs (`github.com/lnurl/luds`), Polar for local networks.
- Go: Effective Go, "100 Go Mistakes."
- Postgres: official docs on transactions and locking.
- GitHub: GitHub Apps and webhooks documentation.
- Security: OWASP ASVS, "Mastering Bitcoin" and "Mastering the Lightning Network."
