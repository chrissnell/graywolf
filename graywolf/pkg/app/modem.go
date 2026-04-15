package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ResolveModemPath figures out where to find the graywolf-modem binary.
// The search order is:
//  1. explicit path (used verbatim, no existence check so the resulting
//     error message points at the user-supplied path)
//  2. $GRAYWOLF_MODEM env var
//  3. sibling of the current executable — handles the installed case
//     (/usr/bin/graywolf → /usr/bin/graywolf-modem) and the dev case
//     (./bin/graywolf → ./bin/graywolf-modem) uniformly
//  4. ./target/release/graywolf-modem — lets a developer run the
//     freshly-built Go binary straight from the repo root against a
//     cargo release build without staging into bin/
//  5. $PATH lookup as a last resort
func ResolveModemPath(explicit string) (string, error) {
	exe, _ := os.Executable()
	return resolveModemPathFrom(explicit, exe, os.Getenv("GRAYWOLF_MODEM"))
}

// resolveModemPathFrom is the testable inner implementation. Callers
// supply the executable path and the GRAYWOLF_MODEM env value explicitly
// so tests can fake both without touching process state. An empty exe
// skips step 3. An empty env skips step 2.
func resolveModemPathFrom(explicit, exe, envPath string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if envPath != "" {
		return envPath, nil
	}
	if exe != "" {
		// Resolve symlinks so /usr/local/bin/graywolf → /opt/graywolf/bin
		// still looks for the modem in the real install dir.
		if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
			exe = resolved
		}
		cand := filepath.Join(filepath.Dir(exe), modemBinaryName)
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
	}
	devPath := filepath.FromSlash("./target/release/" + modemBinaryName)
	if _, err := os.Stat(devPath); err == nil {
		return devPath, nil
	}
	if p, err := exec.LookPath("graywolf-modem"); err == nil {
		return p, nil
	}
	return "", errors.New("graywolf-modem binary not found; pass -modem, set GRAYWOLF_MODEM, place it next to graywolf, or build with `make release`")
}

// QueryModemVersion runs `<path> --version` and returns its stdout
// trimmed of whitespace. The Rust side formats it to exactly match
// graywolf's own FullVersion() so string equality is sufficient.
//
// This deliberately uses a short context.WithTimeout derived from
// context.Background(): it runs during early startup before the App
// context exists, and its timeout is the only thing preventing a hung
// modem binary from wedging startup forever.
func QueryModemVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
