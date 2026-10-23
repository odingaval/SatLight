package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/satlight/satlight/internal/bounty"
	"github.com/satlight/satlight/internal/config"
	"github.com/satlight/satlight/internal/db"
	"github.com/satlight/satlight/internal/ledger"
	"github.com/satlight/satlight/internal/queue"
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
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	}

	pool, err := db.NewPool(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer pool.Close()

	ledgerService := ledger.NewService(cfg, pool)
	bountyService := bounty.NewService(cfg, pool)
	bountyService.SetDependencies(ledgerService, nil)

	workers := river.NewWorkers()
	river.AddWorker(workers, &queue.ProcessPayoutJob{})
	river.AddWorker(workers, &queue.ProcessWebhookJob{})
	river.AddWorker(workers, &queue.ReconciliationJob{Service: ledgerService})
	river.AddWorker(workers, &queue.ClaimExpiryJob{Service: bountyService})
	river.AddWorker(workers, &queue.BountyExpiryJob{Service: bountyService})

	driver := riverpgxv5.New(pool)
	client, err := river.NewClient(driver, &river.Config{
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(
				river.PeriodicInterval(time.Hour),
				func() (river.JobArgs, *river.InsertOpts) {
					return queue.ReconciliationArgs{}, &river.InsertOpts{Queue: "reconciliation"}
				},
				&river.PeriodicJobOpts{RunOnStart: true},
			),
		},
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
			"payouts":          {MaxWorkers: 5},
			"webhooks":         {MaxWorkers: 10},
			"reconciliation":   {MaxWorkers: 1},
		},
		Workers: workers,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create river client")
	}

	go func() {
		log.Info().Msg("starting worker")
		if err := client.Start(context.Background()); err != nil {
			log.Fatal().Err(err).Msg("worker error")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down worker...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client.Stop(ctx)

	log.Info().Msg("worker exited")
}
