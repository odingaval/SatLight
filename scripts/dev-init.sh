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
