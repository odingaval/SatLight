package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/joho/godotenv"
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

	direction := flag.String("dir", "up", "Migration direction: up or down")
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
		log.Info().Msg("Migrations applied successfully")
	case "down":
		log.Info().Msg("Rolling back last migration...")
		if err := m.Steps(-1); err != nil {
			log.Fatal().Err(err).Msg("migration down failed")
		}
		log.Info().Msg("Migration rolled back successfully")
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