package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/config"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/secrets"
)

func runProxyKeys(args []string) {
	if len(args) < 1 {
		printProxyKeysUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "create":
		runProxyKeysCreate(args[1:])
	case "list":
		runProxyKeysList(args[1:])
	case "get":
		runProxyKeysGet(args[1:])
	case "revoke":
		runProxyKeysRevoke(args[1:])
	case "set-provider":
		runProxyKeysSetProvider(args[1:])
	case "remove-provider":
		runProxyKeysRemoveProvider(args[1:])
	case "list-providers":
		runProxyKeysListProviders(args[1:])
	case "help", "-h", "--help":
		printProxyKeysUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown proxy-keys subcommand: %s\n\n", args[0])
		printProxyKeysUsage()
		os.Exit(1)
	}
}

func printProxyKeysUsage() {
	fmt.Println(`Usage: majordomo proxy-keys <subcommand> [options]

Subcommands:
  create           Create a new proxy key
  list             List proxy keys
  get              Get details of a proxy key
  revoke           Revoke a proxy key
  set-provider     Set a provider API key mapping
  remove-provider  Remove a provider API key mapping
  list-providers   List provider mappings for a proxy key

Run 'majordomo proxy-keys <subcommand> --help' for more information.`)
}

func runProxyKeysCreate(args []string) {
	fs := flag.NewFlagSet("proxy-keys create", flag.ExitOnError)
	name := fs.String("name", "", "Name for the proxy key (required)")
	description := fs.String("description", "", "Description for the proxy key")
	majordomoKeyID := fs.String("majordomo-key-id", "", "Majordomo API key ID to associate with (required)")
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(args)

	if *name == "" {
		fmt.Fprintln(os.Stderr, "Error: --name is required")
		fs.Usage()
		os.Exit(1)
	}

	if *majordomoKeyID == "" {
		fmt.Fprintln(os.Stderr, "Error: --majordomo-key-id is required")
		fs.Usage()
		os.Exit(1)
	}

	mkID, err := uuid.Parse(*majordomoKeyID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid majordomo-key-id: %v\n", err)
		os.Exit(1)
	}

	store := connectDB(*configPath, nil)
	defer store.Close()

	// Verify the Majordomo key exists
	_, err = store.GetAPIKeyByID(context.Background(), mkID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Majordomo key not found: %v\n", err)
		os.Exit(1)
	}

	plaintext, hash, err := auth.GenerateProxyKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating proxy key: %v\n", err)
		os.Exit(1)
	}

	input := &models.CreateProxyKeyInput{
		Name: *name,
	}
	if *description != "" {
		input.Description = description
	}

	pk, err := store.CreateProxyKey(context.Background(), hash, mkID, input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating proxy key: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Proxy key created successfully!")
	fmt.Println()
	fmt.Printf("ID:                %s\n", pk.ID)
	fmt.Printf("Name:              %s\n", pk.Name)
	fmt.Printf("Majordomo Key ID:  %s\n", pk.MajordomoAPIKeyID)
	fmt.Println()
	fmt.Println("IMPORTANT: Save this key - it will not be shown again:")
	fmt.Println()
	fmt.Printf("  %s\n", plaintext)
	fmt.Println()
}

func runProxyKeysList(args []string) {
	fs := flag.NewFlagSet("proxy-keys list", flag.ExitOnError)
	majordomoKeyID := fs.String("majordomo-key-id", "", "Filter by Majordomo API key ID")
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(args)

	if *majordomoKeyID == "" {
		fmt.Fprintln(os.Stderr, "Error: --majordomo-key-id is required")
		fs.Usage()
		os.Exit(1)
	}

	mkID, err := uuid.Parse(*majordomoKeyID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid majordomo-key-id: %v\n", err)
		os.Exit(1)
	}

	store := connectDB(*configPath, nil)
	defer store.Close()

	keys, err := store.ListProxyKeys(context.Background(), mkID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing proxy keys: %v\n", err)
		os.Exit(1)
	}

	if len(keys) == 0 {
		fmt.Println("No proxy keys found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSTATUS\tCREATED\tREQUESTS")
	for _, k := range keys {
		status := "active"
		if !k.IsActive {
			status = "revoked"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n",
			k.ID, k.Name, status,
			k.CreatedAt.Format("2006-01-02"),
			k.RequestCount)
	}
	w.Flush()
}

func runProxyKeysGet(args []string) {
	fs := flag.NewFlagSet("proxy-keys get", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Error: proxy key ID required")
		fmt.Fprintln(os.Stderr, "Usage: majordomo proxy-keys get <id>")
		os.Exit(1)
	}

	id, err := uuid.Parse(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid proxy key ID: %v\n", err)
		os.Exit(1)
	}

	store := connectDB(*configPath, nil)
	defer store.Close()

	pk, err := store.GetProxyKeyByID(context.Background(), id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("ID:                %s\n", pk.ID)
	fmt.Printf("Name:              %s\n", pk.Name)
	if pk.Description != nil && *pk.Description != "" {
		fmt.Printf("Description:       %s\n", *pk.Description)
	}
	fmt.Printf("Majordomo Key ID:  %s\n", pk.MajordomoAPIKeyID)
	fmt.Printf("Status:            %s\n", proxyKeyStatusString(pk))
	fmt.Printf("Created:           %s\n", pk.CreatedAt.Format(time.RFC3339))
	if pk.RevokedAt != nil {
		fmt.Printf("Revoked:           %s\n", pk.RevokedAt.Format(time.RFC3339))
	}
	if pk.LastUsedAt != nil {
		fmt.Printf("Last Used:         %s\n", pk.LastUsedAt.Format(time.RFC3339))
	}
	fmt.Printf("Request Count:     %d\n", pk.RequestCount)
}

func runProxyKeysRevoke(args []string) {
	fs := flag.NewFlagSet("proxy-keys revoke", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Error: proxy key ID required")
		fmt.Fprintln(os.Stderr, "Usage: majordomo proxy-keys revoke <id>")
		os.Exit(1)
	}

	id, err := uuid.Parse(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid proxy key ID: %v\n", err)
		os.Exit(1)
	}

	store := connectDB(*configPath, nil)
	defer store.Close()

	err = store.RevokeProxyKey(context.Background(), id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error revoking proxy key: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Proxy key %s has been revoked.\n", id)
}

func runProxyKeysSetProvider(args []string) {
	fs := flag.NewFlagSet("proxy-keys set-provider", flag.ExitOnError)
	provider := fs.String("provider", "", "Provider name (e.g., openai, anthropic, gemini) (required)")
	apiKey := fs.String("api-key", "", "Provider API key (required)")
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Error: proxy key ID required")
		fmt.Fprintln(os.Stderr, "Usage: majordomo proxy-keys set-provider <proxy-key-id> --provider <name> --api-key <key>")
		os.Exit(1)
	}

	if *provider == "" {
		fmt.Fprintln(os.Stderr, "Error: --provider is required")
		os.Exit(1)
	}

	if *apiKey == "" {
		fmt.Fprintln(os.Stderr, "Error: --api-key is required")
		os.Exit(1)
	}

	id, err := uuid.Parse(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid proxy key ID: %v\n", err)
		os.Exit(1)
	}

	cfg := loadConfig(*configPath)
	secretStore, err := newSecretStoreFromConfig(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	store := connectDB(*configPath, nil)
	defer store.Close()

	// Verify proxy key exists
	_, err = store.GetProxyKeyByID(context.Background(), id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: proxy key not found: %v\n", err)
		os.Exit(1)
	}

	encrypted, err := secretStore.Encrypt(*apiKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error encrypting API key: %v\n", err)
		os.Exit(1)
	}

	err = store.SetProviderMapping(context.Background(), id, *provider, encrypted)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error setting provider mapping: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Provider mapping set: %s → %s (encrypted)\n", *provider, id)
}

func runProxyKeysRemoveProvider(args []string) {
	fs := flag.NewFlagSet("proxy-keys remove-provider", flag.ExitOnError)
	provider := fs.String("provider", "", "Provider name (required)")
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Error: proxy key ID required")
		fmt.Fprintln(os.Stderr, "Usage: majordomo proxy-keys remove-provider <proxy-key-id> --provider <name>")
		os.Exit(1)
	}

	if *provider == "" {
		fmt.Fprintln(os.Stderr, "Error: --provider is required")
		os.Exit(1)
	}

	id, err := uuid.Parse(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid proxy key ID: %v\n", err)
		os.Exit(1)
	}

	store := connectDB(*configPath, nil)
	defer store.Close()

	err = store.DeleteProviderMapping(context.Background(), id, *provider)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error removing provider mapping: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Provider mapping removed: %s for proxy key %s\n", *provider, id)
}

func runProxyKeysListProviders(args []string) {
	fs := flag.NewFlagSet("proxy-keys list-providers", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Error: proxy key ID required")
		fmt.Fprintln(os.Stderr, "Usage: majordomo proxy-keys list-providers <proxy-key-id>")
		os.Exit(1)
	}

	id, err := uuid.Parse(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid proxy key ID: %v\n", err)
		os.Exit(1)
	}

	store := connectDB(*configPath, nil)
	defer store.Close()

	mappings, err := store.ListProviderMappings(context.Background(), id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing provider mappings: %v\n", err)
		os.Exit(1)
	}

	if len(mappings) == 0 {
		fmt.Println("No provider mappings found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tCREATED\tUPDATED")
	for _, m := range mappings {
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			m.Provider,
			m.CreatedAt.Format("2006-01-02"),
			m.UpdatedAt.Format("2006-01-02"))
	}
	w.Flush()
}

func proxyKeyStatusString(pk *models.ProxyKey) string {
	if !pk.IsActive {
		return "revoked"
	}
	return "active"
}

func newSecretStoreFromConfig(cfg *config.Config) (secrets.SecretStore, error) {
	if cfg.Secrets.EncryptionKey == "" {
		return nil, fmt.Errorf("secrets.encryption_key is required (set MAJORDOMO_SECRETS_ENCRYPTION_KEY)")
	}
	return secrets.NewAESStore(cfg.Secrets.EncryptionKey)
}
