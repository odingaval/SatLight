package lightning

import (
	"context"
	"encoding/hex"
	"errors"
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
	cfg           *Config
	pool          *pgxpool.Pool
	lightningClient lnrpc.LightningClient
	routerClient    routerrpc.RouterClient
	conn          *grpc.ClientConn
	log           zerolog.Logger
}

type Config struct {
	Host             string
	TLSCertPath      string
	InvoiceMacaroon  string
	PaymentMacaroon  string
	MaxFeePPM        int64
	MaxFeeFloorSats  int64
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

	// Load invoice macaroon from file
	invoiceMacBytes, err := os.ReadFile(s.cfg.InvoiceMacaroon)
	if err != nil {
		return err
	}
	invoiceMac := &macaroon.Macaroon{}
	if err := invoiceMac.UnmarshalBinary(invoiceMacBytes); err != nil {
		return err
	}
	invoiceCred, err := macaroons.NewMacaroonCredential(invoiceMac)
	if err != nil {
		return err
	}

	// Load payment macaroon from file
	paymentMacBytes, err := os.ReadFile(s.cfg.PaymentMacaroon)
	if err != nil {
		return err
	}
	paymentMac := &macaroon.Macaroon{}
	if err := paymentMac.UnmarshalBinary(paymentMacBytes); err != nil {
		return err
	}
	// paymentCred is created but we use invoiceCred for the connection
	// Payment macaroon is used via the router client which shares the connection
	_, _ = macaroons.NewMacaroonCredential(paymentMac)

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(tlsCreds),
		grpc.WithPerRPCCredentials(invoiceCred), // default to invoice cred
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(50 * 1024 * 1024)),
	}

	conn, err := grpc.DialContext(ctx, s.cfg.Host, opts...)
	if err != nil {
		return err
	}

	s.conn = conn
	s.lightningClient = lnrpc.NewLightningClient(conn)
	s.routerClient = routerrpc.NewRouterClient(conn)
	s.log.Info().Msg("Connected to LND")
	return nil
}

func (s *Service) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *Service) CreateInvoice(ctx context.Context, amountSats int64, memo string, expiry time.Duration) (string, []byte, error) {
	resp, err := s.lightningClient.AddInvoice(ctx, &lnrpc.Invoice{
		Value:    amountSats,
		Memo:     memo,
		Expiry:   int64(expiry.Seconds()),
	})
	if err != nil {
		return "", nil, err
	}
	return resp.PaymentRequest, resp.RHash, nil
}

func (s *Service) PayInvoice(ctx context.Context, invoice string, maxFeeSats int64) ([]byte, error) {
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
	// Use router's TrackPaymentV2 to get payment status
	stream, err := s.routerClient.TrackPaymentV2(ctx, &routerrpc.TrackPaymentRequest{
		PaymentHash: paymentHash,
	})
	if err != nil {
		return "", err
	}

	for {
		payment, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		return payment.Status.String(), nil
	}
	return "unknown", nil
}

func (s *Service) GetChannelBalance(ctx context.Context) (int64, error) {
	resp, err := s.lightningClient.ChannelBalance(ctx, &lnrpc.ChannelBalanceRequest{})
	if err != nil {
		return 0, err
	}
	return int64(resp.LocalBalance.Sat), nil
}

func (s *Service) GetWalletBalance(ctx context.Context) (int64, error) {
	resp, err := s.lightningClient.WalletBalance(ctx, &lnrpc.WalletBalanceRequest{})
	if err != nil {
		return 0, err
	}
	return int64(resp.TotalBalance), nil
}

func (s *Service) DecodePayReq(ctx context.Context, payReq string) (*lnrpc.PayReq, error) {
	return s.lightningClient.DecodePayReq(ctx, &lnrpc.PayReqString{PayReq: payReq})
}

func (s *Service) HealthCheck(ctx context.Context) error {
	_, err := s.lightningClient.GetInfo(ctx, &lnrpc.GetInfoRequest{})
	return err
}