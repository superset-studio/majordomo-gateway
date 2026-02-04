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
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

func runKeys(args []string) {
	if len(args) < 1 {
		printKeysUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "create":
		runKeysCreate(args[1:])
	case "list":
		runKeysList(args[1:])
	case "get":
		runKeysGet(args[1:])
	case "revoke":
		runKeysRevoke(args[1:])
	case "update":
		runKeysUpdate(args[1:])
	case "help", "-h", "--help":
		printKeysUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown keys subcommand: %s\n\n", args[0])
		printKeysUsage()
		os.Exit(1)
	}
}

func printKeysUsage() {
	fmt.Println(`Usage: majordomo keys <subcommand> [options]

Subcommands:
  create    Create a new API key
  list      List all API keys
  get       Get details of an API key
  revoke    Revoke an API key
  update    Update an API key

Run 'majordomo keys <subcommand> --help' for more information.`)
}

func runKeysCreate(args []string) {
	fs := flag.NewFlagSet("keys create", flag.ExitOnError)
	name := fs.String("name", "", "Name for the API key (required)")
	description := fs.String("description", "", "Description for the API key")
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(args)

	if *name == "" {
		fmt.Fprintln(os.Stderr, "Error: --name is required")
		fs.Usage()
		os.Exit(1)
	}

	store := connectDB(*configPath, nil)
	defer store.Close()

	plaintext, hash, err := auth.GenerateAPIKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating key: %v\n", err)
		os.Exit(1)
	}

	input := &models.CreateAPIKeyInput{
		Name: *name,
	}
	if *description != "" {
		input.Description = description
	}

	key, err := store.CreateAPIKey(context.Background(), hash, input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating key: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("API key created successfully!")
	fmt.Println()
	fmt.Printf("ID:   %s\n", key.ID)
	fmt.Printf("Name: %s\n", key.Name)
	fmt.Println()
	fmt.Println("IMPORTANT: Save this key - it will not be shown again:")
	fmt.Println()
	fmt.Printf("  %s\n", plaintext)
	fmt.Println()
}

func runKeysList(args []string) {
	fs := flag.NewFlagSet("keys list", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(args)

	store := connectDB(*configPath, nil)
	defer store.Close()

	keys, err := store.ListAPIKeys(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing keys: %v\n", err)
		os.Exit(1)
	}

	if len(keys) == 0 {
		fmt.Println("No API keys found.")
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

func runKeysGet(args []string) {
	fs := flag.NewFlagSet("keys get", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Error: key ID required")
		fmt.Fprintln(os.Stderr, "Usage: majordomo keys get <id>")
		os.Exit(1)
	}

	keyID := fs.Arg(0)
	id, err := uuid.Parse(keyID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid key ID: %v\n", err)
		os.Exit(1)
	}

	store := connectDB(*configPath, nil)
	defer store.Close()

	key, err := store.GetAPIKeyByID(context.Background(), id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("ID:            %s\n", key.ID)
	fmt.Printf("Name:          %s\n", key.Name)
	if key.Description != nil && *key.Description != "" {
		fmt.Printf("Description:   %s\n", *key.Description)
	}
	fmt.Printf("Status:        %s\n", statusString(key))
	fmt.Printf("Created:       %s\n", key.CreatedAt.Format(time.RFC3339))
	if key.RevokedAt != nil {
		fmt.Printf("Revoked:       %s\n", key.RevokedAt.Format(time.RFC3339))
	}
	if key.LastUsedAt != nil {
		fmt.Printf("Last Used:     %s\n", key.LastUsedAt.Format(time.RFC3339))
	}
	fmt.Printf("Request Count: %d\n", key.RequestCount)
}

func runKeysRevoke(args []string) {
	fs := flag.NewFlagSet("keys revoke", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Error: key ID required")
		fmt.Fprintln(os.Stderr, "Usage: majordomo keys revoke <id>")
		os.Exit(1)
	}

	keyID := fs.Arg(0)
	id, err := uuid.Parse(keyID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid key ID: %v\n", err)
		os.Exit(1)
	}

	store := connectDB(*configPath, nil)
	defer store.Close()

	err = store.RevokeAPIKey(context.Background(), id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error revoking key: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("API key %s has been revoked.\n", id)
}

func runKeysUpdate(args []string) {
	fs := flag.NewFlagSet("keys update", flag.ExitOnError)
	name := fs.String("name", "", "New name for the API key")
	description := fs.String("description", "", "New description")
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Error: key ID required")
		fmt.Fprintln(os.Stderr, "Usage: majordomo keys update <id> [--name NAME] [--description DESC]")
		os.Exit(1)
	}

	if *name == "" && *description == "" {
		fmt.Fprintln(os.Stderr, "Error: at least one of --name or --description is required")
		os.Exit(1)
	}

	keyID := fs.Arg(0)
	id, err := uuid.Parse(keyID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid key ID: %v\n", err)
		os.Exit(1)
	}

	store := connectDB(*configPath, nil)
	defer store.Close()

	input := &models.UpdateAPIKeyInput{}
	if *name != "" {
		input.Name = name
	}
	if *description != "" {
		input.Description = description
	}

	key, err := store.UpdateAPIKey(context.Background(), id, input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error updating key: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("API key %s updated successfully.\n", key.ID)
	fmt.Printf("Name: %s\n", key.Name)
	if key.Description != nil && *key.Description != "" {
		fmt.Printf("Description: %s\n", *key.Description)
	}
}

func statusString(key *models.APIKey) string {
	if !key.IsActive {
		return "revoked"
	}
	return "active"
}
