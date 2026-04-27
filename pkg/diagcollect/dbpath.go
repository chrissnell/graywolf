package diagcollect

import (
	"errors"
	"os"
	"path/filepath"
)

// DiscoverOptions feeds DiscoverConfigDB. Every field is testable in
// isolation; production callers leave Stat nil to use the real
// filesystem.
type DiscoverOptions struct {
	// Explicit is the --db flag. Used verbatim when set; the file is
	// NOT stat-checked because the eventual configstore.Open call
	// produces a clearer error message ("permission denied opening
	// /tmp/foo.db") than a pre-flight existence check would.
	Explicit string
	// Env is the value of $GRAYWOLF_DB. Stat-checked because it's a
	// best-effort fallback the operator may have set in their shell
	// profile months ago and forgotten about.
	Env string
	// ServiceInstall is the systemd-install location.
	// Defaults to /var/lib/graywolf/graywolf.db when empty.
	ServiceInstall string
	// UserConfigDir is os.UserConfigDir() + "/Graywolf". Empty when
	// os.UserConfigDir errored.
	UserConfigDir string
	// Workdir is the CWD-equivalent. Defaults to "." when empty.
	Workdir string
	// Stat is the existence probe. Defaults to a real os.Stat wrapper.
	Stat func(string) bool
}

// ErrConfigDBNotFound signals that no candidate path matched. The
// caller should warn the operator and continue with a degraded flare
// (config_db_unavailable issue).
var ErrConfigDBNotFound = errors.New("graywolf.db not found in any of the documented discovery locations")

// ErrIsConfigDBNotFound returns true when err is ErrConfigDBNotFound
// (or wraps it). Provided as a helper so callers don't have to import
// errors just to check this one sentinel.
func ErrIsConfigDBNotFound(err error) bool {
	return errors.Is(err, ErrConfigDBNotFound)
}

// DiscoverConfigDB returns the first path that exists from the
// documented five-step lookup order, plus a short string identifying
// which step matched (useful for --verbose output).
func DiscoverConfigDB(opts DiscoverOptions) (string, string, error) {
	if opts.Stat == nil {
		opts.Stat = func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		}
	}
	if opts.ServiceInstall == "" {
		opts.ServiceInstall = "/var/lib/graywolf/graywolf.db"
	}
	if opts.UserConfigDir == "" {
		if d, err := os.UserConfigDir(); err == nil {
			opts.UserConfigDir = filepath.Join(d, "Graywolf")
		}
	}
	if opts.Workdir == "" {
		opts.Workdir = "."
	}
	return discoverConfigDBFrom(opts)
}

// discoverConfigDBFrom is the testable inner. It does NOT default
// fields — the wrapper above does so each test fixture is explicit
// about which inputs are in play.
func discoverConfigDBFrom(opts DiscoverOptions) (string, string, error) {
	stat := opts.Stat
	if stat == nil {
		stat = func(string) bool { return false }
	}

	if opts.Explicit != "" {
		return opts.Explicit, "flag", nil
	}
	if opts.Env != "" && stat(opts.Env) {
		return opts.Env, "env", nil
	}
	if opts.ServiceInstall != "" && stat(opts.ServiceInstall) {
		return opts.ServiceInstall, "service_install", nil
	}
	if opts.UserConfigDir != "" {
		cand := filepath.Join(opts.UserConfigDir, "graywolf.db")
		if stat(cand) {
			return cand, "user_config", nil
		}
	}
	if opts.Workdir != "" {
		cand := filepath.Join(opts.Workdir, "graywolf.db")
		if stat(cand) {
			return cand, "cwd", nil
		}
	}
	return "", "", ErrConfigDBNotFound
}
