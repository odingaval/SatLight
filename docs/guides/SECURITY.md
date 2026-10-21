# Security Policy

Satlight holds and moves Bitcoin. We take security reports seriously.

## Reporting a vulnerability
**Do not open a public issue or PR.** Email **security@<your-domain>** (PGP key: `<fingerprint / link>`) or use GitHub private vulnerability reporting on this repository.

Include: description, impact, reproduction steps, affected versions or endpoints, and any proof of concept. Never access, modify, or move other users' funds while testing.

## What to expect
| Step | Target |
|---|---|
| Acknowledgement | within 2 business days |
| Triage and severity | within 5 business days |
| Fix or mitigation plan | within 30 days (critical: ASAP) |
| Coordinated disclosure | after a fix ships, agreed with reporter |

## Scope
In scope: the Satlight API, web app, worker, ledger, payout flow, GitHub App integration, infrastructure configuration in this repository.
Out of scope: social engineering, physical attacks, denial of service by volumetric traffic, third-party services (GitHub, Lightning wallets), reports from automated scanners without a demonstrated impact.

## Priority areas
Payout logic and double-payment, ledger integrity, webhook spoofing, authentication and session handling, secrets exposure, Lightning macaroon or key handling.

## Safe harbor
Good-faith research that follows this policy will not be pursued legally. Use testnet, regtest, or your own accounts and funds.

## Rewards
Bounties for valid findings are paid in sats. Amounts are set by severity and published in `docs/specs/bounty-guidelines.md` once the beta starts.

## Supported versions
Only the latest release and `main` receive fixes before 1.0.
