# Runbook: signet Lightning node for staging

The deployed app holds no coins itself: deposits and payouts flow through an
LND node it reaches over gRPC. This runbook provisions that node on a small
VPS for staging (signet only — never mainnet until the launch review in
`docs/runbooks/operations.md` is signed off).

## 1. Provision the host

- Any VPS with 2 GB RAM, 40 GB disk, Ubuntu LTS, and a public IP or hostname.
- Install Docker + the compose plugin.
- Firewall: allow `9735` (Lightning P2P) from anywhere; allow `10009` (gRPC)
  from as narrow a range as you can — the app authenticates with TLS plus
  macaroons, but there is no reason to advertise the port.

## 2. Start bitcoind + LND on signet

Adapt the service definitions from `docker-compose.ln.yml`: same images and
layout, but replace every `regtest` flag/value with `signet`
(`--bitcoin.signet`, `-signet=1`, signet ports) and set
`--tlsextradomain=<your public host>` so the app's TLS check passes.
Mount a password file for `--wallet-unlock-password-file` plus
`--wallet-unlock-allow-create` so restarts never strand on a locked wallet
(see `scripts/dev-init.sh` for the dev equivalent).

Bring up bitcoind first and wait for IBD (signet is small, usually minutes),
then LND. Create the wallet once over the unlocker RPC
(`go run ./scripts/lninit <dir-with-tls.cert> <host>:10009` with a
`wallet.pass` beside the cert, or `lncli create`).

## 3. Connect the app (read-only first)

1. Copy three files off the node: `tls.cert`, `invoice.macaroon`, and a
   **freshly baked** payment macaroon, never admin:
   `lncli bakemacaroon offchain:read offchain:write onchain:read`.
2. Upload them as Render Secret Files and set, on the web service:
   `LND_HOST=<public host>:10009`, `LND_TLS_CERT`, `LND_INVOICE_MACAROON`,
   `LND_PAYMENT_MACAROON` to the `/etc/secrets/` paths.
3. Redeploy and confirm `/metrics` serves and no `tls:`/`certificate`
   errors appear in the logs.

## 4. Fund and open channels

1. Get signet coins from a faucet into the node's on-chain wallet
   (`lncli newaddress p2wkh`, wait for confirms).
2. Open channels to well-connected signet peers for **outbound** (payouts).
3. Arrange **inbound** before accepting deposits (an unannounced new node
   cannot receive): a peer swap, a loop-out, or spend-first and confirm
   `GetChannelBalance` shows remote capacity. Do not skip this — deposits
   into a node with no inbound fail at the sender.
4. Rehearse the loop from the local stack first if you have not: invoice,
   pay, wallet credit, ledger balance.

## 5. Unpause and verify

1. Clear `funding_paused` and `outflows_paused` through the two-person
   resume (`cmd/opsctl propose` as one operator, `approve` as another).
2. Fund a scratch project with a small deposit, confirm the wallet credit
   and the balanced `node:liquidity` / `project:{id}:available` entries,
   then create and cancel a test bounty to watch reservation accounting.
3. Back up the seed phrase offline and export a static channel backup
   (`lncli exportchanbackup --all`) after every channel open; see
   `docs/runbooks/disaster-recovery.md`.
