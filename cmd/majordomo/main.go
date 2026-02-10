package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/pricing"
	"github.com/superset-studio/majordomo-gateway/internal/proxy"
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
  serve    Start the proxy server
  keys     Manage API keys

Run 'majordomo <command> --help' for more information.`)
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	fs.Parse(args)

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg := loadConfig(*configPath)
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

	proxyHandler := proxy.NewHandler(store, s3Storage, pricingSvc, resolver, cfg)

	srv := server.New(&cfg.Server, proxyHandler, store)

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
