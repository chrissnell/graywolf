// Package app is the graywolf composition root. It parses flags, wires
// every runtime component (configstore, modembridge, TX governor, KISS,
// AGW, digipeater, GPS, beacon, iGate, HTTP) into an App, and exposes a
// single Run entry point that the main shim calls.
//
// Splitting this out of main makes startup/shutdown ordering testable and
// gives new services a single, obvious place to be wired in.
package app

import (
	"errors"
	"fmt"
	"time"
)

// Config is the fully-resolved runtime configuration for an App instance.
// Every field corresponds to either a command-line flag or a value
// injected by the main shim at build time (Version, GitCommit). New
// fields must be added here rather than threaded through Run as extra
// parameters.
type Config struct {
	// DBPath is the path to the SQLite config database (-config).
	DBPath string

	// ModemPath is the explicit path to the graywolf-modem binary
	// (-modem). Empty means auto-resolve via resolveModemPath.
	ModemPath string

	// HTTPAddr is the web server listen address (-http).
	HTTPAddr string

	// ShutdownTimeout bounds how long Stop will wait for components to
	// exit cleanly (-shutdown-timeout).
	ShutdownTimeout time.Duration

	// HistoryDBPath is the path to the optional position-history SQLite
	// database (-history-db). The web UI toggles enabled/disabled but
	// the path is set here so service managers (systemd, Windows SCM)
	// can point it at a writable location.
	HistoryDBPath string

	// FlacFile, when non-empty, overrides the first audio device with a
	// FLAC file for offline testing (-flac).
	FlacFile string

	// Debug enables debug-level logging (-debug).
	Debug bool

	// SessionMaxAge, when non-zero, overrides the webauth package's
	// default session cookie lifetime. Zero means use the webauth
	// default (currently 7 days). Threaded through wireHTTP into
	// webauth.Handlers; not yet surfaced as a CLI flag.
	SessionMaxAge time.Duration

	// Version and GitCommit are injected by the main shim from
	// -ldflags-provided build constants. They are not parsed from
	// command-line flags.
	Version   string
	GitCommit string
}

// DefaultConfig returns a Config populated with the same defaults
// ParseFlags applies when no flags are provided. Tests that need a
// minimal-but-valid Config should start from this.
func DefaultConfig() Config {
	return Config{
		DBPath:          defaultDBPath(),
		HistoryDBPath:   defaultHistoryDBPath(),
		HTTPAddr:        "127.0.0.1:8080",
		ShutdownTimeout: 10 * time.Second,
	}
}

// Validate performs basic sanity checks on the Config. It is intentionally
// cheap: filesystem checks (does DBPath's directory exist, is FlacFile
// readable) are deferred to the actual Start path so that a programmer
// can construct a Config in a test without having the real paths present.
func (c Config) Validate() error {
	if c.DBPath == "" {
		return errors.New("DBPath is required")
	}
	if c.HTTPAddr == "" {
		return errors.New("HTTPAddr is required")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("ShutdownTimeout must be > 0 (got %s)", c.ShutdownTimeout)
	}
	return nil
}

// FullVersion returns the display-format version string shared with
// graywolf-modem, e.g. "v0.7.13-abcdef1". The Rust side must produce a
// byte-identical string so the startup banner's mismatch check works.
func (c Config) FullVersion() string {
	return fmt.Sprintf("v%s-%s", c.Version, c.GitCommit)
}
