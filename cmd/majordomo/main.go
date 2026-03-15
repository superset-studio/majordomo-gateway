package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/superset-studio/majordomo-gateway/internal/api"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	apiemail "github.com/superset-studio/majordomo-gateway/internal/email"
	"github.com/superset-studio/majordomo-gateway/internal/claudecode"
	"github.com/superset-studio/majordomo-gateway/internal/pricing"
	"github.com/superset-studio/majordomo-gateway/internal/proxy"
	"github.com/superset-studio/majordomo-gateway/internal/secrets"
	"github.com/superset-studio/majordomo-gateway/internal/server"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
	case "keys":
		runKeys(os.Args[2:])
	case "proxy-keys":
		runProxyKeys(os.Args[2:])
	case "users":
		runUsers(os.Args[2:])
	case "metadata":
		runMetadata(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Usage: majordomo <command> [options]

Commands:
  serve        Start the proxy server
  keys         Manage API keys
  proxy-keys   Manage proxy keys
  users        Manage web UI users
  metadata     Manage metadata indexing

Run 'majordomo <command> --help' for more information.`)
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	fs.Parse(args)

	cfg := loadConfig(*configPath)

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.Logging.Level),
	})))
	ctx := context.Background()

	store, err := storage.NewPostgresStorage(ctx, cfg.Storage.Postgres.DSN(), cfg.Storage.Postgres.MaxConns, &storage.PostgresStorageConfig{
		HLLFlushInterval:   cfg.Metadata.HLLFlushInterval,
		ActiveKeysCacheTTL: cfg.Metadata.ActiveKeysCacheTTL,
	})
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	pricingSvc := pricing.NewService(
		cfg.Pricing.RemoteURL,
		cfg.Pricing.FallbackFile,
		cfg.Pricing.AliasesFile,
		cfg.Pricing.RefreshInterval,
	)
	defer pricingSvc.Close()

	var s3Storage *storage.S3BodyStorage
	if cfg.S3.Enabled {
		s3Storage, err = storage.NewS3BodyStorage(ctx, storage.S3Config{
			Bucket:          cfg.S3.Bucket,
			Region:          cfg.S3.Region,
			Endpoint:        cfg.S3.Endpoint,
			AccessKeyID:     cfg.S3.AccessKeyID,
			SecretAccessKey: cfg.S3.SecretAccessKey,
		})
		if err != nil {
			slog.Error("failed to initialize S3 storage", "error", err)
			os.Exit(1)
		}
		defer s3Storage.Close()
		slog.Info("S3 body storage enabled", "bucket", cfg.S3.Bucket, "region", cfg.S3.Region)
	}

	resolver := auth.NewResolver(store)

	// Set up proxy key support if encryption key is configured
	var proxyResolver *auth.ProxyResolver
	var apiHandler *api.Handler
	if cfg.Secrets.EncryptionKey != "" {
		secretStore, err := secrets.NewAESStore(cfg.Secrets.EncryptionKey)
		if err != nil {
			slog.Error("failed to initialize secret store", "error", err)
			os.Exit(1)
		}
		proxyResolver = auth.NewProxyResolver(store, secretStore)
		apiHandler = api.NewHandler(store, secretStore)
		slog.Info("proxy key support enabled")
	}

	// Set up Claude Code session tracking
	sessionMgr := claudecode.NewSessionManager(store)
	claudeHandler := api.NewClaudeSessionHandler(sessionMgr, store)

	// Set up per-user S3 body storage
	userS3Storage := storage.NewUserS3Storage()

	// Determine secret store and user store for proxy handler
	var proxySecretStore secrets.SecretStore
	if cfg.Secrets.EncryptionKey != "" {
		proxySecretStore, _ = secrets.NewAESStore(cfg.Secrets.EncryptionKey)
	}

	proxyHandler := proxy.NewHandler(store, s3Storage, userS3Storage, store, store, proxySecretStore, pricingSvc, resolver, proxyResolver, sessionMgr, cfg)

	// Set up admin web UI if JWT secret is configured
	var adminCfg *server.AdminConfig
	var usageHandler *api.UsageHandler
	var metadataHandler *api.MetadataHandler
	var claudeAnalyticsHandler *api.ClaudeAnalyticsHandler
	if cfg.JWT.Secret != "" {
		jwtSvc := auth.NewJWTService(cfg.JWT.Secret, cfg.JWT.Expiry)

		var adminSecretStore secrets.SecretStore
		if cfg.Secrets.EncryptionKey != "" {
			adminSecretStore, err = secrets.NewAESStore(cfg.Secrets.EncryptionKey)
			if err != nil {
				slog.Error("failed to initialize secret store for admin", "error", err)
				os.Exit(1)
			}
		}

		// Email sender and frontend URL for password reset links
		var emailSender api.EmailSender
		frontendURL := cfg.Email.FrontendURL
		if frontendURL == "" {
			frontendURL = cfg.OAuth.FrontendURL
			if frontendURL == "" {
				frontendURL = cfg.Server.BaseURL
			}
		}
		if cfg.Email.ResendAPIKey != "" && cfg.Email.From != "" {
			emailSender = apiemail.NewResendSender(cfg.Email.ResendAPIKey, cfg.Email.From)
		}

		adminHandler := api.NewAdminHandler(store, store, store, store, adminSecretStore, jwtSvc, store, store, emailSender, frontendURL)
		usageHandler = api.NewUsageHandler(store, store)
		metadataHandler = api.NewMetadataHandler(store, store)
		claudeAnalyticsHandler = api.NewClaudeAnalyticsHandler(store, store)
		var orgHandler *api.OrgHandler
		if adminSecretStore != nil {
			orgHandler = api.NewOrgHandler(store, store, adminSecretStore, jwtSvc, store, emailSender, frontendURL)
		}

		adminCfg = &server.AdminConfig{
			AdminHandler: adminHandler,
			OrgHandler:   orgHandler,
			JWTService:   jwtSvc,
			CORSOrigins:  cfg.CORS.AllowedOrigins,
		}

		// Set up OAuth if any provider is configured
		if cfg.OAuth.GitHub.ClientID != "" || cfg.OAuth.Google.ClientID != "" {
			gatewayBaseURL := cfg.Server.BaseURL
			if gatewayBaseURL == "" {
				gatewayBaseURL = fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
			}
			oauthHandler := api.NewOAuthHandler(store, store, jwtSvc, cfg.OAuth, gatewayBaseURL)
			adminCfg.OAuthHandler = oauthHandler
			slog.Info("OAuth authentication enabled",
				"github", cfg.OAuth.GitHub.ClientID != "",
				"google", cfg.OAuth.Google.ClientID != "",
			)
		}

		slog.Info("admin web UI enabled")
	}

	srv := server.New(&cfg.Server, proxyHandler, store, apiHandler, resolver, adminCfg, claudeHandler, usageHandler, metadataHandler, claudeAnalyticsHandler)

	errChan := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errChan:
		slog.Error("server error", "error", err)
		os.Exit(1)
	case sig := <-sigChan:
		slog.Info("received signal, shutting down", "signal", sig)
	}

	if err := srv.ShutdownWithTimeout(30 * time.Second); err != nil {
		slog.Error("shutdown error", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}
