package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "start":
		runStart(os.Args[2:])
	case "stop":
		runStop(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Usage: majordomo-companion <command> [options]

Commands:
  start    Start the companion proxy (registers a session with the gateway)
  stop     Stop a running companion proxy

Run 'majordomo-companion <command> --help' for more information.`)
}

func runStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	gateway := fs.String("gateway", "http://localhost:7680", "gateway URL")
	apiKey := fs.String("api-key", "", "Majordomo API key (required)")
	port := fs.Int("port", 0, "local port to listen on (0 = random)")
	pidFile := fs.String("pid-file", "", "path to write PID file (required)")
	envFile := fs.String("env-file", "", "path to write ANTHROPIC_BASE_URL export")
	fs.Parse(args)

	if *apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: --api-key is required")
		os.Exit(1)
	}
	if *pidFile == "" {
		fmt.Fprintln(os.Stderr, "error: --pid-file is required")
		os.Exit(1)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	gatewayURL, err := url.Parse(*gateway)
	if err != nil {
		slog.Error("invalid gateway URL", "error", err)
		os.Exit(1)
	}

	// Start listening before registering session so we know the port
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		slog.Error("failed to listen", "error", err)
		os.Exit(1)
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	localURL := fmt.Sprintf("http://127.0.0.1:%d", actualPort)

	// Register session with gateway
	sessionID, err := registerSession(*gateway, *apiKey)
	if err != nil {
		slog.Error("failed to register session", "error", err)
		listener.Close()
		os.Exit(1)
	}

	slog.Info("session registered", "session_id", sessionID, "port", actualPort)

	// Write PID file
	if err := os.WriteFile(*pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		slog.Error("failed to write PID file", "error", err)
		listener.Close()
		os.Exit(1)
	}

	// Write env file if requested
	if *envFile != "" {
		content := fmt.Sprintf("export ANTHROPIC_BASE_URL=%s\n", localURL)
		if err := os.WriteFile(*envFile, []byte(content), 0644); err != nil {
			slog.Error("failed to write env file", "error", err)
		}
	}

	// Print the URL so callers can capture it
	fmt.Println(localURL)

	// Create reverse proxy
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = gatewayURL.Scheme
			req.URL.Host = gatewayURL.Host
			req.Host = gatewayURL.Host
			req.Header.Set("X-Majordomo-Key", *apiKey)
			req.Header.Set("X-Majordomo-Session-Id", sessionID)
		},
	}

	server := &http.Server{
		Handler:      proxy,
		ReadTimeout:  5 * time.Minute,
		WriteTimeout: 10 * time.Minute,
	}

	// Signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("shutting down companion")

		// End session
		if err := endSession(*gateway, *apiKey, sessionID); err != nil {
			slog.Error("failed to end session", "error", err)
		} else {
			slog.Info("session ended", "session_id", sessionID)
		}

		// Clean up PID file
		os.Remove(*pidFile)

		// Shutdown server
		server.Close()
	}()

	slog.Info("companion proxy started", "listen", localURL, "gateway", *gateway)

	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func runStop(args []string) {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	pidFile := fs.String("pid-file", "", "path to PID file (required)")
	fs.Parse(args)

	if *pidFile == "" {
		fmt.Fprintln(os.Stderr, "error: --pid-file is required")
		os.Exit(1)
	}

	data, err := os.ReadFile(*pidFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading PID file: %v\n", err)
		os.Exit(1)
	}

	pid, err := strconv.Atoi(string(bytes.TrimSpace(data)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid PID in file: %v\n", err)
		os.Exit(1)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "process not found: %v\n", err)
		os.Exit(1)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		fmt.Fprintf(os.Stderr, "failed to send SIGTERM: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Sent SIGTERM to companion (PID %d)\n", pid)
}

type sessionResponse struct {
	ID string `json:"id"`
}

func registerSession(gateway, apiKey string) (string, error) {
	req, err := http.NewRequest("POST", gateway+"/api/v1/claude-sessions", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Majordomo-Key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var session sessionResponse
	if err := json.Unmarshal(body, &session); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return session.ID, nil
}

func endSession(gateway, apiKey, sessionID string) error {
	req, err := http.NewRequest("POST", gateway+"/api/v1/claude-sessions/"+sessionID+"/end", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Majordomo-Key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
