package lightning

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lightningnetwork/lnd/lnrpc"
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
	cfg    *Config
	pool   *pgxpool.Pool
	client lnrpc.LightningClient
	conn   *grpc.ClientConn
	log    *zerolog.Logger
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
	return &Service{
		cfg:  c,
		pool: pool,
		log:  zerolog.New(zerolog.ConsoleWriter{Out: zerolog.ConsoleWriter{Out: zerolog.ConsoleWriter{Out: zerolog.ConsoleWriter{Out: zerolog.ConsoleWriter{Out: gin.DefaultWriter, TimeFormat: zerolog.TimeFormatUnix}}}, TimeFormat: zerolog.TimeFormatUnix}).With().Timestamp().Logger(),
	}
}

func (s *Service) Connect(ctx context.Context) error {
	tlsCreds, err := credentials.NewClientTLSFromFile(s.cfg.TLSCertPath, "")
	if err != nil {
		return err
	}

	invoiceMac, err := macaroons.NewMacaroonCredential(s.cfg.InvoiceMacaroon)
	if err != nil {
		return err
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(tlsCreds),
		grpc.WithPerRPCCredentials(invoiceMac),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(50 * 1024 * 1024)),
	}

	conn, err := grpc.DialContext(ctx, s.cfg.Host, opts...)
	if err != nil {
		return err
	}

	s.conn = conn
	s.client = lnrpc.NewLightningClient(conn)
	return nil
}

func (s *Service) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *Service) CreateInvoice(ctx context.Context, amountSats int64, memo string, expiry time.Duration) (string, []byte, error) {
	resp, err := s.client.AddInvoice(ctx, &lnrpc.Invoice{
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
	resp, err := s.client.SendPaymentSync(ctx, &lnrpc.SendRequest{
		PaymentRequest: invoice,
		FeeLimitSat:    maxFeeSats,
		TimeoutSeconds: 60,
	})
	if err != nil {
		return nil, err
	}
	if resp.PaymentError != "" {
		return nil, errors.New(resp.PaymentError)
	}
	return resp.PaymentHash, nil
}

func (s *Service) GetPaymentStatus(ctx context.Context, paymentHash []byte) (string, error) {
	resp, err := s.client.GetPaymentStatus(ctx, &lnrpc.PaymentHash{RHash: paymentHash})
	if err != nil {
		return "", err
	}
	return resp.Status.String(), nil
}

func (s *Service) GetChannelBalance(ctx context.Context) (int64, error) {
	resp, err := s.client.ChannelBalance(ctx, &lnrpc.ChannelBalanceRequest{})
	if err != nil {
		return 0, err
	}
	return resp.LocalBalance.Sat, nil
}

func (s *Service) GetWalletBalance(ctx context.Context) (int64, error) {
	resp, err := s.client.WalletBalance(ctx, &lnrpc.WalletBalanceRequest{})
	if err != nil {
		return 0, err
	}
	return resp.TotalBalance, nil
}

func (s *Service) DecodePayReq(ctx context.Context, payReq string) (*lnrpc.PayReq, error) {
	return s.client.DecodePayReq(ctx, &lnrpc.PayReqString{PayReq: payReq})
}

func (s *Service) HealthCheck(ctx context.Context) error {
	_, err := s.client.GetInfo(ctx, &lnrpc.GetInfoRequest{})
	return err
}