# Dev secrets and startup hardening (admin macaroon, panic)

Labels: security,tech-debt

Priority: Medium (dev defaults teach bad production habits).

## Problem

Dev compose authorizes payments with the LND **admin** macaroon and hardcodes
`dev-...-change-in-production` session/encryption secrets, normalizing the
opposite of least privilege. Separately, `queue.NewClient` panics on River
client-construction errors instead of returning them, crashing the process on a
recoverable startup fault.

## Evidence

- [docker-compose.yml](/home/bocal/SatLight/docker-compose.yml:139)
  `LND_PAYMENT_MACAROON=.../admin.macaroon`; dev secrets at :145.
- [queue.go](/home/bocal/SatLight/internal/queue/queue.go:38) `panic(err)`.

## Acceptance criteria

- [ ] Dev uses a constrained payment macaroon with a documented bakery recipe;
      secrets generated per dev environment, never hardcoded.
- [ ] `NewClient` returns errors; callers fail startup cleanly with context.
- [ ] Secrets-manager path documented for staging/prod per the threat model.
