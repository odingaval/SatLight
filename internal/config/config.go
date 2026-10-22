package config

import (
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

type Config struct {
	AppEnv                  string `mapstructure:"APP_ENV"`
	Network                 string `mapstructure:"NETWORK"`
	HTTPAddr                string `mapstructure:"HTTP_ADDR"`
	BaseURL                 string `mapstructure:"BASE_URL"`
	WebURL                  string `mapstructure:"WEB_URL"`
	DatabaseURL             string `mapstructure:"DATABASE_URL"`
	LNDHost                 string `mapstructure:"LND_HOST"`
	LNDTLSCert              string `mapstructure:"LND_TLS_CERT"`
	LNDInvoiceMacaroon      string `mapstructure:"LND_INVOICE_MACAROON"`
	LNDPaymentMacaroon      string `mapstructure:"LND_PAYMENT_MACAROON"`
	PayoutMaxFeePPM         int    `mapstructure:"PAYOUT_MAX_FEE_PPM"`
	PayoutMaxFeeFloorSats   int    `mapstructure:"PAYOUT_MAX_FEE_FLOOR_SATS"`
	GitHubAppID             string `mapstructure:"GITHUB_APP_ID"`
	GitHubAppPrivateKeyPath string `mapstructure:"GITHUB_APP_PRIVATE_KEY_PATH"`
	GitHubWebhookSecret     string `mapstructure:"GITHUB_WEBHOOK_SECRET"`
	GitHubOAuthClientID     string `mapstructure:"GITHUB_OAUTH_CLIENT_ID"`
	GitHubOAuthClientSecret string `mapstructure:"GITHUB_OAUTH_CLIENT_SECRET"`
	PlatformFeeBPS          int    `mapstructure:"PLATFORM_FEE_BPS"`
	BountyDefaultExpiryDays int    `mapstructure:"BOUNTY_DEFAULT_EXPIRY_DAYS"`
	ClaimExpiryDays         int    `mapstructure:"CLAIM_EXPIRY_DAYS"`
	PayoutsPaused           bool   `mapstructure:"PAYOUTS_PAUSED"`
	MinRewardSats           int64  `mapstructure:"MIN_REWARD_SATS"`
	SessionSecret           string `mapstructure:"SESSION_SECRET"`
	EncryptionKey           string `mapstructure:"ENCRYPTION_KEY"`
	SMTPURL                 string `mapstructure:"SMTP_URL"`
	EmailFrom               string `mapstructure:"EMAIL_FROM"`
	LogLevel                string `mapstructure:"LOG_LEVEL"`
	SentryDSN               string `mapstructure:"SENTRY_DSN"`
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	viper.AutomaticEnv()
	viper.SetDefault("APP_ENV", "development")
	viper.SetDefault("NETWORK", "regtest")
	viper.SetDefault("HTTP_ADDR", ":8080")
	viper.SetDefault("BASE_URL", "http://localhost:8080")
	viper.SetDefault("WEB_URL", "http://localhost:3000")
	viper.SetDefault("PLATFORM_FEE_BPS", 200)
	viper.SetDefault("BOUNTY_DEFAULT_EXPIRY_DAYS", 90)
	viper.SetDefault("CLAIM_EXPIRY_DAYS", 7)
	viper.SetDefault("PAYOUTS_PAUSED", false)
	viper.SetDefault("MIN_REWARD_SATS", 500)
	viper.SetDefault("LOG_LEVEL", "debug")
	viper.SetDefault("PAYOUT_MAX_FEE_PPM", 10000)
	viper.SetDefault("PAYOUT_MAX_FEE_FLOOR_SATS", 10)

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	if cfg.DatabaseURL == "" {
		return nil, ErrMissingConfig("DATABASE_URL")
	}
	if cfg.SessionSecret == "change-me" || cfg.SessionSecret == "" {
		return nil, ErrMissingConfig("SESSION_SECRET")
	}
	if cfg.EncryptionKey == "change-me" || cfg.EncryptionKey == "" {
		return nil, ErrMissingConfig("ENCRYPTION_KEY")
	}

	return &cfg, nil
}

func (c *Config) IsProduction() bool {
	return c.AppEnv == "production"
}

func (c *Config) ClaimExpiryDuration() time.Duration {
	return time.Duration(c.ClaimExpiryDays) * 24 * time.Hour
}

func (c *Config) BountyDefaultExpiryDuration() time.Duration {
	return time.Duration(c.BountyDefaultExpiryDays) * 24 * time.Hour
}

type ErrMissingConfig string

func (e ErrMissingConfig) Error() string {
	return "missing required config: " + string(e)
}

func (e ErrMissingConfig) Is(target error) bool {
	_, ok := target.(ErrMissingConfig)
	return ok
}

func MustLoad() *Config {
	cfg, err := Load()
	if err != nil {
		_, _ = os.Stderr.WriteString("config error: " + err.Error() + "\n")
		os.Exit(1)
	}
	return cfg
}
