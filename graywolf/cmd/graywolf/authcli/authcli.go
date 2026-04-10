// Package authcli implements the `graywolf auth` subcommand tree:
// set-password, list-users, and delete-user. It is a separate package
// so the main dispatch shim stays tiny; the auth CLI has its own
// flag set and does not share any global state with the normal
// service-running path.
package authcli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/app"
	"github.com/chrissnell/graywolf/pkg/webauth"
)

// Run dispatches one of the three auth subcommands. args is the
// slice after "graywolf auth", i.e. os.Args[2:]. Errors are returned
// rather than printed/os.Exit'd so the calling shim controls the
// exit code in one place.
func Run(args []string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if len(args) == 0 {
		return errors.New("usage: graywolf auth {set-password|list-users|delete-user} [options]")
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "set-password":
		return runSetPassword(rest)
	case "list-users":
		return runListUsers(rest)
	case "delete-user":
		return runDeleteUser(rest)
	case "-h", "--help", "help":
		printHelp(os.Stderr)
		return nil
	default:
		return fmt.Errorf("unknown auth subcommand: %s", sub)
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "usage: graywolf auth SUBCOMMAND [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "subcommands:")
	fmt.Fprintln(w, "  set-password --user USERNAME   create or reset a password")
	fmt.Fprintln(w, "  list-users                     print every configured user")
	fmt.Fprintln(w, "  delete-user USERNAME           remove a user")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "options:")
	fmt.Fprintln(w, "  -config PATH    path to SQLite config database (default ./graywolf.db)")
}

// newAuthFlagSet builds a FlagSet that returns errors (not os.Exit)
// on bad usage and routes help output to stderr consistently.
func newAuthFlagSet(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dbPath := fs.String("config", "./graywolf.db", "path to SQLite config database")
	return fs, dbPath
}

func runSetPassword(args []string) error {
	// The old handler stripped --user out manually so it could coexist
	// with the -config flag parser. Preserve that behavior: pull --user
	// out of args, then parse the remainder with a normal FlagSet.
	user := ""
	remaining := make([]string, 0, len(args))
	i := 0
	for i < len(args) {
		if args[i] == "--user" && i+1 < len(args) {
			user = args[i+1]
			i += 2
			continue
		}
		if strings.HasPrefix(args[i], "--user=") {
			user = strings.TrimPrefix(args[i], "--user=")
			i++
			continue
		}
		remaining = append(remaining, args[i])
		i++
	}

	fs, dbPath := newAuthFlagSet("auth set-password")
	if err := fs.Parse(remaining); err != nil {
		return err
	}
	if user == "" {
		user = "admin"
	}

	fmt.Printf("Enter password for %s: ", user)
	var password string
	fmt.Scanln(&password)
	if password == "" {
		return errors.New("empty password")
	}

	store, authStore, cleanup, err := app.OpenStoreAndAuth(*dbPath)
	if err != nil {
		return err
	}
	defer cleanup()

	hash, err := webauth.HashPassword(password)
	if err != nil {
		return err
	}

	ctx := context.Background()
	existing, err := authStore.GetUserByUsername(ctx, user)
	if err != nil {
		if _, err := authStore.CreateUser(ctx, user, hash); err != nil {
			return err
		}
		fmt.Printf("Created user %s\n", user)
		return nil
	}
	existing.PasswordHash = hash
	store.DB().Save(existing)
	fmt.Printf("Updated password for %s\n", user)
	return nil
}

func runListUsers(args []string) error {
	fs, dbPath := newAuthFlagSet("auth list-users")
	if err := fs.Parse(args); err != nil {
		return err
	}

	authStore, cleanup, err := app.OpenAuthStore(*dbPath)
	if err != nil {
		return err
	}
	defer cleanup()

	users, err := authStore.ListUsers(context.Background())
	if err != nil {
		return err
	}
	if len(users) == 0 {
		fmt.Println("No users configured.")
		return nil
	}
	fmt.Printf("%-20s %-20s\n", "USERNAME", "CREATED")
	for _, u := range users {
		fmt.Printf("%-20s %-20s\n", u.Username, u.CreatedAt.Format(time.RFC3339))
	}
	return nil
}

func runDeleteUser(args []string) error {
	fs, dbPath := newAuthFlagSet("auth delete-user")
	if err := fs.Parse(args); err != nil {
		return err
	}
	remaining := fs.Args()
	if len(remaining) == 0 {
		return errors.New("usage: graywolf auth delete-user USERNAME")
	}
	username := remaining[0]

	authStore, cleanup, err := app.OpenAuthStore(*dbPath)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := authStore.DeleteUser(context.Background(), username); err != nil {
		return fmt.Errorf("deleting %s: %w", username, err)
	}
	fmt.Printf("Deleted user %s\n", username)
	return nil
}
