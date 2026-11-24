#!/usr/bin/env bash
# Bake the constrained payment macaroon for the dev LND node (see
# docs/specs/lightning-integration.md). Run after `make dev` brings the stack
# up, then restart the API/worker so they pick the new file up:
#
#   ./scripts/dev-macaroon.sh && docker compose restart api worker
set -euo pipefail
docker exec satlight-lnd lncli --network=regtest --rpcserver=localhost:10009 \
  --tlscertpath=/data/tls.cert \
  --macaroonpath=/data/chain/bitcoin/regtest/admin.macaroon \
  bakemacaroon offchain:read offchain:write onchain:read \
  --save_to=/data/chain/bitcoin/regtest/payment.macaroon
echo "payment macaroon baked; restart api/worker to use it"
