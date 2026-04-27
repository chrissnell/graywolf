package submit

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPendingDir_DefaultUsesLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/tmp/fakehome")
	got := PendingDir()
	if got != "/tmp/fakehome/.local/state/graywolf" {
		t.Fatalf("got %q", got)
	}
}

func TestPendingDir_HonorsXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg")
	got := PendingDir()
	if got != "/tmp/xdg/graywolf" {
		t.Fatalf("got %q", got)
	}
}

func TestSavePendingFlareAt(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`{"schema_version":1}`)
	path, err := SavePendingFlareAt(dir, body)
	if err != nil {
		t.Fatalf("SavePendingFlareAt: %v", err)
	}
	if !strings.Contains(path, "pending-flare-") {
		t.Fatalf("path = %q", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !json.Valid(got) {
		t.Fatalf("saved body invalid: %s", got)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}
