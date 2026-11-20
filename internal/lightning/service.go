package lightning

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/macaroons"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"gopkg.in/macaroon.v2"
)

var (
	ErrInvoiceExpired = errors.New("invoice expired")
	ErrPaymentFailed  = errors.New("payment failed")
	ErrNodeUnhealthy  = errors.New("lightning node unhealthy")
)

type Service struct {
	cfg             *Config
	pool            *pgxpool.Pool
	lightningClient lnrpc.LightningClient
	routerClient    routerrpc.RouterClient
	conn            *grpc.ClientConn
	routerConn      *grpc.ClientConn
	log             zerolog.Logger
}

type Config struct {
	Host            string
	TLSCertPath     string
	InvoiceMacaroon string
	PaymentMacaroon string
	MaxFeePPM       int64
	MaxFeeFloorSats int64
}

func NewService(cfg interface{}, pool *pgxpool.Pool) *Service {
	c := cfg.(*Config)
	logger := zerolog.New(zerolog.ConsoleWriter{Out: gin.DefaultWriter, TimeFormat: zerolog.TimeFormatUnix}).With().Timestamp().Logger()
	return &Service{
		cfg:  c,
		pool: pool,
		log:  logger,
	}
}

func (s *Service) Connect(ctx context.Context) error {
	tlsCreds, err := credentials.NewClientTLSFromFile(s.cfg.TLSCertPath, "")
	if err != nil {
		return err
	}

	// The invoice macaroon authorizes receive-side calls (invoices, balances,
	// node info); the payment macaroon authorizes the router (payment sends and
	// payment tracking). They travel on separate connections so a
	// least-privilege invoice macaroon can never send funds.
	invoiceCred, err := loadMacaroonCredential(s.cfg.InvoiceMacaroon)
	if err != nil {
		return fmt.Errorf("load invoice macaroon: %w", err)
	}
	paymentCred, err := loadMacaroonCredential(s.cfg.PaymentMacaroon)
	if err != nil {
		return fmt.Errorf("load payment macaroon: %w", err)
	}

	dial := func(cred credentials.PerRPCCredentials) (*grpc.ClientConn, error) {
		opts := []grpc.DialOption{
			grpc.WithTransportCredentials(tlsCreds),
			grpc.WithPerRPCCredentials(cred),
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(50 * 1024 * 1024)),
		}
		return grpc.NewClient(s.cfg.Host, opts...)
	}

	invoiceConn, err := dial(invoiceCred)
	if err != nil {
		return err
	}
	routerConn, err := dial(paymentCred)
	if err != nil {
		_ = invoiceConn.Close()
		return err
	}

	s.conn = invoiceConn
	s.routerConn = routerConn
	s.lightningClient = lnrpc.NewLightningClient(invoiceConn)
	s.routerClient = routerrpc.NewRouterClient(routerConn)
	s.log.Info().Msg("Connected to LND")
	return nil
}

// loadMacaroonCredential reads one serialized macaroon file for gRPC auth.
func loadMacaroonCredential(path string) (credentials.PerRPCCredentials, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	mac := &macaroon.Macaroon{}
	if err := mac.UnmarshalBinary(raw); err != nil {
		return nil, err
	}
	return macaroons.NewMacaroonCredential(mac)
}

func (s *Service) Close() error {
	var firstErr error
	for _, conn := range []*grpc.ClientConn{s.conn, s.routerConn} {
		if conn != nil {
			if err := conn.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	s.conn, s.routerConn = nil, nil
	s.lightningClient, s.routerClient = nil, nil
	return firstErr
}

func (s *Service) Connected() bool {
	return s.lightningClient != nil
}

func (s *Service) CreateInvoice(ctx context.Context, amountSats int64, memo string, expiry time.Duration) (string, []byte, error) {
	if s.lightningClient == nil {
		return "", nil, ErrNodeUnhealthy
	}
	resp, err := s.lightningClient.AddInvoice(ctx, &lnrpc.Invoice{
		Value:  amountSats,
		Memo:   memo,
		Expiry: int64(expiry.Seconds()),
	})
	if err != nil {
		return "", nil, err
	}
	return resp.PaymentRequest, resp.RHash, nil
}

type InvoiceUpdate struct {
	PaymentHash    []byte
	AmountPaidSats int64
	State          string
}

// SubscribeInvoices streams invoice state updates. Callers should reconnect
// this stream when it returns an error.
func (s *Service) SubscribeInvoices(ctx context.Context, handler func(InvoiceUpdate) error) error {
	if s.lightningClient == nil {
		return ErrNodeUnhealthy
	}
	stream, err := s.lightningClient.SubscribeInvoices(ctx, &lnrpc.InvoiceSubscription{})
	if err != nil {
		return err
	}
	for {
		invoice, err := stream.Recv()
		if err != nil {
			return err
		}
		state := ""
		switch invoice.State {
		case lnrpc.Invoice_SETTLED:
			state = "settled"
		case lnrpc.Invoice_CANCELED:
			state = "canceled"
		default:
			continue
		}
		if err := handler(InvoiceUpdate{PaymentHash: invoice.RHash, AmountPaidSats: invoice.AmtPaidSat, State: state}); err != nil {
			return err
		}
	}
}

func (s *Service) PayInvoice(ctx context.Context, invoice string, maxFeeSats int64) ([]byte, error) {
	if s.lightningClient == nil || s.routerClient == nil {
		return nil, ErrNodeUnhealthy
	}
	// Parse the invoice to get the amount and destination
	payReq, err := s.lightningClient.DecodePayReq(ctx, &lnrpc.PayReqString{PayReq: invoice})
	if err != nil {
		return nil, err
	}

	// Use router's SendPaymentV2 for sending payments
	stream, err := s.routerClient.SendPaymentV2(ctx, &routerrpc.SendPaymentRequest{
		PaymentRequest: invoice,
		Amt:            payReq.NumSatoshis,
		FeeLimitSat:    maxFeeSats,
		TimeoutSeconds: 60,
	})
	if err != nil {
		return nil, err
	}

	// Wait for payment to complete
	for {
		payment, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if payment.Status == lnrpc.Payment_SUCCEEDED {
			// PaymentHash is hex string, decode to bytes
			hashBytes, err := hex.DecodeString(payment.PaymentHash)
			if err != nil {
				return nil, err
			}
			return hashBytes, nil
		}
		if payment.Status == lnrpc.Payment_FAILED {
			return nil, errors.New(payment.FailureReason.String())
		}
	}
	return nil, ErrPaymentFailed
}

func (s *Service) GetPaymentStatus(ctx context.Context, paymentHash []byte) (string, error) {
	status, _, err := s.GetPaymentResult(ctx, paymentHash)
	return status, err
}

// GetPaymentResult returns the status and routing fee of a payment attempt.
// Unknown hashes (never sent) return an error from the node.
func (s *Service) GetPaymentResult(ctx context.Context, paymentHash []byte) (string, int64, error) {
	if s.routerClient == nil {
		return "", 0, ErrNodeUnhealthy
	}
	stream, err := s.routerClient.TrackPaymentV2(ctx, &routerrpc.TrackPaymentRequest{
		PaymentHash: paymentHash,
	})
	if err != nil {
		return "", 0, err
	}

	for {
		payment, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", 0, err
		}
		return payment.Status.String(), payment.FeeSat, nil
	}
	return "unknown", 0, nil
}

func (s *Service) GetChannelBalance(ctx context.Context) (int64, error) {
	resp, err := s.lightningClient.ChannelBalance(ctx, &lnrpc.ChannelBalanceRequest{})
	if err != nil {
		return 0, err
	}
	return int64(resp.LocalBalance.Sat), nil
}

// GetTotalBalance returns confirmed on-chain wallet funds plus channel-local
// funds. Unconfirmed wallet funds are excluded from reconciliation assets.
func (s *Service) GetTotalBalance(ctx context.Context) (int64, error) {
	if s.lightningClient == nil {
		return 0, ErrNodeUnhealthy
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	wallet, err := s.lightningClient.WalletBalance(ctx, &lnrpc.WalletBalanceRequest{})
	if err != nil {
		return 0, err
	}
	channels, err := s.lightningClient.ChannelBalance(ctx, &lnrpc.ChannelBalanceRequest{})
	if err != nil {
		return 0, err
	}
	return wallet.ConfirmedBalance + int64(channels.LocalBalance.Sat), nil
}

func (s *Service) GetWalletBalance(ctx context.Context) (int64, error) {
	resp, err := s.lightningClient.WalletBalance(ctx, &lnrpc.WalletBalanceRequest{})
	if err != nil {
		return 0, err
	}
	return int64(resp.TotalBalance), nil
}

func (s *Service) DecodePayReq(ctx context.Context, payReq string) (*lnrpc.PayReq, error) {
	if s.lightningClient == nil {
		return nil, ErrNodeUnhealthy
	}
	return s.lightningClient.DecodePayReq(ctx, &lnrpc.PayReqString{PayReq: payReq})
}

func (s *Service) HealthCheck(ctx context.Context) error {
	_, err := s.lightningClient.GetInfo(ctx, &lnrpc.GetInfoRequest{})
	return err
}
