// graywolf entry point. All runtime wiring lives in pkg/app; this file
// is a thin dispatch shim responsible for build-time version injection,
// subcommand routing, and signal handling. The normal-path main() body
// is app.New(cfg, logger).Run(ctx).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/chrissnell/graywolf/cmd/graywolf/authcli"
	"github.com/chrissnell/graywolf/pkg/app"
)

// Version and GitCommit are injected at build time via -ldflags. Both
// sides of the build (Go + Rust) format their display string as
// "v<Version>-<GitCommit>"; the Rust side must produce a byte-identical
// string so the startup banner's mismatch check works.
var (
	Version   = "dev"
	GitCommit = "unknown"
)

func fullVersion() string {
	return fmt.Sprintf("v%s-%s", Version, GitCommit)
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Handle subcommands before flag parsing so "graywolf auth
	// set-password --user foo" does not collide with the main flag
	// set.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "auth":
			if err := authcli.Run(os.Args[2:], logger, Version); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		case "version":
			fmt.Println(fullVersion())
			return
		}
	}

	cfg, err := app.ParseFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// FlagSet already wrote usage to stderr.
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	cfg.Version = Version
	cfg.GitCommit = GitCommit

	if cfg.Debug {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := app.New(cfg, logger).Run(ctx); err != nil {
		logger.Error("graywolf exited with error", "err", err)
		os.Exit(1)
	}
}
