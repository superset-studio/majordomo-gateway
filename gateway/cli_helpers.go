package gateway

import (
	"context"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/superset-studio/majordomo-gateway/internal/config"
	"github.com/superset-studio/majordomo-gateway/internal/secrets"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

// connectDB loads config and returns a connected PostgresStorage.
// Used by CLI commands that only need DB access, not a full server.
func connectDB(configPath string) *storage.PostgresStorage {
	_ = godotenv.Load()
	cfg := loadCLIConfig(configPath)

	store, err := storage.NewPostgresStorage(
		context.Background(),
		cfg.Storage.Postgres.DSN(),
		cfg.Storage.Postgres.MaxConns,
		nil,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to database: %v\n", err)
		os.Exit(1)
	}
	return store
}

// loadCLIConfig loads config for CLI commands (non-serve paths).
func loadCLIConfig(configPath string) *config.Config {
	_ = godotenv.Load()
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

func newSecretStoreFromConfig(cfg *config.Config) (secrets.SecretStore, error) {
	if cfg.Secrets.EncryptionKey == "" {
		return nil, fmt.Errorf("secrets.encryption_key is required (set MAJORDOMO_SECRETS_ENCRYPTION_KEY)")
	}
	return secrets.NewAESStore(cfg.Secrets.EncryptionKey)
}
