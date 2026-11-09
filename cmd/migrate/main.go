package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/satlight/satlight/internal/config"
)

func main() {
	_ = godotenv.Load()

	cfg := config.MustLoad()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if cfg.LogLevel != "" {
		level, err := zerolog.ParseLevel(cfg.LogLevel)
		if err == nil {
			zerolog.SetGlobalLevel(level)
		}
	}

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: zerolog.TimeFormatUnix})

	direction := flag.String("dir", "up", "Migration direction: up, down, river-down, or version")
	flag.Parse()

	m, err := migrate.New(
		"file://migrations",
		cfg.DatabaseURL,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create migrate instance")
	}
	defer m.Close()

	switch *direction {
	case "up":
		log.Info().Msg("Running migrations up...")
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			log.Fatal().Err(err).Msg("migration up failed")
		}
		log.Info().Msg("Running River queue migrations up...")
		if err := migrateRiver(cfg.DatabaseURL, rivermigrate.DirectionUp, nil); err != nil {
			log.Fatal().Err(err).Msg("river migration up failed")
		}
		log.Info().Msg("Migrations applied successfully")
	case "down":
		log.Info().Msg("Rolling back last migration...")
		if err := m.Steps(-1); err != nil {
			log.Fatal().Err(err).Msg("migration down failed")
		}
		// The River queue schema is shared job infrastructure; leave it in
		// place when rolling back application migrations.
		log.Info().Msg("Migration rolled back successfully")
	case "river-down":
		log.Info().Msg("Rolling back River queue migrations...")
		if err := migrateRiver(cfg.DatabaseURL, rivermigrate.DirectionDown, &rivermigrate.MigrateOpts{MaxSteps: 1}); err != nil {
			log.Fatal().Err(err).Msg("river migration down failed")
		}
		log.Info().Msg("River migration rolled back successfully")
	case "version":
		version, dirty, err := m.Version()
		if err != nil {
			log.Fatal().Err(err).Msg("failed to get migration version")
		}
		fmt.Printf("Version: %d, Dirty: %v\n", version, dirty)
	default:
		flag.Usage()
		os.Exit(1)
	}
}

func migrateRiver(databaseURL string, direction rivermigrate.Direction, opts *rivermigrate.MigrateOpts) error {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return err
	}
	_, err = migrator.Migrate(ctx, direction, opts)
	return err
}
