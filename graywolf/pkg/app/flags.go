package app

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// ParseFlags parses the graywolf command-line flags (everything after
// the program name, i.e. os.Args[1:]) into a Config. It uses a fresh
// flag.FlagSet in ContinueOnError mode so callers get real errors
// instead of an os.Exit, making it testable.
//
// Help output is written to os.Stderr. When the user passes -h or
// --help the FlagSet prints its usage and ParseFlags returns the
// sentinel flag.ErrHelp wrapped with %w, so the caller can detect it
// via errors.Is and exit 0 rather than 2.
//
// Version and GitCommit are left zero; the main shim sets them from
// its build-time ldflags constants before calling app.New.
func ParseFlags(args []string) (Config, error) {
	return parseFlagsTo(args, os.Stderr)
}

// parseFlagsTo is the test hook: it lets tests redirect the FlagSet's
// help/error output to an io.Writer rather than os.Stderr so `go test
// -v` does not get cluttered with "-unknown: flag provided but not
// defined" noise. Passing nil routes output to io.Discard.
func parseFlagsTo(args []string, w io.Writer) (Config, error) {
	cfg := DefaultConfig()

	fs := flag.NewFlagSet("graywolf", flag.ContinueOnError)
	if w == nil {
		w = io.Discard
	}
	fs.SetOutput(w)

	fs.StringVar(&cfg.DBPath, "config", cfg.DBPath, "path to SQLite config database")
	fs.StringVar(&cfg.ModemPath, "modem", "",
		"path to graywolf-modem binary (default: $GRAYWOLF_MODEM, then next to graywolf, then ./target/release/graywolf-modem, then $PATH)")
	fs.StringVar(&cfg.HTTPAddr, "http", cfg.HTTPAddr, "HTTP listen address")
	fs.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", cfg.ShutdownTimeout,
		"max time to wait for clean shutdown")
	fs.StringVar(&cfg.FlacFile, "flac", "", "override audio device with a FLAC file for testing")
	fs.BoolVar(&cfg.Debug, "debug", false, "enable debug-level logging")

	if err := fs.Parse(args); err != nil {
		// Preserve flag.ErrHelp as the wrapped cause so callers can
		// errors.Is(err, flag.ErrHelp) to distinguish "user asked for
		// help" from a real parse failure.
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}
	if leftover := fs.Args(); len(leftover) > 0 {
		return Config{}, fmt.Errorf("unexpected positional arguments: %v", leftover)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

