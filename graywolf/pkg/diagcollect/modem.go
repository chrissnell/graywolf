package diagcollect

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// modemBinaryName is the platform-specific filename. Mirrors the
// constant in pkg/app/platform_*.go; duplicated here so this package
// stays a leaf with no internal-app dependencies.
func modemBinaryName() string {
	if runtime.GOOS == "windows" {
		return "graywolf-modem.exe"
	}
	return "graywolf-modem"
}

// ResolveModemPath returns the path to the graywolf-modem binary
// using the same lookup order as pkg/app/modem.go's ResolveModemPath:
// explicit → $GRAYWOLF_MODEM → sibling of running executable →
// ./target/release/<modem> → $PATH. Errors when nothing is found.
func ResolveModemPath(explicit string) (string, error) {
	exe, _ := os.Executable()
	cwd, _ := os.Getwd()
	return resolveModemPathFrom(explicit, exe, os.Getenv("GRAYWOLF_MODEM"), cwd)
}

// resolveModemPathFrom is the testable inner: explicit/exe/env/cwd
// are supplied so tests don't have to mutate process state. exe="" or
// cwd="" skip those steps respectively.
func resolveModemPathFrom(explicit, exe, env, cwd string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if env != "" {
		return env, nil
	}
	if exe != "" {
		// Resolve symlinks so /usr/local/bin/graywolf →
		// /opt/graywolf/bin still looks for the modem in the real
		// install dir.
		if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
			exe = resolved
		}
		cand := filepath.Join(filepath.Dir(exe), modemBinaryName())
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
	}
	if cwd != "" {
		cand := filepath.Join(cwd, "target", "release", modemBinaryName())
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
	}
	if p, err := exec.LookPath(modemBinaryName()); err == nil {
		return p, nil
	}
	return "", errors.New("graywolf-modem binary not found; pass --modem, set GRAYWOLF_MODEM, place it next to graywolf, or build with `make release`")
}

// RunListing executes graywolf-modem with one --list-* flag and
// returns its stdout. Failures are shaped as a *CollectorIssue so the
// caller can drop them into the relevant section's Issues slice
// without further mapping.
//
// The 8-second context timeout is the only thing preventing a hung
// modem binary from wedging the entire flare. Audio enumeration on a
// laptop with PulseAudio occasionally takes a couple of seconds; 8s
// is a comfortable upper bound.
func RunListing(bin, flag string) ([]byte, *flareschema.CollectorIssue) {
	if bin == "" {
		return nil, &flareschema.CollectorIssue{
			Kind:    "modem_unavailable",
			Message: "graywolf-modem path not resolved",
		}
	}
	if _, err := os.Stat(bin); err != nil {
		return nil, &flareschema.CollectorIssue{
			Kind:    "modem_unavailable",
			Message: fmt.Sprintf("stat %s: %v", bin, err),
			Path:    bin,
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, flag)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, &flareschema.CollectorIssue{
				Kind:    "modem_failed",
				Message: fmt.Sprintf("%s %s: exit=%d stderr=%q", filepath.Base(bin), flag, ee.ExitCode(), string(ee.Stderr)),
				Path:    bin,
			}
		}
		return nil, &flareschema.CollectorIssue{
			Kind:    "modem_failed",
			Message: fmt.Sprintf("%s %s: %v", filepath.Base(bin), flag, err),
			Path:    bin,
		}
	}
	return out, nil
}
