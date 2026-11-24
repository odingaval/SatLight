#!/usr/bin/env bash
# Full Lightning regtest run: fresh chain state, funded two-node network,
# Go suite against the real nodes, teardown. Invoked by `make test-ln`.
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE=(docker compose -p satlight-ln -f docker-compose.ln.yml)

cleanup() {
  if [ "${KEEP:-0}" = "1" ]; then
    echo "KEEP=1: leaving the stack up for inspection (compose -p satlight-ln -f docker-compose.ln.yml down to remove)"
    return 0
  fi
  "${COMPOSE[@]}" down >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "--- starting regtest network"
# LND runs as root, so its bind-mounted state is root-owned; wipe it from
# inside a throwaway container (the image is already required for the stack).
docker run --rm -v "$(pwd)/testdata/ln-regtest:/w" --entrypoint rm lightninglabs/lnd:v0.21.4-beta -rf /w/app /w/contrib /w/bitcoind
mkdir -p testdata/ln-regtest/app testdata/ln-regtest/contrib testdata/ln-regtest/bitcoind
# Ephemeral wallet passwords for the test nodes only; never committed.
openssl rand -hex 16 > testdata/ln-regtest/app/wallet.pass
openssl rand -hex 16 > testdata/ln-regtest/contrib/wallet.pass
chmod 600 testdata/ln-regtest/app/wallet.pass testdata/ln-regtest/contrib/wallet.pass
"${COMPOSE[@]}" up -d bitcoind --wait

echo "--- pre-mining (LND chain sync needs blocks on boot)"
docker exec satlight-ln-bitcoind-1 bitcoin-cli -regtest -rpcuser=satlight -rpcpassword=satlight createwallet "miner" >/dev/null 2>&1 || true
MINER_ADDR=$(docker exec satlight-ln-bitcoind-1 bitcoin-cli -regtest -rpcuser=satlight -rpcpassword=satlight -rpcwallet=miner getnewaddress)
docker exec satlight-ln-bitcoind-1 bitcoin-cli -regtest -rpcuser=satlight -rpcpassword=satlight -rpcwallet=miner generatetoaddress 120 "$MINER_ADDR" >/dev/null

"${COMPOSE[@]}" up -d --wait

echo "--- waiting for LND RPC ports"
for _ in $(seq 1 60); do
  if (echo > /dev/tcp/localhost/11009) >/dev/null 2>&1 && (echo > /dev/tcp/localhost/11010) >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

echo "--- creating LND wallets"
go run ./scripts/lninit testdata/ln-regtest/app localhost:11009
go run ./scripts/lninit testdata/ln-regtest/contrib localhost:11010
echo "--- waiting for macaroons"
for _ in $(seq 1 60); do
  if docker exec satlight-ln-lnd-app-1 test -f /data/chain/bitcoin/regtest/admin.macaroon 2>/dev/null \
  && docker exec satlight-ln-lnd-contrib-1 test -f /data/chain/bitcoin/regtest/admin.macaroon 2>/dev/null; then
    break
  fi
  sleep 5
done
docker exec satlight-ln-lnd-app-1 test -f /data/chain/bitcoin/regtest/admin.macaroon
docker exec satlight-ln-lnd-contrib-1 test -f /data/chain/bitcoin/regtest/admin.macaroon
echo "--- waiting for node RPC"
for _ in $(seq 1 60); do
  if docker exec satlight-ln-lnd-app-1 lncli --network=regtest --rpcserver=localhost:10009 --tlscertpath=/data/tls.cert --macaroonpath=/data/chain/bitcoin/regtest/admin.macaroon getinfo >/dev/null 2>&1 \
  && docker exec satlight-ln-lnd-contrib-1 lncli --network=regtest --rpcserver=localhost:10009 --tlscertpath=/data/tls.cert --macaroonpath=/data/chain/bitcoin/regtest/admin.macaroon getinfo >/dev/null 2>&1; then
    break
  fi
  sleep 5
done
docker exec satlight-ln-lnd-app-1 lncli --network=regtest --rpcserver=localhost:10009 --tlscertpath=/data/tls.cert --macaroonpath=/data/chain/bitcoin/regtest/admin.macaroon getinfo >/dev/null
docker exec satlight-ln-lnd-contrib-1 lncli --network=regtest --rpcserver=localhost:10009 --tlscertpath=/data/tls.cert --macaroonpath=/data/chain/bitcoin/regtest/admin.macaroon getinfo >/dev/null
# LND runs as root; make its certs readable to the test process.
docker exec satlight-ln-lnd-app-1 chmod -R a+rX /data/chain /data/tls.cert
docker exec satlight-ln-lnd-contrib-1 chmod -R a+rX /data/chain /data/tls.cert

./scripts/ln-setup.sh

echo "--- running regtest suite"
# Absolute paths: `go test` runs inside the package directory.
LNREG="$(pwd)/testdata/ln-regtest"
LND_REGTEST_APP_HOST=localhost:11009 \
LND_REGTEST_APP_TLS="$LNREG/app/tls.cert" \
LND_REGTEST_APP_INVOICE_MAC="$LNREG/app/chain/bitcoin/regtest/invoice.macaroon" \
LND_REGTEST_APP_ADMIN_MAC="$LNREG/app/chain/bitcoin/regtest/admin.macaroon" \
LND_REGTEST_CONTRIB_HOST=localhost:11010 \
LND_REGTEST_CONTRIB_TLS="$LNREG/contrib/tls.cert" \
LND_REGTEST_CONTRIB_INVOICE_MAC="$LNREG/contrib/chain/bitcoin/regtest/invoice.macaroon" \
LND_REGTEST_CONTRIB_ADMIN_MAC="$LNREG/contrib/chain/bitcoin/regtest/admin.macaroon" \
  go test -v -tags=integration -count=1 -timeout 10m ./internal/lightning/ -run 'TestRegtest'
