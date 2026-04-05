package gateway

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

	"github.com/joho/godotenv"
)

// RunCLI is the single entry point for both the OSS and enterprise binaries.
// It dispatches to the appropriate subcommand handler. For the serve command,
// any provided Options (e.g. WithPolicyEnforcer, WithRequestEnricher) are
// forwarded to Build and take effect at server startup.
func RunCLI(args []string, opts ...Option) {
	if len(args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch args[1] {
	case "serve":
		runServe(args[2:], opts)
	case "keys":
		runKeys(args[2:])
	case "proxy-keys":
		runProxyKeys(args[2:])
	case "users":
		runUsers(args[2:])
	case "metadata":
		runMetadata(args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", args[1])
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

func runServe(args []string, opts []Option) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	fs.Parse(args)

	_ = godotenv.Load()

	ctx := context.Background()

	buildOpts := append([]Option{WithConfigPath(*configPath)}, opts...)
	srv, err := Build(ctx, buildOpts...)
	if err != nil {
		slog.Error("failed to build server", "error", err)
		os.Exit(1)
	}

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
