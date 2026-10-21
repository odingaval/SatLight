package lightning

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// TODO: Re-enable LND gRPC client when compatible version is available
// Currently blocked by LND v0.21.4-beta protobuf compatibility issue
// See: https://github.com/lightningnetwork/lnd/issues/XXXX

var (
	ErrInvoiceExpired = errors.New("invoice expired")
	ErrPaymentFailed  = errors.New("payment failed")
	ErrNodeUnhealthy  = errors.New("lightning node unhealthy")
)

type Service struct {
	cfg  *Config
	pool *pgxpool.Pool
	log  zerolog.Logger
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
	s.log.Info().Msg("Lightning service connect called (stub)")
	return nil
}

func (s *Service) Close() error {
	return nil
}

func (s *Service) CreateInvoice(ctx context.Context, amountSats int64, memo string, expiry time.Duration) (string, []byte, error) {
	s.log.Info().Int64("amount_sats", amountSats).Msg("CreateInvoice called (stub)")
	// Return a fake invoice for development
	return "lnbc" + memo + "placeholder", []byte("placeholder_hash"), nil
}

func (s *Service) PayInvoice(ctx context.Context, invoice string, maxFeeSats int64) ([]byte, error) {
	s.log.Info().Str("invoice", invoice).Int64("max_fee", maxFeeSats).Msg("PayInvoice called (stub)")
	return []byte("placeholder_payment_hash"), nil
}

func (s *Service) GetPaymentStatus(ctx context.Context, paymentHash []byte) (string, error) {
	s.log.Info().Str("payment_hash", string(paymentHash)).Msg("GetPaymentStatus called (stub)")
	return "succeeded", nil
}

func (s *Service) GetChannelBalance(ctx context.Context) (int64, error) {
	return 1000000, nil
}

func (s *Service) GetWalletBalance(ctx context.Context) (int64, error) {
	return 1000000, nil
}

func (s *Service) DecodePayReq(ctx context.Context, payReq string) (interface{}, error) {
	return nil, nil
}

func (s *Service) HealthCheck(ctx context.Context) error {
	return nil
}