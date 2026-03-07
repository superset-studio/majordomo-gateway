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
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// metadataFlags collects repeatable --metadata key=value flags.
type metadataFlags []string

func (m *metadataFlags) String() string { return strings.Join(*m, ", ") }
func (m *metadataFlags) Set(value string) error {
	*m = append(*m, value)
	return nil
}

// companionProxy holds all state needed to run the proxy.
type companionProxy struct {
	gateway     string
	gatewayURL  *url.URL
	apiKey      string
	sessionID   string
	sessionName string
	listener    net.Listener
	server      *http.Server
	localURL    string
	metadata    map[string]string
}

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
	case "exec":
		runExec(os.Args[2:])
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
  exec     Start the proxy, run a command, then stop the proxy on exit

Examples:
  majordomo-companion start --gateway https://gw.example.com --api-key $KEY --pid-file /tmp/companion.pid
  majordomo-companion stop --pid-file /tmp/companion.pid
  majordomo-companion exec --gateway https://gw.example.com --api-key $KEY -- claude

Run 'majordomo-companion <command> --help' for more information.`)
}

// parseCommonFlags registers and parses flags common to start and exec.
func parseCommonFlags(fs *flag.FlagSet, args []string) (gateway, apiKey, workdir, sessionName string, port int, cliMetadata metadataFlags) {
	gatewayFlag := fs.String("gateway", "http://localhost:7680", "gateway URL")
	apiKeyFlag := fs.String("api-key", "", "Majordomo API key (required)")
	portFlag := fs.Int("port", 0, "local port to listen on (0 = random)")
	workdirFlag := fs.String("workdir", "", "directory to read .majordomo/metadata.json from (default: current directory)")
	sessionNameFlag := fs.String("session-name", "", "optional name for this Claude Code session")
	fs.Var(&cliMetadata, "metadata", "metadata key=value pair (repeatable)")
	fs.Parse(args)

	return *gatewayFlag, *apiKeyFlag, *workdirFlag, *sessionNameFlag, *portFlag, cliMetadata
}

// buildMetadata merges metadata from .majordomo/metadata.json and CLI flags.
func buildMetadata(workdir string, cliMetadata metadataFlags) map[string]string {
	metadata := loadMetadataFile(workdir)
	for _, kv := range cliMetadata {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			slog.Warn("ignoring malformed --metadata flag (expected key=value)", "value", kv)
			continue
		}
		metadata[key] = value
	}

	if len(metadata) > 0 {
		slog.Info("metadata loaded", "keys", metadataKeys(metadata))
	}
	return metadata
}

// startProxy creates a listener, registers a session, and starts the reverse proxy.
// The proxy server runs in a background goroutine. Call cp.shutdown() to stop it.
func startProxy(gateway, apiKey, sessionName string, port int, metadata map[string]string) (*companionProxy, error) {
	gatewayURL, err := url.Parse(gateway)
	if err != nil {
		return nil, fmt.Errorf("invalid gateway URL: %w", err)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	localURL := fmt.Sprintf("http://127.0.0.1:%d", actualPort)

	sessionID, err := registerSession(gateway, apiKey, sessionName)
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("failed to register session: %w", err)
	}

	slog.Info("session registered", "session_id", sessionID, "port", actualPort)

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = gatewayURL.Scheme
			req.URL.Host = gatewayURL.Host
			req.Host = gatewayURL.Host
			req.Header.Set("X-Majordomo-Key", apiKey)
			req.Header.Set("X-Majordomo-Client", "claude-code")
			req.Header.Set("X-Majordomo-ClaudeCode-Session-Id", sessionID)
			if sessionName != "" {
				req.Header.Set("X-Majordomo-ClaudeCode-Session-Name", sessionName)
			}

			for key, value := range metadata {
				req.Header.Set("X-Majordomo-"+key, value)
			}
		},
	}

	server := &http.Server{
		Handler:      proxy,
		ReadTimeout:  5 * time.Minute,
		WriteTimeout: 10 * time.Minute,
	}

	cp := &companionProxy{
		gateway:     gateway,
		gatewayURL:  gatewayURL,
		apiKey:      apiKey,
		sessionID:   sessionID,
		sessionName: sessionName,
		listener:    listener,
		server:      server,
		localURL:    localURL,
		metadata:    metadata,
	}

	// Start serving in background
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()

	return cp, nil
}

// shutdown ends the session and stops the proxy server.
func (cp *companionProxy) shutdown() {
	if err := endSession(cp.gateway, cp.apiKey, cp.sessionID); err != nil {
		slog.Error("failed to end session", "error", err)
	} else {
		slog.Info("session ended", "session_id", cp.sessionID)
	}
	cp.server.Close()
}

func runStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	pidFile := fs.String("pid-file", "", "path to write PID file (required)")
	envFile := fs.String("env-file", "", "path to write ANTHROPIC_BASE_URL export")
	gateway, apiKey, workdir, sessionName, port, cliMeta := parseCommonFlags(fs, args)

	if apiKey == "" {
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

	metadata := buildMetadata(workdir, cliMeta)

	cp, err := startProxy(gateway, apiKey, sessionName, port, metadata)
	if err != nil {
		slog.Error("failed to start proxy", "error", err)
		os.Exit(1)
	}

	// Write PID file
	if err := os.WriteFile(*pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		slog.Error("failed to write PID file", "error", err)
		cp.shutdown()
		os.Exit(1)
	}

	// Write env file if requested
	if *envFile != "" {
		content := fmt.Sprintf("export ANTHROPIC_BASE_URL=%s\n", cp.localURL)
		if err := os.WriteFile(*envFile, []byte(content), 0644); err != nil {
			slog.Error("failed to write env file", "error", err)
		}
	}

	// Print the URL so callers can capture it
	fmt.Println(cp.localURL)

	// Signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	slog.Info("shutting down companion")
	cp.shutdown()
	os.Remove(*pidFile)
}

func runExec(args []string) {
	fs := flag.NewFlagSet("exec", flag.ExitOnError)
	gateway, apiKey, workdir, sessionName, port, cliMeta := parseCommonFlags(fs, args)

	// Everything after flag parsing is the command to run
	cmdArgs := fs.Args()
	if len(cmdArgs) == 0 {
		fmt.Fprintln(os.Stderr, "error: no command specified after flags")
		fmt.Fprintln(os.Stderr, "usage: majordomo-companion exec [options] -- <command> [args...]")
		os.Exit(1)
	}

	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: --api-key is required")
		os.Exit(1)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	metadata := buildMetadata(workdir, cliMeta)

	cp, err := startProxy(gateway, apiKey, sessionName, port, metadata)
	if err != nil {
		slog.Error("failed to start proxy", "error", err)
		os.Exit(1)
	}

	slog.Info("companion proxy started", "listen", cp.localURL, "gateway", gateway)

	// Launch the child command with ANTHROPIC_BASE_URL set
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "ANTHROPIC_BASE_URL="+cp.localURL)

	// Forward signals to child
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		if cmd.Process != nil {
			cmd.Process.Signal(sig)
		}
	}()

	exitCode := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			slog.Error("failed to run command", "error", err)
			exitCode = 1
		}
	}

	slog.Info("command exited, shutting down companion", "exit_code", exitCode)
	cp.shutdown()
	os.Exit(exitCode)
}

// loadMetadataFile reads .majordomo/metadata.json from the given directory,
// or the current working directory if dir is empty.
// Returns an empty map if the file doesn't exist or can't be parsed.
func loadMetadataFile(dir string) map[string]string {
	metadata := make(map[string]string)

	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return metadata
		}
	}

	path := filepath.Join(dir, ".majordomo", "metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return metadata
	}

	if err := json.Unmarshal(data, &metadata); err != nil {
		slog.Warn("failed to parse .majordomo/metadata.json", "error", err)
		return make(map[string]string)
	}

	slog.Info("loaded metadata from file", "path", path, "keys", metadataKeys(metadata))
	return metadata
}

func metadataKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
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

func registerSession(gateway, apiKey, sessionName string) (string, error) {
	var body io.Reader
	if sessionName != "" {
		jsonBody, _ := json.Marshal(map[string]string{"name": sessionName})
		body = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest("POST", gateway+"/api/v1/claude-sessions", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Majordomo-Key", apiKey)
	if sessionName != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var session sessionResponse
	if err := json.Unmarshal(respBody, &session); err != nil {
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
