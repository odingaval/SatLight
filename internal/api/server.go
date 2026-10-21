package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/satlight/satlight/internal/auth"
	"github.com/satlight/satlight/internal/bounty"
	"github.com/satlight/satlight/internal/config"
	"github.com/satlight/satlight/internal/github"
	"github.com/satlight/satlight/internal/ledger"
	"github.com/satlight/satlight/internal/lightning"
	"github.com/satlight/satlight/internal/queue"
)

type Server struct {
	cfg      *config.Config
	pool     *pgxpool.Pool
	router   *gin.Engine
	logger   *zerolog.Logger
	services *Services
}

type Services struct {
	Auth       *auth.Service
	Bounty     *bounty.Service
	Ledger     *ledger.Service
	Lightning  *lightning.Service
	GitHub     *github.Service
	Queue      *queue.Client
}

func NewServer(cfg *config.Config, pool *pgxpool.Pool) *Server {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: zerolog.ConsoleWriter{Out: gin.DefaultWriter, TimeFormat: zerolog.TimeFormatUnix}}).With().Timestamp().Logger()

	services := &Services{
		Auth:       auth.NewService(cfg, pool),
		Bounty:     bounty.NewService(cfg, pool),
		Ledger:     ledger.NewService(cfg, pool),
		Lightning:  lightning.NewService(cfg, pool),
		GitHub:     github.NewService(cfg, pool),
		Queue:      queue.NewClient(pool),
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(zerologMiddleware(&logger))
	router.Use(corsMiddleware())

	s := &Server{
		cfg:      cfg,
		pool:     pool,
		router:   router,
		logger:   &logger,
		services: services,
	}

	s.setupRoutes()
	return s
}

func (s *Server) Router() *gin.Engine {
	return s.router
}

func (s *Server) setupRoutes() {
	s.router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := s.router.Group("/api/v1")
	{
		v1.POST("/waitlist", s.handleWaitlist)

		v1.GET("/bounties", s.handleListBounties)
		v1.GET("/bounties/:id", s.handleGetBounty)
		v1.POST("/bounties/:id/claim", s.handleClaimBounty)
		v1.DELETE("/bounties/:id/claim", s.handleReleaseClaim)
		v1.POST("/bounties/:id/cancel", s.handleCancelBounty)
		v1.POST("/bounties/:id/dispute", s.handleDisputeBounty)

		v1.POST("/projects", s.handleCreateProject)
		v1.GET("/projects/:id/wallet", s.handleGetWallet)
		v1.POST("/projects/:id/deposits", s.handleCreateDeposit)
		v1.POST("/projects/:id/withdrawals", s.handleCreateWithdrawal)
		v1.POST("/projects/:id/bounties", s.handleCreateBounty)

		v1.GET("/me", s.authRequired(), s.handleGetMe)
		v1.PUT("/me/payout-destination", s.authRequired(), s.handleSetPayoutDestination)
		v1.GET("/me/payouts", s.authRequired(), s.handleGetPayouts)

		v1.GET("/payouts/claim/:token", s.handleRedeemPayoutClaim)

		v1.POST("/webhooks/github", s.handleGitHubWebhook)
	}
}

func (s *Server) authRequired() gin.HandlerFunc {
	return s.services.Auth.Middleware()
}

func zerologMiddleware(logger *zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := zerolog.Now()
		path := c.Request.URL.Path
		c.Next()
		logger.Info().
			Str("method", c.Request.Method).
			Str("path", path).
			Int("status", c.Writer.Status()).
			Dur("latency", zerolog.Since(start)).
			Msg("request")
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}