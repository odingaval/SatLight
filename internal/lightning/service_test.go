package lightning

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/macaroon.v2"
)

func TestLoadMacaroonCredential(t *testing.T) {
	dir := t.TempDir()

	valid, err := macaroon.New(
		[]byte("0123456789abcdef0123456789abcdef"),
		[]byte("test-id"), "satlight-test", macaroon.V2)
	if err != nil {
		t.Fatalf("macaroon.New() error = %v", err)
	}
	raw, err := valid.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	validPath := filepath.Join(dir, "valid.macaroon")
	if err := os.WriteFile(validPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(dir, "bad.macaroon")
	if err := os.WriteFile(badPath, []byte("not-a-macaroon"), 0600); err != nil {
		t.Fatal(err)
	}

	if cred, err := loadMacaroonCredential(validPath); err != nil || cred == nil {
		t.Errorf("loadMacaroonCredential(valid) = %v, %v; want credential, nil", cred, err)
	}
	if _, err := loadMacaroonCredential(badPath); err == nil {
		t.Error("loadMacaroonCredential(garbage) = nil, want error")
	}
	if _, err := loadMacaroonCredential(filepath.Join(dir, "missing.macaroon")); err == nil {
		t.Error("loadMacaroonCredential(missing) = nil, want error")
	}
}

func TestConnectFailsFastWithoutTLS(t *testing.T) {
	svc := &Service{cfg: &Config{TLSCertPath: filepath.Join(t.TempDir(), "no-cert.pem")}}
	if err := svc.Connect(context.Background()); err == nil {
		t.Error("Connect() with missing TLS cert = nil, want error")
	}
}
