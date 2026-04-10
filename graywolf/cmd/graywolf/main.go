// graywolf entry point. All runtime wiring lives in pkg/app; this file
// is a thin dispatch shim responsible for build-time version injection,
// subcommand routing, and signal handling. The normal-path main() body
// is app.New(cfg, logger).Run(ctx).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chrissnell/graywolf/pkg/app"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webauth"
)

// Version and GitCommit are injected at build time via -ldflags. Both
// sides of the build (Go + Rust) format their display string as
// "v<Version>-<GitCommit>"; the Rust side must produce a byte-identical
// string so the startup banner's mismatch check works.
var (
	Version   = "dev"
	GitCommit = "unknown"
)

// fullVersion returns the display-format version string shared with
// graywolf-modem, e.g. "v0.7.13-abcdef1" or "v0.7.13-abcdef1-dirty".
func fullVersion() string {
	return fmt.Sprintf("v%s-%s", Version, GitCommit)
}

func main() {
	// Handle subcommands before flag parsing so "graywolf auth
	// set-password --user foo" does not collide with the main flag
	// set.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "auth":
			handleAuthSubcommand(os.Args[2:])
			return
		case "version":
			fmt.Println(fullVersion())
			return
		}
	}

	cfg, err := app.ParseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	cfg.Version = Version
	cfg.GitCommit = GitCommit

	logLevel := slog.LevelInfo
	if cfg.Debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	a := app.New(cfg, logger)
	if err := a.Run(ctx); err != nil {
		logger.Error("graywolf exited with error", "err", err)
		os.Exit(1)
	}
}

// --- Auth subcommand ----------------------------------------------------------

// handleAuthSubcommand dispatches graywolf auth {set-password,list-users,delete-user}.
//
// TODO(work order 06, commit 6): extract into its own package
// cmd/graywolf/authcli so the main file is a pure dispatch shim.
func handleAuthSubcommand(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: graywolf auth {set-password|list-users|delete-user} [options]")
		os.Exit(1)
	}

	fs := flag.NewFlagSet("auth", flag.ExitOnError)
	dbPath := fs.String("config", "./graywolf.db", "path to SQLite config database")

	switch args[0] {
	case "set-password":
		user := ""
		remaining := args[1:]
		for i := 0; i < len(remaining); i++ {
			if remaining[i] == "--user" && i+1 < len(remaining) {
				user = remaining[i+1]
				remaining = append(remaining[:i], remaining[i+2:]...)
				break
			}
			if strings.HasPrefix(remaining[i], "--user=") {
				user = strings.TrimPrefix(remaining[i], "--user=")
				remaining = append(remaining[:i], remaining[i+1:]...)
				break
			}
		}
		fs.Parse(remaining)
		if user == "" {
			user = "admin"
		}

		fmt.Printf("Enter password for %s: ", user)
		var password string
		fmt.Scanln(&password)
		if password == "" {
			fmt.Fprintln(os.Stderr, "error: empty password")
			os.Exit(1)
		}

		store, authStore := openAuthDB(*dbPath)
		defer store.Close()

		hash, err := webauth.HashPassword(password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		ctx := context.Background()
		existing, err := authStore.GetUserByUsername(ctx, user)
		if err != nil {
			if _, err := authStore.CreateUser(ctx, user, hash); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Created user %s\n", user)
		} else {
			existing.PasswordHash = hash
			store.DB().Save(existing)
			fmt.Printf("Updated password for %s\n", user)
		}

	case "list-users":
		fs.Parse(args[1:])
		_, authStore := openAuthDB(*dbPath)

		users, err := authStore.ListUsers(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if len(users) == 0 {
			fmt.Println("No users configured.")
			return
		}
		fmt.Printf("%-20s %-20s\n", "USERNAME", "CREATED")
		for _, u := range users {
			fmt.Printf("%-20s %-20s\n", u.Username, u.CreatedAt.Format(time.RFC3339))
		}

	case "delete-user":
		fs.Parse(args[1:])
		remaining := fs.Args()
		if len(remaining) == 0 {
			fmt.Fprintln(os.Stderr, "usage: graywolf auth delete-user USERNAME")
			os.Exit(1)
		}
		username := remaining[0]
		_, authStore := openAuthDB(*dbPath)

		if err := authStore.DeleteUser(context.Background(), username); err != nil {
			fmt.Fprintf(os.Stderr, "error deleting %s: %v\n", username, err)
			os.Exit(1)
		}
		fmt.Printf("Deleted user %s\n", username)

	default:
		fmt.Fprintf(os.Stderr, "unknown auth subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func openAuthDB(dbPath string) (*configstore.Store, *webauth.AuthStore) {
	store, err := configstore.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	authStore, err := webauth.NewAuthStore(store.DB())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing auth: %v\n", err)
		os.Exit(1)
	}
	return store, authStore
}
