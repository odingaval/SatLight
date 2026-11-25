#!/usr/bin/env bash
# Generate per-checkout dev secrets into .env (gitignored, never committed).
# Idempotent: existing values are kept. Run once after cloning, or let
# `make dev` run it for you.
set -euo pipefail
cd "$(dirname "$0")/.."
touch .env
ensure() {
  if ! grep -q "^$1=" .env; then
    echo "$1=$(openssl rand -hex 32)" >> .env
    echo "generated $1"
  fi
}
ensure SESSION_SECRET
ensure ENCRYPTION_KEY
echo "dev secrets ready in .env"
# LND wallet passwords for the dev nodes (gitignored via dev/). The compose
# file mounts these for wallet auto-unlock and first-boot auto-creation, so
# `make dev` never stops at a locked wallet.
mkdir -p dev/lnd
for node in app contrib; do
  if [ ! -f "dev/lnd/$node.pass" ]; then
    openssl rand -hex 16 > "dev/lnd/$node.pass"
    chmod 600 "dev/lnd/$node.pass"
    echo "generated dev/lnd/$node.pass"
  fi
done
