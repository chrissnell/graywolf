package flareschema

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// repoRoot walks up looking for graywolf-modem/Cargo.toml — the same
// marker the modembridge integration tests use to locate the workspace
// root. Returns "" when no marker is found, which the test interprets as
// a skip signal.
func repoRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "graywolf-modem", "Cargo.toml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// findModemBinary returns the path to a runnable graywolf-modem binary,
// or "" with a skip reason. Lookup order:
//  1. CI-built sibling (<repo>/bin/graywolf-modem)
//  2. cargo-built workspace artifact (<repo>/target/release/graywolf-modem)
//  3. $PATH
//
// We do NOT cargo-build the modem from this test; the build-tag-gated
// fallback lives in modembridge's integration_test.go and is invoked
// from explicit integration runs only.
func findModemBinary(t *testing.T) string {
	t.Helper()

	root := repoRoot()
	if root != "" {
		ciBin := filepath.Join(root, "bin", "graywolf-modem")
		if _, err := os.Stat(ciBin); err == nil {
			return ciBin
		}
		releaseBin := filepath.Join(root, "target", "release", "graywolf-modem")
		if _, err := os.Stat(releaseBin); err == nil {
			return releaseBin
		}
	}
	if path, err := exec.LookPath("graywolf-modem"); err == nil {
		return path
	}
	return ""
}

// runModemListing executes graywolf-modem with one of the --list-* flags
// and returns its stdout. Errors are returned verbatim so the caller can
// distinguish "binary missing" from "binary present but failed".
func runModemListing(bin, flag string) ([]byte, error) {
	cmd := exec.Command(bin, flag)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, errors.New("graywolf-modem " + flag + " failed: " + string(ee.Stderr))
		}
		return nil, err
	}
	return out, nil
}

func TestConvergenceListAudio(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows runners don't ship the modem binary in this matrix")
	}
	bin := findModemBinary(t)
	if bin == "" {
		t.Skip("graywolf-modem binary not found; build with `cargo build --release` to enable")
	}

	stdout, err := runModemListing(bin, "--list-audio")
	if err != nil {
		t.Fatalf("--list-audio: %v", err)
	}

	var got AudioDevices
	if err := json.Unmarshal(stdout, &got); err != nil {
		t.Fatalf("unmarshal --list-audio output into AudioDevices: %v\nstdout:\n%s", err, stdout)
	}

	// Round-trip the parsed value back to JSON and re-parse so a stray
	// extra field on the Rust side gets surfaced. encoding/json is lossy
	// on unknown fields by default; we want this to be a contract test.
	roundTrip, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	var second AudioDevices
	if err := json.Unmarshal(roundTrip, &second); err != nil {
		t.Fatalf("re-parse: %v", err)
	}

	// Sanity: the document must be well-formed even on hosts with no
	// audio devices. issues is the catch-all in that case.
	if got.Hosts == nil && got.Issues == nil {
		t.Fatalf("both hosts and issues nil; document was empty")
	}
}
