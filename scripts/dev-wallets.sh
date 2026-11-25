#!/usr/bin/env bash
# Create the dev LND wallets on first boot. LND never auto-creates a wallet
# (the password file only auto-unlocks afterwards), so fresh checkouts need
# this once after `make dev` brings the nodes up. Idempotent: nodes that
# already hold an admin macaroon are skipped.
set -euo pipefail
cd "$(dirname "$0")/.."

create_wallet() { # <name> <container> <host-port> <pass-file>
  if docker exec "$2" test -f /data/chain/bitcoin/regtest/admin.macaroon 2>/dev/null; then
    echo "$1: wallet already exists, skipping"
    return 0
  fi
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' RETURN
  docker cp "$2:/data/tls.cert" "$tmpdir/tls.cert"
  cp "$4" "$tmpdir/wallet.pass"
  go run ./scripts/lninit "$tmpdir" "localhost:$3"
  for _ in $(seq 1 40); do
    if docker exec "$2" test -f /data/chain/bitcoin/regtest/admin.macaroon 2>/dev/null; then
      echo "$1: wallet ready"
      return 0
    fi
    sleep 5
  done
  echo "$1: admin macaroon never appeared" >&2
  return 1
}

create_wallet app satlight-lnd 10009 dev/lnd/app.pass
create_wallet contrib satlight-lnd-contributor 10010 dev/lnd/contrib.pass
