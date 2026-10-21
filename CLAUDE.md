# CLAUDE.md (also usable as AGENTS.md)

Instructions for AI coding assistants working on Satlight.

1. Read `SKILLS.md` first; its rules are non-negotiable, especially: integer sats, append-only double-entry ledger, idempotent money operations, one payout per bounty, persist-before-pay.
2. Specs live in `docs/`. The full documentation is `docs/SATLIGHT-DOCS.md`; the API contract is `openapi.yaml`; the schema is `migrations/`.
3. Ask before changing the ledger schema, state machine, payout flow, authentication, or secrets handling.
4. Never put real keys, macaroons, tokens, or invoices in code, tests, logs, or commits. Use `.env.example` placeholders.
5. Test on regtest, signet, or testnet only.
6. Prefer small diffs with tests, including failure paths (crash mid-payout, duplicate webhook, node timeout).
7. Do not invent Lightning or GitHub API behavior; check official docs and `docs/specs/`.
8. Update `openapi.yaml`, docs, and `CHANGELOG.md` with behavior changes.
9. Items marked **[ASSUMPTION]** in docs are unconfirmed; flag them when they affect your work.

Commands: `make dev`, `make seed`, `make test`, `make test-int`, `make test-ln`, `make lint`, `make openapi`.
