package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/satlight/satlight/internal/api"
	"github.com/satlight/satlight/internal/config"
	"github.com/satlight/satlight/internal/db"
)

func main() {
	cfg := config.MustLoad()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if cfg.LogLevel != "" {
		level, err := zerolog.ParseLevel(cfg.LogLevel)
		if err == nil {
			zerolog.SetGlobalLevel(level)
		}
	}

	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	}

	pool, err := db.NewPool(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer pool.Close()

	server := api.NewServer(cfg, pool)

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: server.Router(),
	}

	go func() {
		log.Info().Str("addr", cfg.HTTPAddr).Msg("starting HTTP server")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("HTTP server error")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("server forced to shutdown")
	}

	log.Info().Msg("server exited")
}