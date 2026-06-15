package main

import (
	"os"
	"path/filepath"
	"testing"
)

// An unknown version must fail the process. The Release workflow's
// `set -euo pipefail` turns this non-zero exit into a release abort, which
// is the whole point: a missing notes.yaml entry should fail loudly rather
// than publish a blank GitHub release body.
func TestRun_UnknownVersionExitsNonzero(t *testing.T) {
	out := filepath.Join(t.TempDir(), "notes.md")
	if code := run([]string{"-version", "0.0.0-does-not-exist", "-out", out}); code == 0 {
		t.Fatalf("expected non-zero exit for unknown version, got 0")
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatalf("output file should not be written when the version is unknown")
	}
}

// Missing required flags is a usage error (exit 2), distinct from a render
// failure (exit 1).
func TestRun_MissingFlagsExitsTwo(t *testing.T) {
	if code := run(nil); code != 2 {
		t.Fatalf("expected exit 2 for missing flags, got %d", code)
	}
}

// A version present in notes.yaml renders and writes a non-empty body.
// 0.14.0 is permanent release history; the file is append-only.
func TestRun_KnownVersionWritesNote(t *testing.T) {
	out := filepath.Join(t.TempDir(), "sub", "notes.md")
	if code := run([]string{"-version", "0.14.0", "-out", out}); code != 0 {
		t.Fatalf("expected exit 0 for known version, got %d", code)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if len(b) == 0 {
		t.Fatalf("output file is empty")
	}
}
