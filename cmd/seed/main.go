package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
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

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer pool.Close()

	ctx := context.Background()

	log.Info().Msg("Seeding sample data...")

	var userID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO users (github_id, github_login, email, lightning_address)
		VALUES (12345, 'demo-maintainer', 'maintainer@example.com', 'maintainer@wallet.example')
		ON CONFLICT (github_id) DO UPDATE SET github_login = EXCLUDED.github_login
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create demo user")
	}
	log.Info().Int64("user_id", userID).Msg("Created demo maintainer")

	var contributorID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO users (github_id, github_login, email, lightning_address)
		VALUES (67890, 'demo-contributor', 'contributor@example.com', 'contributor@wallet.example')
		ON CONFLICT (github_id) DO UPDATE SET github_login = EXCLUDED.github_login
		RETURNING id
	`).Scan(&contributorID)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create demo contributor")
	}
	log.Info().Int64("user_id", contributorID).Msg("Created demo contributor")

	var projectID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO projects (owner_user_id, github_installation_id, repo_full_name)
		VALUES ($1, 98765, 'demo/repo')
		ON CONFLICT (repo_full_name) DO UPDATE SET owner_user_id = EXCLUDED.owner_user_id
		RETURNING id
	`, userID).Scan(&projectID)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create demo project")
	}
	log.Info().Int64("project_id", projectID).Msg("Created demo project")

	_, err = pool.Exec(ctx, `
		INSERT INTO wallets (project_id, available_sats, reserved_sats)
		VALUES ($1, 1000000, 0)
		ON CONFLICT (project_id) DO UPDATE SET available_sats = 1000000, reserved_sats = 0
	`, projectID)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create wallet")
	}
	log.Info().Msg("Created demo wallet with 1,000,000 sats")

	bounties := []struct {
		issueNumber int
		title       string
		description string
		difficulty  string
		reward      int64
	}{
		{1, "Fix typo in README", "There's a typo in the installation section of the README. Please fix 'instalation' to 'installation'.", "trivial", 2000},
		{2, "Add unit tests for utils package", "The utils package lacks unit tests. Add tests for the string helpers and date formatting functions.", "easy", 10000},
		{3, "Update dependencies", "Several dependencies are outdated. Update them to their latest versions and fix any breaking changes.", "easy", 5000},
		{4, "Improve error handling in API client", "The API client doesn't properly handle 429 rate limit responses. Add retry logic with exponential backoff.", "medium", 25000},
		{5, "Add dark mode support", "Add a dark mode toggle to the UI with proper persistence in localStorage.", "medium", 30000},
	}

	for _, b := range bounties {
		var bountyID int64
		err = pool.QueryRow(ctx, `
			INSERT INTO bounties (project_id, github_issue_number, title, description, difficulty, reward_sats, fee_sats, state, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'open', now() + interval '90 days')
			ON CONFLICT (project_id, github_issue_number) DO UPDATE SET title = EXCLUDED.title
			RETURNING id
		`, projectID, b.issueNumber, b.title, b.description, b.difficulty, b.reward, (b.reward*200)/10000).Scan(&bountyID)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create bounty")
		}
		log.Info().Int64("bounty_id", bountyID).Str("title", b.title).Msg("Created demo bounty")
	}

	log.Info().Msg("Database seeded successfully!")
	fmt.Println("\nDemo accounts:")
	fmt.Println("  Maintainer: github_id=12345, lightning=maintainer@wallet.example")
	fmt.Println("  Contributor: github_id=67890, lightning=contributor@wallet.example")
	fmt.Println("\nDemo project: demo/repo (5 bounties created)")
	fmt.Println("Demo wallet: 1,000,000 sats available")
}