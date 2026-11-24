#!/usr/bin/env bash
# Bring the regtest network in docker-compose.ln.yml to a funded, connected
# state: mine regtest coins, fund both LND wallets, peer the nodes, and open
# one channel in each direction so both sides start liquid.
#
# Expects the compose stack healthy (wallets auto-created via the password
# files written by scripts/ln-test.sh). Idempotent only on a fresh data dir;
# ln-test.sh wipes testdata/ln-regtest before starting.
set -euo pipefail

BITCOIND=(docker exec satlight-ln-bitcoind-1 bitcoin-cli -regtest -rpcuser=satlight -rpcpassword=satlight)
LNCLI_APP=(docker exec satlight-ln-lnd-app-1 lncli --network=regtest --rpcserver=localhost:10009 --tlscertpath=/data/tls.cert --macaroonpath=/data/chain/bitcoin/regtest/admin.macaroon)
LNCLI_CONTRIB=(docker exec satlight-ln-lnd-contrib-1 lncli --network=regtest --rpcserver=localhost:10009 --tlscertpath=/data/tls.cert --macaroonpath=/data/chain/bitcoin/regtest/admin.macaroon)

json_get() { python3 -c "import json,sys; print(json.load(sys.stdin)$1)"; }

echo "--- mining regtest coins"
"${BITCOIND[@]}" createwallet "miner" >/dev/null 2>&1 || true
MINER_ADDR=$("${BITCOIND[@]}" -rpcwallet=miner getnewaddress)
"${BITCOIND[@]}" -rpcwallet=miner generatetoaddress 120 "$MINER_ADDR" >/dev/null

echo "--- funding LND wallets"
APP_ADDR=$("${LNCLI_APP[@]}" newaddress p2wkh | json_get "['address']")
CONTRIB_ADDR=$("${LNCLI_CONTRIB[@]}" newaddress p2wkh | json_get "['address']")
"${BITCOIND[@]}" -rpcwallet=miner sendtoaddress "$APP_ADDR" 1 >/dev/null
"${BITCOIND[@]}" -rpcwallet=miner sendtoaddress "$CONTRIB_ADDR" 1 >/dev/null
"${BITCOIND[@]}" -rpcwallet=miner generatetoaddress 6 "$MINER_ADDR" >/dev/null

echo "--- peering nodes"
APP_PUB=$("${LNCLI_APP[@]}" getinfo | json_get "['identity_pubkey']")
CONTRIB_PUB=$("${LNCLI_CONTRIB[@]}" getinfo | json_get "['identity_pubkey']")
"${LNCLI_APP[@]}" connect "$CONTRIB_PUB@lnd-contrib:9735" >/dev/null

count_active() { python3 -c "import json,sys; print(len(json.load(sys.stdin)['channels']))" 2>/dev/null || echo 0; }
wait_active() { # <lncli-array-name> <want>
  local -n cli=$1
  for _ in $(seq 1 60); do
    if [ "$("${cli[@]}" listchannels --active_only | count_active)" -ge "$2" ]; then
      return 0
    fi
    sleep 5
  done
  echo "timed out waiting for $2 active channels" >&2
  return 1
}

# Sequential opens: LND accepts one pending channel per peer by default.
echo "--- opening channel app -> contrib"
"${LNCLI_APP[@]}" openchannel --node_key="$CONTRIB_PUB" --local_amt=3000000 >/dev/null
"${BITCOIND[@]}" -rpcwallet=miner generatetoaddress 6 "$MINER_ADDR" >/dev/null
wait_active LNCLI_APP 1

echo "--- opening channel contrib -> app"
"${LNCLI_CONTRIB[@]}" openchannel --node_key="$APP_PUB" --local_amt=3000000 >/dev/null
"${BITCOIND[@]}" -rpcwallet=miner generatetoaddress 6 "$MINER_ADDR" >/dev/null
# Each node now sees both channels (one opened, one accepted).
wait_active LNCLI_CONTRIB 2
wait_active LNCLI_APP 2

echo "regtest network ready: 1 active channel per direction"
