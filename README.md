# Satlight

**A Bitcoin-native marketplace for open-source contributions.**

Every merged pull request deserves instant Bitcoin.

Maintainers fund a repository with Bitcoin and publish small tasks. Contributors claim a task, open a pull request, and receive sats over the Lightning Network within seconds of the merge. No banks, no invoices, no payment processors.

> **Status:** M1 foundations in progress. Not production ready. Do not use with funds you cannot afford to lose.

---

## Why this exists

Thousands of small but valuable contributions (docs fixes, minor bugs, tests, translations, code reviews) go unrewarded because paying someone a few dollars costs more in fees and admin than the work is worth. Lightning makes micropayments practical, so tasks worth 2,000 to 20,000 sats can finally be paid for.

## How it works

1. **Fund:** a maintainer installs the GitHub App, then deposits sats into a project wallet via a Lightning invoice.
2. **Publish:** the maintainer creates bounties (description, difficulty, reward, repo/issue link).
3. **Claim:** a contributor claims a bounty and sets a Lightning address.
4. **Contribute:** the contributor opens a PR containing `Closes #<issue>`.
5. **Get paid:** when the maintainer merges, the platform verifies the merge with GitHub and pays the contributor automatically, minus the 2% platform fee.

## Features by milestone

| Milestone | Deliverable | Status |
|---|---|---|
| M1 | Landing page, project registration, contributor waitlist | In progress |
| M2 | Create and fund bounties | Planned |
| M3 | GitHub pull request and merge verification | Planned |
| M4 | Automated Lightning payouts and closed beta (5 projects) | Planned |

## Tech stack

- **Backend:** Go
- **Database:** PostgreSQL
- **Lightning:** LND (default, see ADR-002)
- **Source control integration:** GitHub App (webhooks + REST API)
- **Frontend:** Vite + TypeScript, with htmx available for progressive enhancement

## Quick start

### Prerequisites

- Go 1.22+
- Docker and Docker Compose
- PostgreSQL 15+ (or use the provided compose file)
- Node.js 20+ (frontend)
- `make`

### Setup

```bash
git clone https://github.com/<org>/satlight.git
cd satlight

cp .env.example .env        # fill in values; never commit real secrets
make dev                    # API + workers + Postgres + regtest bitcoind + 2 LND nodes
make seed                   # load sample projects and bounties
```

In a second terminal, start the web UI:

```bash
cd web
npm ci
npm run dev
```

The API runs at `http://localhost:8080`; the web UI runs at `http://localhost:3000` and proxies API requests locally. To enable GitHub sign-in and project registration, configure the GitHub App's OAuth client ID and secret in `.env` and set its callback URL to `http://localhost:8080/api/v1/auth/github/callback`.

### Common commands

```bash
make test        # unit tests
make test-int    # integration tests (containers)
make test-ln     # Lightning tests on regtest
make lint        # golangci-lint, go vet, govulncheck
make openapi     # validate the API spec
make fmt         # format Go code
make build       # build binaries
```

### Local Lightning and GitHub

- Use regtest with two LND nodes (via `docker compose` or [Polar](https://lightningpolar.com)) to open channels and simulate deposits and payouts.
- Use recorded webhook fixtures in `testdata/github/` for offline work, or a tunnel (smee.io, ngrok) for live webhooks from a test GitHub App.

## Configuration

Key environment variables (full list in `.env.example`):

| Variable | Description |
|---|---|
| `DATABASE_URL` | PostgreSQL connection string |
| `BASE_URL` / `WEB_URL` | API callback origin and web UI origin |
| `LND_HOST` / `LND_TLS_CERT` / `LND_INVOICE_MACAROON` / `LND_PAYMENT_MACAROON` | Lightning node connection |
| `GITHUB_APP_ID` / `GITHUB_APP_PRIVATE_KEY_PATH` | GitHub App credentials |
| `GITHUB_OAUTH_CLIENT_ID` / `GITHUB_OAUTH_CLIENT_SECRET` | GitHub sign-in for project registration |
| `GITHUB_WEBHOOK_SECRET` | Webhook signature verification |
| `PLATFORM_FEE_BPS` | Platform fee in basis points (default `200` = 2%) |
| `NETWORK` | `regtest`, `signet`, `testnet`, or `mainnet` |

## Project structure

```
.
├── cmd/            # entrypoints (api, worker, migrate, seed)
├── internal/       # application code (bounties, ledger, lightning, github)
├── migrations/     # database migrations
├── web/            # Vite + TypeScript frontend
├── docs/           # all documentation
│   ├── adr/            # architecture decision records
│   ├── specs/          # technical specifications (API, state machine, etc.)
│   ├── runbooks/       # operational runbooks
│   ├── legal/          # legal documents (license, terms, privacy)
│   ├── business/       # business docs (economics, competitive analysis)
│   └── guides/         # user/developer guides (README, CONTRIBUTING, etc.)
├── testdata/       # fixtures (GitHub webhooks, etc.)
└── README.md
```

## Documentation

All documentation is organized under `docs/`:

| Category | Location |
|---|---|
| **Complete reference** | [`docs/SATLIGHT-DOCS.md`](./docs/SATLIGHT-DOCS.md) - product, architecture, security, legal, design, process, launch |
| **API specification** | [`docs/specs/openapi.yaml`](./docs/specs/openapi.yaml) |
| **Architecture decisions** | [`docs/adr/`](./docs/adr/) - ADR-001 through ADR-010 |
| **Technical specs** | [`docs/specs/`](./docs/specs/) - state machine, ledger, lightning, github, fraud, etc. |
| **Operations runbooks** | [`docs/runbooks/`](./docs/runbooks/) - incident response, disaster recovery, release process |
| **Legal & compliance** | [`docs/legal/`](./docs/legal/) - license, terms of service, privacy policy, regulatory memo |
| **Business strategy** | [`docs/business/`](./docs/business/) - economics model, competitive analysis, beta program |
| **Guides** | [`docs/guides/`](./docs/guides/) - README, CONTRIBUTING, SECURITY, SKILLS, ROADMAP, CHANGELOG, CODE_OF_CONDUCT |

## Security

Payout and ledger code handles real funds. Key rules:

- Money is always integer sats; the ledger is append-only and double-entry.
- All money-moving operations are idempotent; one payout per bounty.
- Webhooks are verified and re-checked against the GitHub API before any payout.

**Found a vulnerability?** Do not open a public issue. Report it privately via [`docs/guides/SECURITY.md`](./docs/guides/SECURITY.md) (or security@<your-domain>).

## Contributing

We use our own bounties, so contributors get paid for merged work. See [`docs/guides/CONTRIBUTING.md`](./docs/guides/CONTRIBUTING.md) for the branch, commit, and PR process, and read [`docs/guides/SKILLS.md`](./docs/guides/SKILLS.md) before touching ledger, payout, or auth code.

## Roadmap

See [`docs/guides/ROADMAP.md`](./docs/guides/ROADMAP.md) and [`docs/guides/CHANGELOG.md`](./docs/guides/CHANGELOG.md) for progress and release notes.

## License

AGPL-3.0 (proposed, see the open decisions register in [`docs/SATLIGHT-DOCS.md`](./docs/SATLIGHT-DOCS.md)). See [`docs/legal/LICENSE`](./docs/legal/LICENSE).
