package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/superset-studio/majordomo-gateway/internal/models"
)

func runUsers(args []string) {
	if len(args) < 1 {
		printUsersUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "create":
		runUsersCreate(args[1:])
	case "list":
		runUsersList(args[1:])
	case "help", "-h", "--help":
		printUsersUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown users subcommand: %s\n\n", args[0])
		printUsersUsage()
		os.Exit(1)
	}
}

func printUsersUsage() {
	fmt.Println(`Usage: majordomo users <subcommand> [options]

Subcommands:
  create    Create a new user
  list      List all users

Run 'majordomo users <subcommand> --help' for more information.`)
}

func runUsersCreate(args []string) {
	fs := flag.NewFlagSet("users create", flag.ExitOnError)
	username := fs.String("username", "", "Username (required)")
	password := fs.String("password", "", "Password (required)")
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(args)

	if *username == "" {
		fmt.Fprintln(os.Stderr, "Error: --username is required")
		fs.Usage()
		os.Exit(1)
	}

	if *password == "" {
		fmt.Fprintln(os.Stderr, "Error: --password is required")
		fs.Usage()
		os.Exit(1)
	}

	store := connectDB(*configPath, nil)
	defer store.Close()

	input := &models.CreateUserInput{
		Username: *username,
		Password: *password,
	}

	user, err := store.CreateUser(context.Background(), input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating user: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("User created successfully!")
	fmt.Println()
	fmt.Printf("ID:       %s\n", user.ID)
	fmt.Printf("Username: %s\n", user.Username)
	fmt.Println()
}

func runUsersList(args []string) {
	fs := flag.NewFlagSet("users list", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(args)

	store := connectDB(*configPath, nil)
	defer store.Close()

	users, err := store.ListUsers(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing users: %v\n", err)
		os.Exit(1)
	}

	if len(users) == 0 {
		fmt.Println("No users found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tUSERNAME\tSTATUS\tCREATED")
	for _, u := range users {
		status := "active"
		if !u.IsActive {
			status = "inactive"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			u.ID, u.Username, status,
			u.CreatedAt.Format("2006-01-02"))
	}
	w.Flush()
}
