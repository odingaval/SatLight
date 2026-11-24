//go:build integration

package lightning

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Regtest suite against the two-node network from docker-compose.ln.yml,
// driven by `make test-ln` (scripts/ln-test.sh). It skips unless the runner
// exports the LND_REGTEST_* connection variables, so the containerized
// `make test-int` run is unaffected.
type regtestNode struct {
	host       string
	tlsCert    string
	invoiceMac string
	paymentMac string
	container  string
}

func regtestEnv(t *testing.T) (app, contrib regtestNode) {
	t.Helper()
	host := os.Getenv("LND_REGTEST_APP_HOST")
	if host == "" {
		t.Skip("LND_REGTEST_* is unset; run the Lightning regtest suite with `make test-ln`")
	}
	app = regtestNode{
		host:       host,
		tlsCert:    os.Getenv("LND_REGTEST_APP_TLS"),
		invoiceMac: os.Getenv("LND_REGTEST_APP_INVOICE_MAC"),
		paymentMac: os.Getenv("LND_REGTEST_APP_ADMIN_MAC"),
		container:  "satlight-ln-lnd-app-1",
	}
	contrib = regtestNode{
		host:       os.Getenv("LND_REGTEST_CONTRIB_HOST"),
		tlsCert:    os.Getenv("LND_REGTEST_CONTRIB_TLS"),
		invoiceMac: os.Getenv("LND_REGTEST_CONTRIB_INVOICE_MAC"),
		paymentMac: os.Getenv("LND_REGTEST_CONTRIB_ADMIN_MAC"),
		container:  "satlight-ln-lnd-contrib-1",
	}
	return app, contrib
}

func regtestService(t *testing.T, node regtestNode) *Service {
	t.Helper()
	svc := NewService(&Config{
		Host:            node.host,
		TLSCertPath:     node.tlsCert,
		InvoiceMacaroon: node.invoiceMac,
		PaymentMacaroon: node.paymentMac,
	}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := svc.Connect(ctx); err != nil {
		t.Fatalf("connect to %s: %v", node.host, err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func regtestLncli(t *testing.T, container string, args ...string) string {
	t.Helper()
	full := append([]string{"exec", container, "lncli", "--network=regtest",
		"--rpcserver=localhost:10009", "--tlscertpath=/data/tls.cert",
		"--macaroonpath=/data/chain/bitcoin/regtest/admin.macaroon"}, args...)
	out, err := exec.CommandContext(context.Background(), "docker", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("lncli %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestRegtestDepositSettlement(t *testing.T) {
	appNode, contribNode := regtestEnv(t)
	app := regtestService(t, appNode)
	contrib := regtestService(t, contribNode)
	ctx := context.Background()

	bolt11, wantHash, err := app.CreateInvoice(ctx, 50000, "regtest deposit", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	settled := make(chan InvoiceUpdate, 4)
	streamCtx, stopStream := context.WithCancel(ctx)
	defer stopStream()
	streamErr := make(chan error, 1)
	go func() {
		streamErr <- app.SubscribeInvoices(streamCtx, func(update InvoiceUpdate) error {
			settled <- update
			return nil
		})
	}()

	sentHash, err := contrib.PayInvoice(ctx, bolt11, 1000)
	if err != nil {
		t.Fatalf("contributor payment failed: %v", err)
	}
	if string(sentHash) != string(wantHash) {
		t.Fatalf("paid hash %x, want invoice hash %x", sentHash, wantHash)
	}
	select {
	case update := <-settled:
		if string(update.PaymentHash) != string(wantHash) || update.State != "settled" {
			t.Fatalf("stream update = %x/%s, want settled invoice", update.PaymentHash, update.State)
		}
		if update.AmountPaidSats != 50000 {
			t.Fatalf("paid = %d sats, want 50000", update.AmountPaidSats)
		}
	case err := <-streamErr:
		t.Fatalf("invoice stream ended: %v", err)
	case <-time.After(60 * time.Second):
		t.Fatal("timed out waiting for the settlement update")
	}
	lookup, err := app.LookupInvoice(ctx, wantHash)
	if err != nil || !lookup.Settled || lookup.PaidSats != 50000 {
		t.Fatalf("lookup = %+v err=%v, want settled 50000", lookup, err)
	}
}

func TestRegtestPayInvoiceSuccess(t *testing.T) {
	appNode, contribNode := regtestEnv(t)
	app := regtestService(t, appNode)
	contrib := regtestService(t, contribNode)
	ctx := context.Background()

	bolt11, wantHash, err := contrib.CreateInvoice(ctx, 20000, "regtest payout", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sentHash, err := app.PayInvoice(ctx, bolt11, 1000)
	if err != nil {
		t.Fatal(err)
	}
	status, fee, err := app.GetPaymentResult(ctx, sentHash)
	if err != nil {
		t.Fatal(err)
	}
	if status != "SUCCEEDED" {
		t.Fatalf("payment status = %q, want SUCCEEDED", status)
	}
	if string(sentHash) != string(wantHash) {
		t.Fatalf("paid hash %x, want invoice hash %x", sentHash, wantHash)
	}
	// Single-hop over the direct channel: no forwarding, so no fee. The
	// assertion pins the accounting path (track-by-hash returns the terminal
	// fee, whatever the topology charges).
	if fee != 0 {
		t.Fatalf("routing fee = %d, want 0 on the direct channel", fee)
	}
	lookup, err := contrib.LookupInvoice(ctx, wantHash)
	if err != nil || !lookup.Settled {
		t.Fatalf("recipient lookup = %+v err=%v, want settled", lookup, err)
	}
}

func TestRegtestNodeBalances(t *testing.T) {
	appNode, _ := regtestEnv(t)
	app := regtestService(t, appNode)
	ctx := context.Background()

	// Both wallets funded the suite setup, so every balance reads positive.
	// Balance reads run on the payment connection (see the macaroon split).
	total, err := app.GetTotalBalance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if total <= 0 {
		t.Fatalf("total balance = %d, want positive", total)
	}
	channels, err := app.GetChannelBalance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if channels <= 0 {
		t.Fatalf("channel balance = %d, want positive", channels)
	}
	wallet, err := app.GetWalletBalance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if wallet < 0 {
		t.Fatalf("wallet balance = %d, want non-negative", wallet)
	}
}

func TestRegtestPayInvoiceNoRoute(t *testing.T) {
	appNode, contribNode := regtestEnv(t)
	app := regtestService(t, appNode)
	contrib := regtestService(t, contribNode)
	ctx := context.Background()

	// Far above channel capacity: no route can exist.
	bolt11, _, err := contrib.CreateInvoice(ctx, 1_000_000_000, "regtest too big", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.PayInvoice(ctx, bolt11, 1000); err == nil {
		t.Fatal("over-capacity payment succeeded, want a no-route failure")
	}
}

func TestRegtestPayInvoiceOfflineRecipient(t *testing.T) {
	appNode, contribNode := regtestEnv(t)
	app := regtestService(t, appNode)
	contrib := regtestService(t, contribNode)
	ctx := context.Background()

	bolt11, _, err := contrib.CreateInvoice(ctx, 15000, "regtest offline", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		IdentityPubkey string `json:"identity_pubkey"`
	}
	if err := json.Unmarshal([]byte(regtestLncli(t, contribNode.container, "getinfo")), &info); err != nil {
		t.Fatal(err)
	}
	// Drop the only peer: with no path to the recipient the send fails.
	regtestLncli(t, appNode.container, "disconnect", info.IdentityPubkey)
	// Restore peering for the tests that follow.
	defer regtestLncli(t, appNode.container, "connect", info.IdentityPubkey+"@lnd-contrib:9735")
	if _, err := app.PayInvoice(ctx, bolt11, 1000); err == nil {
		t.Fatal("payment to an offline recipient succeeded, want failure")
	}
}

func TestRegtestInvoiceExpiry(t *testing.T) {
	appNode, contribNode := regtestEnv(t)
	app := regtestService(t, appNode)
	contrib := regtestService(t, contribNode)
	ctx := context.Background()

	bolt11, _, err := contrib.CreateInvoice(ctx, 10000, "regtest short-lived", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
	if _, err := app.PayInvoice(ctx, bolt11, 1000); err == nil {
		t.Fatal("payment to an expired invoice succeeded, want failure")
	}
}

func TestRegtestDuplicatePayment(t *testing.T) {
	appNode, contribNode := regtestEnv(t)
	app := regtestService(t, appNode)
	contrib := regtestService(t, contribNode)
	ctx := context.Background()

	bolt11, _, err := contrib.CreateInvoice(ctx, 12000, "regtest duplicate", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.PayInvoice(ctx, bolt11, 1000); err != nil {
		t.Fatalf("first payment failed: %v", err)
	}
	// The same invoice twice: LND rejects the replay, which is the
	// node-level backstop behind payout idempotency.
	if _, err := app.PayInvoice(ctx, bolt11, 1000); err == nil {
		t.Fatal("second payment of the same invoice succeeded, want rejection")
	}
}

// Last: it restarts the app node, so it runs after every other test in this
// package (Go runs tests in file order within a package).
func TestRegtestRestartRecovery(t *testing.T) {
	appNode, contribNode := regtestEnv(t)
	app := regtestService(t, appNode)
	contrib := regtestService(t, contribNode)
	ctx := context.Background()

	bolt11, wantHash, err := contrib.CreateInvoice(ctx, 18000, "regtest restart", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sentHash, err := app.PayInvoice(ctx, bolt11, 1000)
	if err != nil {
		t.Fatal(err)
	}
	beforeStatus, beforeFee, err := app.GetPaymentResult(ctx, sentHash)
	if err != nil || beforeStatus != "SUCCEEDED" {
		t.Fatalf("pre-restart status = %q err=%v, want SUCCEEDED", beforeStatus, err)
	}

	restartCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(restartCtx, "docker", "restart", appNode.container).CombinedOutput(); err != nil {
		t.Fatalf("docker restart: %v\n%s", err, out)
	}
	// `docker restart` returns once the container starts, not once LND is
	// back; wait for the RPC to answer (the wallet auto-unlocks).
	ready := false
	for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); {
		out, err := exec.CommandContext(ctx, "docker", "exec", appNode.container, "lncli",
			"--network=regtest", "--rpcserver=localhost:10009", "--tlscertpath=/data/tls.cert",
			"--macaroonpath=/data/chain/bitcoin/regtest/admin.macaroon", "getinfo").CombinedOutput()
		if err == nil {
			ready = true
			break
		}
		_ = out
		time.Sleep(3 * time.Second)
	}
	if !ready {
		t.Fatal("app node did not come back after restart")
	}
	reconnected := regtestService(t, appNode)
	afterStatus, afterFee, err := reconnected.GetPaymentResult(ctx, sentHash)
	if err != nil {
		t.Fatalf("post-restart lookup failed: %v", err)
	}
	if afterStatus != "SUCCEEDED" || afterFee != beforeFee || string(sentHash) != string(wantHash) {
		t.Fatalf("post-restart = %q fee %d, want SUCCEEDED fee %d", afterStatus, afterFee, beforeFee)
	}
}
