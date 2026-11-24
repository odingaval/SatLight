package lightning

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// fakeRouter scripts TrackPaymentV2 streams; the embedded nil interface keeps
// the fake small since only TrackPaymentV2 is exercised.
type fakeRouter struct {
	routerrpc.RouterClient
	dialErr error
	stream  *fakeTrackStream
	gotReq  *routerrpc.TrackPaymentRequest
}

func (f *fakeRouter) TrackPaymentV2(ctx context.Context, in *routerrpc.TrackPaymentRequest, _ ...grpc.CallOption) (routerrpc.Router_TrackPaymentV2Client, error) {
	f.gotReq = in
	if f.dialErr != nil {
		return nil, f.dialErr
	}
	f.stream.ctx = ctx
	return f.stream, nil
}

type fakeTrackStream struct {
	grpc.ClientStream
	ctx    context.Context
	msgs   []*lnrpc.Payment
	block  bool // block until ctx ends instead of serving msgs
	served int
}

func (f *fakeTrackStream) Recv() (*lnrpc.Payment, error) {
	if f.block {
		<-f.ctx.Done()
		return nil, f.ctx.Err()
	}
	if f.served >= len(f.msgs) {
		return nil, io.EOF
	}
	msg := f.msgs[f.served]
	f.served++
	return msg, nil
}

func paymentMsg(status lnrpc.Payment_PaymentStatus, feeSat int64) *lnrpc.Payment {
	return &lnrpc.Payment{Status: status, FeeSat: feeSat}
}

func TestGetPaymentResultTerminalFirstMessage(t *testing.T) {
	router := &fakeRouter{stream: &fakeTrackStream{msgs: []*lnrpc.Payment{paymentMsg(lnrpc.Payment_SUCCEEDED, 7)}}}
	svc := &Service{routerClient: router, trackTimeout: time.Second}
	status, fee, err := svc.GetPaymentResult(context.Background(), []byte("hash"))
	if err != nil || status != "SUCCEEDED" || fee != 7 {
		t.Fatalf("GetPaymentResult() = %q, %d, %v; want SUCCEEDED, 7, nil", status, fee, err)
	}
	if router.gotReq == nil || !router.gotReq.NoInflightUpdates {
		t.Fatal("TrackPaymentV2 was not asked to suppress in-flight updates")
	}
}

func TestGetPaymentResultSkipsInflightMessages(t *testing.T) {
	router := &fakeRouter{stream: &fakeTrackStream{msgs: []*lnrpc.Payment{
		paymentMsg(lnrpc.Payment_IN_FLIGHT, 0),
		paymentMsg(lnrpc.Payment_IN_FLIGHT, 3),
		paymentMsg(lnrpc.Payment_SUCCEEDED, 42),
	}}}
	svc := &Service{routerClient: router, trackTimeout: time.Second}
	status, fee, err := svc.GetPaymentResult(context.Background(), []byte("hash"))
	if err != nil || status != "SUCCEEDED" || fee != 42 {
		t.Fatalf("GetPaymentResult() = %q, %d, %v; want SUCCEEDED, 42, nil", status, fee, err)
	}
}

func TestGetPaymentResultFailed(t *testing.T) {
	router := &fakeRouter{stream: &fakeTrackStream{msgs: []*lnrpc.Payment{
		paymentMsg(lnrpc.Payment_IN_FLIGHT, 0),
		paymentMsg(lnrpc.Payment_FAILED, 0),
	}}}
	svc := &Service{routerClient: router, trackTimeout: time.Second}
	status, fee, err := svc.GetPaymentResult(context.Background(), []byte("hash"))
	if err != nil || status != "FAILED" || fee != 0 {
		t.Fatalf("GetPaymentResult() = %q, %d, %v; want FAILED, 0, nil", status, fee, err)
	}
}

func TestGetPaymentResultTimeoutReportsInflight(t *testing.T) {
	router := &fakeRouter{stream: &fakeTrackStream{block: true}}
	svc := &Service{routerClient: router, trackTimeout: 50 * time.Millisecond}
	start := time.Now()
	status, fee, err := svc.GetPaymentResult(context.Background(), []byte("hash"))
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("GetPaymentResult() blocked %v, want a bounded wait", elapsed)
	}
	if err != nil || status != "IN_FLIGHT" || fee != 0 {
		t.Fatalf("GetPaymentResult() = %q, %d, %v; want IN_FLIGHT, 0, nil", status, fee, err)
	}
}

func TestGetPaymentResultUnknownHash(t *testing.T) {
	router := &fakeRouter{dialErr: status.Error(codes.NotFound, "payment not initiated")}
	svc := &Service{routerClient: router, trackTimeout: time.Second}
	if status, _, err := svc.GetPaymentResult(context.Background(), []byte("nope")); err == nil {
		t.Fatalf("GetPaymentResult() = %q, nil; want NotFound error", status)
	}
}

func TestGetPaymentResultEOFWithoutTerminal(t *testing.T) {
	router := &fakeRouter{stream: &fakeTrackStream{}}
	svc := &Service{routerClient: router, trackTimeout: time.Second}
	status, fee, err := svc.GetPaymentResult(context.Background(), []byte("hash"))
	if err != nil || status != "unknown" || fee != 0 {
		t.Fatalf("GetPaymentResult() = %q, %d, %v; want unknown, 0, nil", status, fee, err)
	}
}

// fakePayClient scripts DecodePayReq; the embedded nil interface keeps the
// fake small since only DecodePayReq is exercised.
type fakePayClient struct {
	lnrpc.LightningClient
	payReq *lnrpc.PayReq
}

func (f *fakePayClient) DecodePayReq(context.Context, *lnrpc.PayReqString, ...grpc.CallOption) (*lnrpc.PayReq, error) {
	return f.payReq, nil
}

func (f *fakeRouter) SendPaymentV2(ctx context.Context, _ *routerrpc.SendPaymentRequest, _ ...grpc.CallOption) (routerrpc.Router_SendPaymentV2Client, error) {
	f.stream.ctx = ctx
	return f.stream, nil
}

func payTestService(router *fakeRouter) *Service {
	return &Service{
		paymentClient: &fakePayClient{payReq: &lnrpc.PayReq{NumSatoshis: 1000}},
		routerClient:  router,
		sendTimeout:   50 * time.Millisecond,
	}
}

func TestPayInvoiceSendTimeout(t *testing.T) {
	router := &fakeRouter{stream: &fakeTrackStream{block: true}}
	svc := payTestService(router)
	start := time.Now()
	hash, err := svc.PayInvoice(context.Background(), "lnbc10mock", 100)
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("PayInvoice() blocked %v, want a bounded wait", elapsed)
	}
	if !errors.Is(err, ErrPaymentTimedOut) {
		t.Fatalf("PayInvoice() = %x, %v; want nil, ErrPaymentTimedOut", hash, err)
	}
}

func TestPayInvoiceSuccess(t *testing.T) {
	router := &fakeRouter{stream: &fakeTrackStream{msgs: []*lnrpc.Payment{
		{Status: lnrpc.Payment_IN_FLIGHT},
		{Status: lnrpc.Payment_SUCCEEDED, PaymentHash: "ab12"},
	}}}
	svc := payTestService(router)
	hash, err := svc.PayInvoice(context.Background(), "lnbc10mock", 100)
	if err != nil {
		t.Fatalf("PayInvoice() error = %v, want nil", err)
	}
	if len(hash) != 2 || hash[0] != 0xab || hash[1] != 0x12 {
		t.Fatalf("PayInvoice() hash = %x, want ab12", hash)
	}
}

func TestPayInvoiceFailedIsDefinitive(t *testing.T) {
	router := &fakeRouter{stream: &fakeTrackStream{msgs: []*lnrpc.Payment{
		{Status: lnrpc.Payment_FAILED, FailureReason: lnrpc.PaymentFailureReason_FAILURE_REASON_NO_ROUTE},
	}}}
	svc := payTestService(router)
	if _, err := svc.PayInvoice(context.Background(), "lnbc10mock", 100); err == nil {
		t.Fatal("PayInvoice() = nil, want a no-route failure")
	} else if errors.Is(err, ErrPaymentTimedOut) {
		t.Fatalf("PayInvoice() error = %v, want a definitive failure, not a timeout", err)
	}
}
