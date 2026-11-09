package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/satlight/satlight/internal/auth"
	"github.com/satlight/satlight/internal/bounty"
	"github.com/satlight/satlight/internal/config"
	"github.com/satlight/satlight/internal/github"
	"github.com/satlight/satlight/internal/ledger"
	"github.com/satlight/satlight/internal/lightning"
	"github.com/satlight/satlight/internal/notify"
	"github.com/satlight/satlight/internal/payout"
	"github.com/satlight/satlight/internal/queue"
)

type Server struct {
	cfg      *config.Config
	pool     *pgxpool.Pool
	router   *gin.Engine
	logger   zerolog.Logger
	services *Services
}

type Services struct {
	Auth      *auth.Service
	Bounty    *bounty.Service
	Ledger    *ledger.Service
	Lightning *lightning.Service
	GitHub    *github.Service
	Queue     *queue.Client
	Notify    *notify.Service
	Payout    *payout.Service
}

func NewServer(cfg *config.Config, pool *pgxpool.Pool) *Server {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: gin.DefaultWriter, TimeFormat: zerolog.TimeFormatUnix}).With().Timestamp().Logger()

	authSvc := auth.NewService(cfg, pool)
	bountySvc := bounty.NewService(cfg, pool)
	ledgerSvc := ledger.NewService(cfg, pool)
	lightningSvc := lightning.NewService(&lightning.Config{
		Host:            cfg.LNDHost,
		TLSCertPath:     cfg.LNDTLSCert,
		InvoiceMacaroon: cfg.LNDInvoiceMacaroon,
		PaymentMacaroon: cfg.LNDPaymentMacaroon,
		MaxFeePPM:       int64(cfg.PayoutMaxFeePPM),
		MaxFeeFloorSats: int64(cfg.PayoutMaxFeeFloorSats),
	}, pool)
	ledgerSvc.SetNodeBalanceProvider(lightningSvc)
	appID, err := strconv.ParseInt(cfg.GitHubAppID, 10, 64)
	if err != nil && cfg.GitHubAppID != "" {
		logger.Warn().Err(err).Msg("invalid GitHub App ID")
	}
	githubSvc := github.NewService(&github.Config{
		AppID:             appID,
		PrivateKeyPath:    cfg.GitHubAppPrivateKeyPath,
		WebhookSecret:     cfg.GitHubWebhookSecret,
		OAuthClientID:     cfg.GitHubOAuthClientID,
		OAuthClientSecret: cfg.GitHubOAuthClientSecret,
	}, pool)
	queueClient := queue.NewClient(pool)
	notifySvc := notify.NewService(cfg, pool)
	payoutSvc := payout.NewService(cfg, pool)
	payoutSvc.SetDependencies(ledgerSvc, lightningSvc, githubSvc, notifySvc, queueClient)

	bountySvc.SetDependencies(ledgerSvc, queueClient)
	bountySvc.SetNotifier(notifySvc)
	githubSvc.SetQueue(queueClient)
	githubSvc.SetNotifier(notifySvc)

	services := &Services{
		Auth:      authSvc,
		Bounty:    bountySvc,
		Ledger:    ledgerSvc,
		Lightning: lightningSvc,
		GitHub:    githubSvc,
		Queue:     queueClient,
		Notify:    notifySvc,
		Payout:    payoutSvc,
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(zerologMiddleware(&logger))
	router.Use(corsMiddleware(cfg.WebURL))

	s := &Server{
		cfg:      cfg,
		pool:     pool,
		router:   router,
		logger:   logger,
		services: services,
	}

	s.setupRoutes()
	return s
}

func (s *Server) Router() *gin.Engine {
	return s.router
}

func (s *Server) ConnectLightning(ctx context.Context) error {
	return s.services.Lightning.Connect(ctx)
}

func (s *Server) CloseLightning() error {
	return s.services.Lightning.Close()
}

// WatchLightningInvoices reconnects the invoice stream after transient LND or
// network failures and settles matching project deposits.
func (s *Server) WatchLightningInvoices(ctx context.Context) {
	wasConnected := s.services.Lightning.Connected()
	for ctx.Err() == nil {
		if !s.services.Lightning.Connected() {
			if err := s.ConnectLightning(ctx); err != nil {
				s.logger.Warn().Err(err).Msg("could not connect to Lightning node; retrying")
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
				continue
			}
		}
		if !wasConnected {
			wasConnected = true
			// Deposits or payments may have settled while disconnected;
			// reconcile immediately instead of waiting for the hourly job.
			if err := s.services.Ledger.RunReconciliation(ctx); err != nil {
				s.logger.Warn().Err(err).Msg("post-reconnect reconciliation reported a problem")
			}
		}
		err := s.services.Lightning.SubscribeInvoices(ctx, func(update lightning.InvoiceUpdate) error {
			switch update.State {
			case "settled":
				err := s.services.Bounty.SettleDeposit(ctx, update.PaymentHash, update.AmountPaidSats)
				if errors.Is(err, bounty.ErrDepositNotFound) {
					if err := s.services.Bounty.RecordUnmatchedReceipt(ctx, update.PaymentHash, update.AmountPaidSats); err != nil {
						return err
					}
					return nil
				}
				return err
			case "canceled":
				return s.services.Bounty.MarkDepositExpired(ctx, update.PaymentHash)
			default:
				return nil
			}
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.logger.Warn().Err(err).Msg("Lightning invoice stream interrupted; reconnecting")
			wasConnected = false
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *Server) setupRoutes() {
	s.router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	s.router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	v1 := s.router.Group("/api/v1")
	{
		v1.GET("/auth/github", s.handleGitHubLogin)
		v1.GET("/auth/github/callback", s.handleGitHubCallback)
		v1.POST("/waitlist", s.handleWaitlist)

		v1.GET("/bounties", s.handleListBounties)
		v1.GET("/bounties/:id", s.handleGetBounty)
		v1.POST("/bounties/:id/claim", s.authRequired(), s.handleClaimBounty)
		v1.DELETE("/bounties/:id/claim", s.authRequired(), s.handleReleaseClaim)
		v1.POST("/bounties/:id/cancel", s.authRequired(), s.handleCancelBounty)
		v1.POST("/bounties/:id/dispute", s.authRequired(), s.handleDisputeBounty)
		v1.POST("/disputes/:id/resolve", s.authRequired(), s.handleResolveDispute)

		v1.POST("/projects", s.authRequired(), s.handleCreateProject)
		v1.GET("/projects/:id/wallet", s.authRequired(), s.handleGetWallet)
		v1.POST("/projects/:id/deposits", s.authRequired(), s.handleCreateDeposit)
		v1.POST("/projects/:id/withdrawals", s.authRequired(), s.handleCreateWithdrawal)
		v1.POST("/projects/:id/bounties", s.authRequired(), s.handleCreateBounty)
		v1.PUT("/projects/:id/notifications", s.authRequired(), s.handleUpdateProjectNotifications)

		v1.GET("/me", s.authRequired(), s.handleGetMe)
		v1.PUT("/me/payout-destination", s.authRequired(), s.handleSetPayoutDestination)
		v1.GET("/me/payouts", s.authRequired(), s.handleGetPayouts)
		v1.GET("/me/claims", s.authRequired(), s.handleGetMyClaims)

		v1.GET("/payouts/claim/:token", s.handleRedeemPayoutClaim)
		v1.GET("/payouts/claim/callback", s.handleWithdrawCallback)

		v1.POST("/webhooks/github", s.handleGitHubWebhook)
	}
}

func (s *Server) handleGitHubLogin(c *gin.Context) {
	if s.cfg.GitHubOAuthClientID == "" || s.cfg.GitHubOAuthClientSecret == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "GITHUB_OAUTH_UNAVAILABLE", "message": "GitHub sign-in is not configured"}})
		return
	}

	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		s.logger.Error().Err(err).Msg("failed to create GitHub OAuth state")
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "could not start GitHub sign-in"}})
		return
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("github_oauth_state", state, 600, "/api/v1/auth/github", "", s.cfg.IsProduction(), true)
	redirectURL := strings.TrimRight(s.cfg.BaseURL, "/") + "/api/v1/auth/github/callback"
	c.Redirect(http.StatusFound, s.services.GitHub.GetOAuthURL(state, redirectURL))
}

func (s *Server) handleGitHubCallback(c *gin.Context) {
	state, cookieErr := c.Cookie("github_oauth_state")
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("github_oauth_state", "", -1, "/api/v1/auth/github", "", s.cfg.IsProduction(), true)
	if cookieErr != nil || c.Query("state") == "" || !hmac.Equal([]byte(state), []byte(c.Query("state"))) {
		c.Redirect(http.StatusFound, strings.TrimRight(s.cfg.WebURL, "/")+"/?github=state_error#register")
		return
	}
	if code := c.Query("code"); code == "" {
		c.Redirect(http.StatusFound, strings.TrimRight(s.cfg.WebURL, "/")+"/?github=denied#register")
		return
	}

	ctx := c.Request.Context()
	redirectURL := strings.TrimRight(s.cfg.BaseURL, "/") + "/api/v1/auth/github/callback"
	token, err := s.services.GitHub.ExchangeCode(ctx, c.Query("code"), redirectURL)
	if err != nil {
		s.logger.Warn().Err(err).Msg("GitHub OAuth code exchange failed")
		c.Redirect(http.StatusFound, strings.TrimRight(s.cfg.WebURL, "/")+"/?github=failed#register")
		return
	}
	githubUser, err := s.services.GitHub.GetUser(ctx, token)
	if err != nil || githubUser.GetID() == 0 || githubUser.GetLogin() == "" {
		s.logger.Warn().Err(err).Msg("failed to load GitHub user")
		c.Redirect(http.StatusFound, strings.TrimRight(s.cfg.WebURL, "/")+"/?github=failed#register")
		return
	}
	userID, err := s.services.Auth.UpsertUser(ctx, githubUser.GetID(), githubUser.GetLogin(), githubUser.GetEmail(), githubUser.GetCreatedAt().Time)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to save GitHub user")
		c.Redirect(http.StatusFound, strings.TrimRight(s.cfg.WebURL, "/")+"/?github=failed#register")
		return
	}
	session, err := s.services.Auth.GenerateToken(userID)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to create user session")
		c.Redirect(http.StatusFound, strings.TrimRight(s.cfg.WebURL, "/")+"/?github=failed#register")
		return
	}
	s.services.Auth.SetSessionCookie(c, session)
	c.Redirect(http.StatusFound, strings.TrimRight(s.cfg.WebURL, "/")+"/?github=connected#register")
}

func (s *Server) authRequired() gin.HandlerFunc {
	return s.services.Auth.Middleware()
}

// idempotencyKey validates the Idempotency-Key header required by all
// money-moving POST endpoints. It writes the 400 response on failure.
func idempotencyKey(c *gin.Context) (string, bool) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if len(key) < 8 || len(key) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_IDEMPOTENCY_KEY", "message": "Idempotency-Key must contain 8 to 128 characters"}})
		return "", false
	}
	return key, true
}

// requestHash returns a stable hash of a bound request struct so that key
// reuse with different request data can be rejected (HTTP 409).
func requestHash(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func zerologMiddleware(logger *zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		c.Next()
		logger.Info().
			Str("method", c.Request.Method).
			Str("path", path).
			Int("status", c.Writer.Status()).
			Dur("latency", time.Since(start)).
			Msg("request")
	}
}

func corsMiddleware(allowedOrigin string) gin.HandlerFunc {
	if allowedOrigin == "" {
		allowedOrigin = "*"
	}
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// Handlers

func (s *Server) handleWaitlist(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
		Role  string `json:"role" binding:"omitempty,oneof=maintainer contributor"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_REQUEST", "message": err.Error()}})
		return
	}

	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO waitlist (email, role) VALUES ($1, $2)
		ON CONFLICT (email) DO UPDATE SET role = EXCLUDED.role
	`, req.Email, req.Role)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to add to waitlist")
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to add to waitlist"}})
		return
	}

	c.Status(http.StatusNoContent)
}

func (s *Server) handleListBounties(c *gin.Context) {
	filters := bounty.ListFilters{
		Query:      c.Query("q"),
		Language:   c.Query("language"),
		Difficulty: c.Query("difficulty"),
		Limit:      20,
	}
	if minReward := c.Query("min_reward"); minReward != "" {
		if v, err := strconv.ParseInt(minReward, 10, 64); err == nil {
			filters.MinReward = v
		}
	}
	if maxReward := c.Query("max_reward"); maxReward != "" {
		if v, err := strconv.ParseInt(maxReward, 10, 64); err == nil {
			filters.MaxReward = v
		}
	}
	if cursor := c.Query("cursor"); cursor != "" {
		filters.Cursor = cursor
	}
	if limit := c.Query("limit"); limit != "" {
		if v, err := strconv.Atoi(limit); err == nil && v > 0 && v <= 100 {
			filters.Limit = v
		}
	}

	bounties, nextCursor, err := s.services.Bounty.ListBounties(c.Request.Context(), filters)
	if err != nil {
		if errors.Is(err, bounty.ErrInvalidCursor) {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_CURSOR", "message": "invalid bounty cursor"}})
			return
		}
		s.logger.Error().Err(err).Msg("failed to list bounties")
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to list bounties"}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":       bounties,
		"next_cursor": nextCursor,
	})
}

func (s *Server) handleGetBounty(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ID", "message": "invalid bounty id"}})
		return
	}

	b, err := s.services.Bounty.GetBounty(c.Request.Context(), id)
	if err != nil {
		if err == bounty.ErrBountyNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "NOT_FOUND", "message": "bounty not found"}})
			return
		}
		s.logger.Error().Err(err).Msg("failed to get bounty")
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to get bounty"}})
		return
	}

	c.JSON(http.StatusOK, b)
}

func (s *Server) handleClaimBounty(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ID", "message": "invalid bounty id"}})
		return
	}

	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "unauthorized"}})
		return
	}

	key, ok := idempotencyKey(c)
	if !ok {
		return
	}

	claim, created, err := s.services.Bounty.ClaimBounty(c.Request.Context(), id, userID, key)
	if err != nil {
		switch err {
		case bounty.ErrBountyNotFound:
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "NOT_FOUND", "message": "bounty not found"}})
		case bounty.ErrBountyNotOpen:
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "NOT_OPEN", "message": "bounty is not open for claims"}})
		case bounty.ErrBountyAlreadyClaimed:
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "ALREADY_CLAIMED", "message": "bounty already claimed"}})
		case bounty.ErrUserBanned:
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "USER_BANNED", "message": "this account is banned from claiming bounties"}})
		case bounty.ErrProjectFrozen:
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "PROJECT_FROZEN", "message": "this project is frozen and not accepting claims"}})
		case bounty.ErrSelfClaim:
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "SELF_CLAIM", "message": "project owners cannot claim their own bounties"}})
		case bounty.ErrClaimantAccountTooNew:
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"code": "ACCOUNT_TOO_NEW", "message": "this GitHub account is too new to claim bounties"}})
		case bounty.ErrTooManyActiveClaims:
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"code": "TOO_MANY_CLAIMS", "message": "release or complete an active claim before claiming another bounty"}})
		case bounty.ErrPayoutDestinationMissing:
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"code": "PAYOUT_DESTINATION_MISSING", "message": "set a Lightning payout destination before claiming"}})
		default:
			s.logger.Error().Err(err).Msg("failed to claim bounty")
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to claim bounty"}})
		}
		return
	}

	if created {
		c.JSON(http.StatusCreated, claim)
	} else {
		c.JSON(http.StatusOK, claim)
	}
}

func (s *Server) handleReleaseClaim(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ID", "message": "invalid bounty id"}})
		return
	}

	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "unauthorized"}})
		return
	}

	if err := s.services.Bounty.ReleaseClaim(c.Request.Context(), id, userID); err != nil {
		s.logger.Error().Err(err).Msg("failed to release claim")
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to release claim"}})
		return
	}

	c.Status(http.StatusNoContent)
}

func (s *Server) handleCancelBounty(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ID", "message": "invalid bounty id"}})
		return
	}

	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "unauthorized"}})
		return
	}

	project, err := s.getUserProject(c.Request.Context(), userID, id)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "not authorized to cancel this bounty"}})
		return
	}

	bountyResult, err := s.services.Bounty.CancelBounty(c.Request.Context(), id, project.ID)
	if err != nil {
		switch err {
		case bounty.ErrBountyNotFound:
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "NOT_FOUND", "message": "bounty not found"}})
		case bounty.ErrInvalidState:
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "INVALID_STATE", "message": "bounty cannot be cancelled in current state"}})
		default:
			s.logger.Error().Err(err).Msg("failed to cancel bounty")
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to cancel bounty"}})
		}
		return
	}

	c.JSON(http.StatusOK, bountyResult)
}

func (s *Server) handleDisputeBounty(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ID", "message": "invalid bounty id"}})
		return
	}

	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "unauthorized"}})
		return
	}

	var req struct {
		Reason   string `json:"reason" binding:"required"`
		Evidence string `json:"evidence"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_REQUEST", "message": err.Error()}})
		return
	}

	if err := s.services.Bounty.DisputeBounty(c.Request.Context(), id, userID, req.Reason, req.Evidence); err != nil {
		switch err {
		case bounty.ErrBountyNotFound:
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "NOT_FOUND", "message": "bounty not found"}})
		case bounty.ErrInvalidState:
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "INVALID_STATE", "message": "bounty cannot be disputed in its current state"}})
		default:
			s.logger.Error().Err(err).Msg("failed to create dispute")
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to create dispute"}})
		}
		return
	}

	c.Status(http.StatusCreated)
}

func (s *Server) handleResolveDispute(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ID", "message": "invalid dispute id"}})
		return
	}

	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "unauthorized"}})
		return
	}

	var req struct {
		Resolution string `json:"resolution" binding:"required,oneof=resolved_pay resolved_cancel resolved_other"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_REQUEST", "message": err.Error()}})
		return
	}

	err = s.services.Bounty.ResolveDispute(c.Request.Context(), id, userID, req.Resolution)
	switch err {
	case nil:
		c.JSON(http.StatusOK, gin.H{"id": id, "status": req.Resolution})
	case bounty.ErrDisputeNotFound:
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "NOT_FOUND", "message": "dispute not found"}})
	case bounty.ErrNotAuthorized:
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "only the project owner can resolve this dispute"}})
	case bounty.ErrDisputeClosed:
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "DISPUTE_CLOSED", "message": "dispute is already resolved"}})
	case bounty.ErrInvalidState:
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "INVALID_STATE", "message": "bounty is no longer disputed"}})
	case bounty.ErrDisputePayNotReady:
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "NO_VERIFIED_MERGE", "message": "cannot release payout without a verified merge"}})
	default:
		s.logger.Error().Err(err).Msg("failed to resolve dispute")
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to resolve dispute"}})
	}
}

func (s *Server) handleCreateProject(c *gin.Context) {
	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "unauthorized"}})
		return
	}

	var req struct {
		RepoFullName   string `json:"repo_full_name" binding:"required"`
		InstallationID int64  `json:"installation_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_REQUEST", "message": err.Error()}})
		return
	}

	_, githubLogin, _, _, err := s.services.Auth.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "could not load the signed-in GitHub user"}})
		return
	}
	isAdmin, err := s.services.GitHub.VerifyRepoAdmin(c.Request.Context(), req.InstallationID, githubLogin, req.RepoFullName)
	if err != nil || !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "not an admin of the repository"}})
		return
	}

	var project struct {
		ID                   int64     `json:"id"`
		OwnerUserID          int64     `json:"owner_user_id"`
		GitHubInstallationID int64     `json:"github_installation_id"`
		RepoFullName         string    `json:"repo_full_name"`
		Status               string    `json:"status"`
		CreatedAt            time.Time `json:"created_at"`
	}
	err = s.pool.QueryRow(c.Request.Context(), `
		INSERT INTO projects (owner_user_id, github_installation_id, repo_full_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (repo_full_name) DO UPDATE SET owner_user_id = EXCLUDED.owner_user_id, github_installation_id = EXCLUDED.github_installation_id
		RETURNING id, owner_user_id, github_installation_id, repo_full_name, status, created_at
	`, userID, req.InstallationID, req.RepoFullName).Scan(&project.ID, &project.OwnerUserID, &project.GitHubInstallationID, &project.RepoFullName, &project.Status, &project.CreatedAt)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to create project")
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to create project"}})
		return
	}

	_, err = s.pool.Exec(c.Request.Context(), `
		INSERT INTO wallets (project_id, available_sats, reserved_sats)
		VALUES ($1, 0, 0)
		ON CONFLICT (project_id) DO NOTHING
	`, project.ID)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to create wallet")
	}

	c.JSON(http.StatusCreated, project)
}

func (s *Server) handleUpdateProjectNotifications(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ID", "message": "invalid project id"}})
		return
	}
	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "unauthorized"}})
		return
	}
	project, err := s.getProjectForUser(c.Request.Context(), userID, id)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "not authorized"}})
		return
	}
	var req struct {
		WebhookURL    *string `json:"webhook_url"`
		WebhookSecret *string `json:"webhook_secret"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_REQUEST", "message": err.Error()}})
		return
	}
	var urlVal, secretVal any
	if req.WebhookURL != nil && strings.TrimSpace(*req.WebhookURL) != "" {
		u, err := url.Parse(strings.TrimSpace(*req.WebhookURL))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_URL", "message": "webhook_url must be an http(s) URL"}})
			return
		}
		urlVal = u.String()
	}
	if req.WebhookSecret != nil && *req.WebhookSecret != "" {
		secretVal = *req.WebhookSecret
	}
	if _, err := s.pool.Exec(c.Request.Context(), `
		UPDATE projects SET notification_webhook_url = $1, notification_webhook_secret = $2 WHERE id = $3
	`, urlVal, secretVal, project.ID); err != nil {
		s.logger.Error().Err(err).Msg("failed to update project notifications")
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to update project notifications"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": project.ID, "webhook_configured": urlVal != nil})
}

func (s *Server) handleGetWallet(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ID", "message": "invalid project id"}})
		return
	}

	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "unauthorized"}})
		return
	}

	project, err := s.getProjectForUser(c.Request.Context(), userID, id)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "not authorized to view this wallet"}})
		return
	}

	wallet, err := s.services.Bounty.GetWallet(c.Request.Context(), project.ID)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get wallet")
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to get wallet"}})
		return
	}

	c.JSON(http.StatusOK, wallet)
}

func (s *Server) handleCreateDeposit(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ID", "message": "invalid project id"}})
		return
	}

	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "unauthorized"}})
		return
	}

	project, err := s.getProjectForUser(c.Request.Context(), userID, id)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "not authorized to fund this project"}})
		return
	}

	var req struct {
		AmountSats int64 `json:"amount_sats" binding:"required,min=1000"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_REQUEST", "message": err.Error()}})
		return
	}
	idemKey, ok := idempotencyKey(c)
	if !ok {
		return
	}
	var invoiceCreationErr error
	var lightningUnavailable bool
	deposit, created, err := s.services.Bounty.GetOrCreateDeposit(c.Request.Context(), project.ID, idemKey, req.AmountSats, func(ctx context.Context) (string, []byte, time.Time, error) {
		if !s.services.Lightning.Connected() {
			lightningUnavailable = true
			return "", nil, time.Time{}, errors.New("lightning invoice service is unavailable")
		}
		bolt11, paymentHash, err := s.services.Lightning.CreateInvoice(ctx, req.AmountSats, "Satlight project wallet funding", time.Hour)
		invoiceCreationErr = err
		return bolt11, paymentHash, time.Now().Add(time.Hour), err
	})
	if err != nil {
		if errors.Is(err, bounty.ErrFundingPaused) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "FUNDING_PAUSED", "message": "project funding is temporarily paused"}})
			return
		}
		if errors.Is(err, bounty.ErrIdempotencyConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "IDEMPOTENCY_KEY_REUSED", "message": "Idempotency-Key was already used with a different amount"}})
			return
		}
		s.logger.Error().Err(err).Msg("failed to create deposit")
		if lightningUnavailable {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "LIGHTNING_UNAVAILABLE", "message": "Lightning invoice service is unavailable"}})
			return
		}
		if invoiceCreationErr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"code": "INVOICE_CREATION_FAILED", "message": "could not create Lightning invoice"}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to persist deposit"}})
		return
	}
	if created {
		c.JSON(http.StatusCreated, deposit)
	} else {
		c.JSON(http.StatusOK, deposit)
	}
}

func (s *Server) handleCreateWithdrawal(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ID", "message": "invalid project id"}})
		return
	}

	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "unauthorized"}})
		return
	}

	_, err = s.getProjectForUser(c.Request.Context(), userID, id)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "not authorized to withdraw from this project"}})
		return
	}
	c.JSON(http.StatusNotImplemented, gin.H{"error": gin.H{"code": "WITHDRAWALS_UNAVAILABLE", "message": "project withdrawals are not implemented yet"}})
}

func (s *Server) handleCreateBounty(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ID", "message": "invalid project id"}})
		return
	}

	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "unauthorized"}})
		return
	}

	project, err := s.getProjectForUser(c.Request.Context(), userID, id)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "not authorized to create bounties for this project"}})
		return
	}

	var req struct {
		GitHubIssueNumber int     `json:"github_issue_number" binding:"required"`
		Title             string  `json:"title" binding:"required,max=140"`
		Description       string  `json:"description" binding:"required"`
		Difficulty        string  `json:"difficulty" binding:"required,oneof=trivial easy medium hard"`
		RewardSats        int64   `json:"reward_sats" binding:"required,min=500"`
		ExpiresAt         *string `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_REQUEST", "message": err.Error()}})
		return
	}

	key, ok := idempotencyKey(c)
	if !ok {
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_EXPIRY", "message": "expires_at must be an RFC3339 timestamp"}})
			return
		}
		expiresAt = &t
	}

	params := bounty.CreateBountyParams{
		ProjectID:         project.ID,
		GitHubIssueNumber: req.GitHubIssueNumber,
		Title:             req.Title,
		Description:       req.Description,
		Difficulty:        req.Difficulty,
		RewardSats:        req.RewardSats,
		ExpiresAt:         expiresAt,
		IdempotencyKey:    key,
		IdempotencyHash:   requestHash(req),
	}

	b, created, err := s.services.Bounty.CreateBounty(c.Request.Context(), params)
	if err != nil {
		if errors.Is(err, bounty.ErrFundingPaused) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "FUNDING_PAUSED", "message": "project funding is temporarily paused"}})
			return
		}
		if errors.Is(err, bounty.ErrInsufficientFunds) {
			c.JSON(http.StatusPaymentRequired, gin.H{"error": gin.H{"code": "INSUFFICIENT_FUNDS", "message": "fund your project wallet before publishing this bounty"}})
			return
		}
		if errors.Is(err, bounty.ErrInvalidExpiry) {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_EXPIRY", "message": "expires_at must be in the future"}})
			return
		}
		if errors.Is(err, bounty.ErrDuplicateIssueBounty) {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "DUPLICATE_ISSUE", "message": "this repository issue already has a bounty"}})
			return
		}
		if errors.Is(err, bounty.ErrProjectFrozen) {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "PROJECT_FROZEN", "message": "this project is frozen and cannot publish bounties"}})
			return
		}
		if errors.Is(err, bounty.ErrProjectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "NOT_FOUND", "message": "project not found"}})
			return
		}
		if errors.Is(err, bounty.ErrIdempotencyConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "IDEMPOTENCY_KEY_REUSED", "message": "Idempotency-Key was already used with different request data"}})
			return
		}
		s.logger.Error().Err(err).Msg("failed to create bounty")
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to create bounty"}})
		return
	}

	if created {
		c.JSON(http.StatusCreated, b)
	} else {
		c.JSON(http.StatusOK, b)
	}
}

func (s *Server) handleGetMe(c *gin.Context) {
	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "unauthorized"}})
		return
	}

	githubID, login, email, lightningAddress, err := s.services.Auth.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get user")
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to get user"}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":                userID,
		"github_id":         githubID,
		"github_login":      login,
		"email":             email,
		"lightning_address": lightningAddress,
	})
}

func (s *Server) handleSetPayoutDestination(c *gin.Context) {
	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "unauthorized"}})
		return
	}

	var req struct {
		LightningAddress string `json:"lightning_address" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_REQUEST", "message": err.Error()}})
		return
	}

	_, err := s.pool.Exec(c.Request.Context(), `
		UPDATE users SET lightning_address = $1, payout_changed_at = now() WHERE id = $2
	`, req.LightningAddress, userID)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to update payout destination")
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to update payout destination"}})
		return
	}
	if err := s.services.Notify.NotifyDestinationChanged(c.Request.Context(), userID); err != nil {
		s.logger.Error().Err(err).Msg("failed to send destination-change notification")
	}

	c.JSON(http.StatusOK, gin.H{"message": "payout destination updated"})
}

func (s *Server) handleGetPayouts(c *gin.Context) {
	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "unauthorized"}})
		return
	}

	rows, err := s.pool.Query(c.Request.Context(), `
		SELECT p.id, p.bounty_id, p.amount_sats, p.fee_sats, p.status, p.settled_at, p.claim_link_token, b.title, pr.repo_full_name
		FROM payouts p
		JOIN bounties b ON p.bounty_id = b.id
		JOIN projects pr ON b.project_id = pr.id
		WHERE p.user_id = $1
		ORDER BY p.created_at DESC
	`, userID)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get payouts")
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to get payouts"}})
		return
	}
	defer rows.Close()

	var payouts []map[string]interface{}
	for rows.Next() {
		var p struct {
			ID           int64
			BountyID     int64
			AmountSats   int64
			FeeSats      int64
			Status       string
			SettledAt    *time.Time
			ClaimLink    *string
			Title        string
			RepoFullName string
		}
		if err := rows.Scan(&p.ID, &p.BountyID, &p.AmountSats, &p.FeeSats, &p.Status, &p.SettledAt, &p.ClaimLink, &p.Title, &p.RepoFullName); err != nil {
			continue
		}
		entry := map[string]interface{}{
			"id":             p.ID,
			"bounty_id":      p.BountyID,
			"amount_sats":    p.AmountSats,
			"fee_sats":       p.FeeSats,
			"status":         p.Status,
			"settled_at":     p.SettledAt,
			"title":          p.Title,
			"repo_full_name": p.RepoFullName,
		}
		if p.ClaimLink != nil {
			entry["claim_link_token"] = *p.ClaimLink
		}
		payouts = append(payouts, entry)
	}

	c.JSON(http.StatusOK, payouts)
}

func (s *Server) handleGetMyClaims(c *gin.Context) {
	userID, ok := auth.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHORIZED", "message": "unauthorized"}})
		return
	}
	rows, err := s.pool.Query(c.Request.Context(), `
		SELECT c.id, c.bounty_id, c.status, c.expires_at, b.title, b.reward_sats, b.state, pr.repo_full_name
		FROM claims c
		JOIN bounties b ON c.bounty_id = b.id
		JOIN projects pr ON b.project_id = pr.id
		WHERE c.user_id = $1
		ORDER BY c.created_at DESC
	`, userID)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get claims")
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to get claims"}})
		return
	}
	defer rows.Close()

	claims := []map[string]interface{}{}
	for rows.Next() {
		var cl struct {
			ID           int64
			BountyID     int64
			Status       string
			ExpiresAt    time.Time
			Title        string
			RewardSats   int64
			BountyState  string
			RepoFullName string
		}
		if err := rows.Scan(&cl.ID, &cl.BountyID, &cl.Status, &cl.ExpiresAt, &cl.Title, &cl.RewardSats, &cl.BountyState, &cl.RepoFullName); err != nil {
			continue
		}
		claims = append(claims, map[string]interface{}{
			"id":             cl.ID,
			"bounty_id":      cl.BountyID,
			"status":         cl.Status,
			"expires_at":     cl.ExpiresAt,
			"title":          cl.Title,
			"reward_sats":    cl.RewardSats,
			"bounty_state":   cl.BountyState,
			"repo_full_name": cl.RepoFullName,
		})
	}
	c.JSON(http.StatusOK, claims)
}

func (s *Server) handleRedeemPayoutClaim(c *gin.Context) {
	token := strings.TrimSpace(c.Param("token"))
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "ERROR", "reason": "claim token is required"})
		return
	}
	req, err := s.services.Payout.GetWithdrawRequest(c.Request.Context(), token)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"status": "ERROR", "reason": "unknown or already-used claim link"})
		return
	}
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to load claim link")
		c.JSON(http.StatusInternalServerError, gin.H{"status": "ERROR", "reason": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"tag":                "withdrawRequest",
		"callback":           req.CallbackURL,
		"k1":                 req.Token,
		"defaultDescription": req.DefaultDescription,
		"minWithdrawable":    req.MinWithdrawableMs,
		"maxWithdrawable":    req.MaxWithdrawableMs,
	})
}

func (s *Server) handleWithdrawCallback(c *gin.Context) {
	k1 := strings.TrimSpace(c.Query("k1"))
	pr := strings.TrimSpace(c.Query("pr"))
	if k1 == "" || pr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "ERROR", "reason": "k1 and pr are required"})
		return
	}
	err := s.services.Payout.RedeemClaimLink(c.Request.Context(), k1, pr)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"status": "OK"})
	case errors.Is(err, sql.ErrNoRows) || errors.Is(err, payout.ErrClaimLinkUsed):
		c.JSON(http.StatusNotFound, gin.H{"status": "ERROR", "reason": "unknown or already-used claim link"})
	case errors.Is(err, payout.ErrInvoiceAmountMismatch):
		c.JSON(http.StatusOK, gin.H{"status": "ERROR", "reason": err.Error()})
	default:
		s.logger.Error().Err(err).Msg("claim-link redemption failed")
		c.JSON(http.StatusOK, gin.H{"status": "ERROR", "reason": "payment failed; the link remains usable"})
	}
}

func (s *Server) handleGitHubWebhook(c *gin.Context) {
	deliveryID := c.GetHeader("X-GitHub-Delivery")
	event := c.GetHeader("X-GitHub-Event")
	signature := c.GetHeader("X-Hub-Signature-256")

	if deliveryID == "" || event == "" || signature == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "MISSING_HEADERS", "message": "missing required headers"}})
		return
	}

	body, err := c.GetRawData()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to read webhook body")
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_BODY", "message": "failed to read body"}})
		return
	}

	if !s.services.GitHub.VerifyWebhookSignature(body, signature) {
		s.logger.Warn().Str("delivery_id", deliveryID).Msg("invalid webhook signature")
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "INVALID_SIGNATURE", "message": "invalid webhook signature"}})
		return
	}

	// Record and enqueue; the worker verifies against the GitHub API and
	// applies lifecycle changes asynchronously with retries.
	_, err = s.services.GitHub.RecordWebhookDelivery(c.Request.Context(), deliveryID, event, body)
	if err != nil {
		s.logger.Error().Err(err).Str("delivery_id", deliveryID).Msg("failed to record webhook")
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "failed to record webhook"}})
		return
	}

	c.Status(http.StatusOK)
}

func (s *Server) getProjectForUser(ctx context.Context, userID int64, projectID int64) (*bounty.Project, error) {
	project, err := s.services.Bounty.GetProjectByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project.OwnerUserID != userID {
		return nil, errors.New("not authorized")
	}
	return project, nil
}

func (s *Server) getUserProject(ctx context.Context, userID int64, bountyID int64) (*bounty.Project, error) {
	_, project, err := s.services.Bounty.GetBountyWithProject(ctx, bountyID)
	if err != nil {
		return nil, err
	}
	if project.OwnerUserID != userID {
		return nil, errors.New("not authorized")
	}
	return project, nil
}
