// Command lninit creates and unlocks the regtest LND wallets over the
// WalletUnlocker RPC. LND's password file only auto-unlocks existing
// wallets, so first boot always needs this step; later boots (including the
// restart-recovery test) unlock on their own.
//
// Usage: go run ./scripts/lninit <lnddir> <host:port>
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: lninit <lnddir> <host:port>")
		os.Exit(2)
	}
	lnddir, addr := os.Args[1], os.Args[2]
	if err := initWallet(lnddir, addr); err != nil {
		fmt.Fprintln(os.Stderr, "lninit:", err)
		os.Exit(1)
	}
	fmt.Println("wallet ready on", addr)
}

func initWallet(lnddir, addr string) error {
	password, err := os.ReadFile(filepath.Join(lnddir, "wallet.pass"))
	if err != nil {
		return err
	}
	// Trim the trailing newline ln-test.sh leaves behind.
	for len(password) > 0 && (password[len(password)-1] == '\n' || password[len(password)-1] == '\r') {
		password = password[:len(password)-1]
	}
	tlsCreds, err := credentials.NewClientTLSFromFile(filepath.Join(lnddir, "tls.cert"), "")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(tlsCreds))
	if err != nil {
		return err
	}
	defer conn.Close()
	unlocker := lnrpc.NewWalletUnlockerClient(conn)

	seed, err := unlocker.GenSeed(ctx, &lnrpc.GenSeedRequest{})
	if err != nil {
		return fmt.Errorf("genseed: %w", err)
	}
	_, err = unlocker.InitWallet(ctx, &lnrpc.InitWalletRequest{
		WalletPassword:     password,
		CipherSeedMnemonic: seed.CipherSeedMnemonic,
	})
	if err != nil {
		return fmt.Errorf("initwallet: %w", err)
	}
	// Macaroon baking and chain sync continue in the background; the caller
	// waits for the admin macaroon over docker exec (LND runs as root, so
	// its files are invisible to this process until chmodded).
	return nil
}
