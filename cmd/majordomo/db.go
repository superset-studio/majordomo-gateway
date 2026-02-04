package main

import (
	"context"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/superset-studio/majordomo-gateway/internal/config"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

// connectDB creates a PostgresStorage connection using the config.
// It loads .env and config file, then connects to the database.
// For CLI commands that don't need HLL/cache, pass nil for storageConfig.
func connectDB(configPath string, storageConfig *storage.PostgresStorageConfig) *storage.PostgresStorage {
	// Load .env file if present
	_ = godotenv.Load()

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	store, err := storage.NewPostgresStorage(
		context.Background(),
		cfg.Storage.Postgres.DSN(),
		cfg.Storage.Postgres.MaxConns,
		storageConfig,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to database: %v\n", err)
		os.Exit(1)
	}

	return store
}

// loadConfig loads the application configuration from the given path.
// It loads .env file first and returns the parsed config.
func loadConfig(configPath string) *config.Config {
	// Load .env file if present
	_ = godotenv.Load()

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	return cfg
}
