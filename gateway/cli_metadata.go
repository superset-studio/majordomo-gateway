package gateway

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/google/uuid"
)

func runMetadata(args []string) {
	if len(args) < 1 {
		printMetadataUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "reindex":
		runMetadataReindex(args[1:])
	case "help", "-h", "--help":
		printMetadataUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown metadata subcommand: %s\n\n", args[0])
		printMetadataUsage()
		os.Exit(1)
	}
}

func printMetadataUsage() {
	fmt.Println(`Usage: majordomo metadata <subcommand> [options]

Subcommands:
  reindex   Populate indexed_metadata from raw_metadata for all active keys

Run 'majordomo metadata <subcommand> --help' for more information.`)
}

func runMetadataReindex(args []string) {
	fs := flag.NewFlagSet("metadata reindex", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	apiKeyID := fs.String("api-key-id", "", "Only reindex for a specific API key (UUID)")
	batchSize := fs.Int("batch-size", 1000, "Number of rows to update per batch")
	fs.Parse(args)

	store := connectDB(*configPath)
	defer store.Close()

	ctx := context.Background()

	type pair struct {
		apiKeyID uuid.UUID
		keyName  string
	}
	var pairs []pair

	if *apiKeyID != "" {
		id, err := uuid.Parse(*apiKeyID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid API key ID: %v\n", err)
			os.Exit(1)
		}
		names, err := store.ListActiveMetadataKeysByAPIKey(ctx, id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing active keys: %v\n", err)
			os.Exit(1)
		}
		for _, name := range names {
			pairs = append(pairs, pair{apiKeyID: id, keyName: name})
		}
	} else {
		all, err := store.ListAllActiveMetadataKeys(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing active keys: %v\n", err)
			os.Exit(1)
		}
		for _, a := range all {
			pairs = append(pairs, pair{apiKeyID: a.APIKeyID, keyName: a.KeyName})
		}
	}

	if len(pairs) == 0 {
		fmt.Println("No active metadata keys found.")
		return
	}

	fmt.Printf("Reindexing %d active metadata key(s) with batch size %d...\n\n", len(pairs), *batchSize)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "API KEY ID\tKEY NAME\tROWS UPDATED")
	for _, p := range pairs {
		n, err := store.BackfillIndexedMetadata(ctx, p.apiKeyID, p.keyName, *batchSize)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reindexing %s/%s: %v\n", p.apiKeyID, p.keyName, err)
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%d\n", p.apiKeyID, p.keyName, n)
	}
	w.Flush()
	fmt.Println("\nDone.")
}
