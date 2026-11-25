package config

import (
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

type Config struct {
	AppEnv                       string `mapstructure:"APP_ENV"`
	Network                      string `mapstructure:"NETWORK"`
	HTTPAddr                     string `mapstructure:"HTTP_ADDR"`
	BaseURL                      string `mapstructure:"BASE_URL"`
	WebURL                       string `mapstructure:"WEB_URL"`
	DatabaseURL                  string `mapstructure:"DATABASE_URL"`
	LNDHost                      string `mapstructure:"LND_HOST"`
	LNDTLSCert                   string `mapstructure:"LND_TLS_CERT"`
	LNDInvoiceMacaroon           string `mapstructure:"LND_INVOICE_MACAROON"`
	LNDPaymentMacaroon           string `mapstructure:"LND_PAYMENT_MACAROON"`
	PayoutMaxFeePPM              int    `mapstructure:"PAYOUT_MAX_FEE_PPM"`
	PayoutMaxFeeFloorSats        int    `mapstructure:"PAYOUT_MAX_FEE_FLOOR_SATS"`
	PayoutMergeGraceMinutes      int    `mapstructure:"PAYOUT_MERGE_GRACE_MINUTES"`
	GitHubAppID                  string `mapstructure:"GITHUB_APP_ID"`
	GitHubAppSlug                string `mapstructure:"GITHUB_APP_SLUG"`
	GitHubAppPrivateKeyPath      string `mapstructure:"GITHUB_APP_PRIVATE_KEY_PATH"`
	GitHubWebhookSecret          string `mapstructure:"GITHUB_WEBHOOK_SECRET"`
	GitHubOAuthClientID          string `mapstructure:"GITHUB_OAUTH_CLIENT_ID"`
	GitHubOAuthClientSecret      string `mapstructure:"GITHUB_OAUTH_CLIENT_SECRET"`
	PlatformFeeBPS               int    `mapstructure:"PLATFORM_FEE_BPS"`
	BountyDefaultExpiryDays      int    `mapstructure:"BOUNTY_DEFAULT_EXPIRY_DAYS"`
	ClaimExpiryDays              int    `mapstructure:"CLAIM_EXPIRY_DAYS"`
	ClaimLinkExpiryDays          int    `mapstructure:"CLAIM_LINK_EXPIRY_DAYS"`
	PayoutsPaused                bool   `mapstructure:"PAYOUTS_PAUSED"`
	MinRewardSats                int64  `mapstructure:"MIN_REWARD_SATS"`
	ClaimMinGitHubAccountAgeDays int    `mapstructure:"CLAIM_MIN_GITHUB_ACCOUNT_AGE_DAYS"`
	ClaimMaxActiveClaims         int    `mapstructure:"CLAIM_MAX_ACTIVE_CLAIMS"`
	DailyPayoutCapSats           int64  `mapstructure:"DAILY_PAYOUT_CAP_SATS"`
	SessionSecret                string `mapstructure:"SESSION_SECRET"`
	EncryptionKey                string `mapstructure:"ENCRYPTION_KEY"`
	SMTPURL                      string `mapstructure:"SMTP_URL"`
	EmailFrom                    string `mapstructure:"EMAIL_FROM"`
	LogLevel                     string `mapstructure:"LOG_LEVEL"`
	SentryDSN                    string `mapstructure:"SENTRY_DSN"`
	RateLimitClaimsPerMinute     int    `mapstructure:"RATE_LIMIT_CLAIMS_PER_MINUTE"`
	RateLimitClaimsBurst         int    `mapstructure:"RATE_LIMIT_CLAIMS_BURST"`
	RateLimitDepositsPerMinute   int    `mapstructure:"RATE_LIMIT_DEPOSITS_PER_MINUTE"`
	RateLimitDepositsBurst       int    `mapstructure:"RATE_LIMIT_DEPOSITS_BURST"`
	RateLimitWebhooksPerMinute   int    `mapstructure:"RATE_LIMIT_WEBHOOKS_PER_MINUTE"`
	RateLimitWebhooksBurst       int    `mapstructure:"RATE_LIMIT_WEBHOOKS_BURST"`
	RateLimitLoginPerMinute      int    `mapstructure:"RATE_LIMIT_LOGIN_PER_MINUTE"`
	RateLimitLoginBurst          int    `mapstructure:"RATE_LIMIT_LOGIN_BURST"`
	RateLimitWaitlistPerMinute   int    `mapstructure:"RATE_LIMIT_WAITLIST_PER_MINUTE"`
	RateLimitWaitlistBurst       int    `mapstructure:"RATE_LIMIT_WAITLIST_BURST"`
	ServeStatic                  bool   `mapstructure:"SERVE_STATIC"`
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	for _, key := range []string{
		"APP_ENV", "NETWORK", "HTTP_ADDR", "BASE_URL", "WEB_URL", "DATABASE_URL",
		"LND_HOST", "LND_TLS_CERT", "LND_INVOICE_MACAROON", "LND_PAYMENT_MACAROON",
		"PAYOUT_MAX_FEE_PPM", "PAYOUT_MAX_FEE_FLOOR_SATS", "PAYOUT_MERGE_GRACE_MINUTES", "GITHUB_APP_ID", "GITHUB_APP_SLUG",
		"GITHUB_APP_PRIVATE_KEY_PATH", "GITHUB_WEBHOOK_SECRET", "GITHUB_OAUTH_CLIENT_ID",
		"GITHUB_OAUTH_CLIENT_SECRET", "PLATFORM_FEE_BPS", "BOUNTY_DEFAULT_EXPIRY_DAYS",
		"CLAIM_EXPIRY_DAYS", "CLAIM_LINK_EXPIRY_DAYS", "PAYOUTS_PAUSED", "MIN_REWARD_SATS", "SESSION_SECRET",
		"ENCRYPTION_KEY", "SMTP_URL", "EMAIL_FROM", "LOG_LEVEL", "SENTRY_DSN",
		"CLAIM_MIN_GITHUB_ACCOUNT_AGE_DAYS", "DAILY_PAYOUT_CAP_SATS",
		"CLAIM_MAX_ACTIVE_CLAIMS", "RATE_LIMIT_CLAIMS_PER_MINUTE",
		"RATE_LIMIT_CLAIMS_BURST", "RATE_LIMIT_DEPOSITS_PER_MINUTE",
		"RATE_LIMIT_DEPOSITS_BURST", "RATE_LIMIT_WEBHOOKS_PER_MINUTE",
		"RATE_LIMIT_WEBHOOKS_BURST", "RATE_LIMIT_LOGIN_PER_MINUTE",
		"RATE_LIMIT_LOGIN_BURST", "RATE_LIMIT_WAITLIST_PER_MINUTE",
		"RATE_LIMIT_WAITLIST_BURST", "SERVE_STATIC",
	} {
		_ = viper.BindEnv(strings.ToLower(key), key)
	}
	viper.SetDefault("APP_ENV", "development")
	viper.SetDefault("NETWORK", "regtest")
	viper.SetDefault("HTTP_ADDR", ":8080")
	viper.SetDefault("BASE_URL", "http://localhost:8080")
	viper.SetDefault("WEB_URL", "http://localhost:3000")
	viper.SetDefault("PLATFORM_FEE_BPS", 200)
	viper.SetDefault("BOUNTY_DEFAULT_EXPIRY_DAYS", 90)
	viper.SetDefault("CLAIM_EXPIRY_DAYS", 7)
	viper.SetDefault("CLAIM_LINK_EXPIRY_DAYS", 30)
	viper.SetDefault("PAYOUTS_PAUSED", false)
	viper.SetDefault("MIN_REWARD_SATS", 500)
	viper.SetDefault("CLAIM_MIN_GITHUB_ACCOUNT_AGE_DAYS", 0)
	viper.SetDefault("CLAIM_MAX_ACTIVE_CLAIMS", 3)
	viper.SetDefault("DAILY_PAYOUT_CAP_SATS", 1000000)
	viper.SetDefault("LOG_LEVEL", "debug")
	viper.SetDefault("PAYOUT_MAX_FEE_PPM", 10000)
	viper.SetDefault("PAYOUT_MAX_FEE_FLOOR_SATS", 10)
	viper.SetDefault("PAYOUT_MERGE_GRACE_MINUTES", 10)
	viper.SetDefault("RATE_LIMIT_CLAIMS_PER_MINUTE", 30)
	viper.SetDefault("RATE_LIMIT_CLAIMS_BURST", 10)
	viper.SetDefault("RATE_LIMIT_DEPOSITS_PER_MINUTE", 30)
	viper.SetDefault("RATE_LIMIT_DEPOSITS_BURST", 10)
	viper.SetDefault("RATE_LIMIT_WEBHOOKS_PER_MINUTE", 600)
	viper.SetDefault("RATE_LIMIT_WEBHOOKS_BURST", 100)
	viper.SetDefault("RATE_LIMIT_LOGIN_PER_MINUTE", 30)
	viper.SetDefault("RATE_LIMIT_LOGIN_BURST", 10)
	viper.SetDefault("RATE_LIMIT_WAITLIST_PER_MINUTE", 20)
	viper.SetDefault("RATE_LIMIT_WAITLIST_BURST", 5)

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// Render injects the public service URL; use it for callbacks and
	// redirects unless BASE_URL/WEB_URL are set explicitly.
	if renderURL := os.Getenv("RENDER_EXTERNAL_URL"); renderURL != "" {
		if os.Getenv("BASE_URL") == "" {
			cfg.BaseURL = renderURL
		}
		if os.Getenv("WEB_URL") == "" {
			cfg.WebURL = renderURL
		}
	}

	// Hosts like Render assign the port at runtime via PORT and
	// health-check it; an explicit HTTP_ADDR still wins when set.
	if port := os.Getenv("PORT"); port != "" && os.Getenv("HTTP_ADDR") == "" {
		cfg.HTTPAddr = ":" + port
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

func (c *Config) ClaimLinkExpiryDuration() time.Duration {
	if c.ClaimLinkExpiryDays <= 0 {
		return 30 * 24 * time.Hour
	}
	return time.Duration(c.ClaimLinkExpiryDays) * 24 * time.Hour
}

func (c *Config) BountyDefaultExpiryDuration() time.Duration {
	return time.Duration(c.BountyDefaultExpiryDays) * 24 * time.Hour
}

// PayoutMergeGraceDuration is how long after a merge automatic payouts wait
// so a force-revert lands first. Zero or negative disables the wait (tests
// and operators who accept the revert risk).
func (c *Config) PayoutMergeGraceDuration() time.Duration {
	if c.PayoutMergeGraceMinutes <= 0 {
		return 0
	}
	return time.Duration(c.PayoutMergeGraceMinutes) * time.Minute
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
