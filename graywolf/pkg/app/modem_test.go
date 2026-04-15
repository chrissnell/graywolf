package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveModemPathExplicit(t *testing.T) {
	// An explicit path wins even if it does not exist; the error from
	// a missing binary is surfaced later at Start time with the exact
	// string the user passed.
	got, err := resolveModemPathFrom("/does/not/exist/graywolf-modem", "", "")
	if err != nil {
		t.Fatalf("explicit: %v", err)
	}
	if got != "/does/not/exist/graywolf-modem" {
		t.Errorf("explicit: got %q", got)
	}
}

func TestResolveModemPathEnv(t *testing.T) {
	got, err := resolveModemPathFrom("", "", "/from/env/graywolf-modem")
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	if got != "/from/env/graywolf-modem" {
		t.Errorf("env: got %q", got)
	}
}

func TestResolveModemPathSibling(t *testing.T) {
	dir := t.TempDir()
	sibling := filepath.Join(dir, modemBinaryName)
	if err := os.WriteFile(sibling, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeExe := filepath.Join(dir, "graywolf")
	if err := os.WriteFile(fakeExe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveModemPathFrom("", fakeExe, "")
	if err != nil {
		t.Fatalf("sibling: %v", err)
	}
	if got != sibling {
		// EvalSymlinks on darwin will canonicalize /var → /private/var;
		// compare with EvalSymlinks applied to both so the test is
		// platform-neutral.
		resolvedGot, _ := filepath.EvalSymlinks(got)
		resolvedWant, _ := filepath.EvalSymlinks(sibling)
		if resolvedGot != resolvedWant {
			t.Errorf("sibling: got %q, want %q", got, sibling)
		}
	}
}

func TestResolveModemPathExplicitBeatsEnv(t *testing.T) {
	got, err := resolveModemPathFrom("/explicit/modem", "", "/env/modem")
	if err != nil {
		t.Fatalf("explicit-over-env: %v", err)
	}
	if got != "/explicit/modem" {
		t.Errorf("explicit-over-env: got %q", got)
	}
}

func TestResolveModemPathNotFound(t *testing.T) {
	// Empty everywhere, no sibling, PATH lookup will (almost certainly)
	// fail in a sandbox. The error message should name the flag and env
	// var so users can recover.
	dir := t.TempDir()
	fakeExe := filepath.Join(dir, "graywolf") // sibling does not exist
	if err := os.WriteFile(fakeExe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Swap PATH so exec.LookPath cannot find a real graywolf-modem
	// anywhere on the host.
	t.Setenv("PATH", dir)

	// Work in a temp CWD so the ./target/release fallback cannot match.
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	_, err = resolveModemPathFrom("", fakeExe, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "graywolf-modem binary not found") {
		t.Errorf("error message should point user at recovery: %v", err)
	}
}
