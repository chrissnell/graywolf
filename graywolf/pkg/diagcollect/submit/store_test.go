package submit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

func TestStorageDir_DefaultUsesLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/tmp/fakehome")
	got := StorageDir()
	if got != "/tmp/fakehome/.local/state/graywolf/flares" {
		t.Fatalf("got %q", got)
	}
}

func TestStorageDir_HonorsXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg")
	got := StorageDir()
	if got != "/tmp/xdg/graywolf/flares" {
		t.Fatalf("got %q", got)
	}
}

func TestSaveLoadToken_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	resp := flareschema.SubmitResponse{
		FlareID:       "abc",
		PortalToken:   "tok",
		PortalURL:     "https://x",
		SchemaVersion: 1,
	}
	path, err := SaveTokenAt(dir, resp)
	if err != nil {
		t.Fatalf("SaveTokenAt: %v", err)
	}
	if !strings.HasSuffix(path, "abc.json") {
		t.Fatalf("path = %q", path)
	}
	got, err := LoadTokenAt(dir, "abc")
	if err != nil {
		t.Fatalf("LoadTokenAt: %v", err)
	}
	if got != resp {
		t.Fatalf("round trip: %+v vs %+v", got, resp)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestLoadTokenAt_MissingReturnsErr(t *testing.T) {
	_, err := LoadTokenAt(t.TempDir(), "no-such")
	if err == nil {
		t.Fatal("err = nil, want missing")
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

func TestSaveTokenAt_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	bad := flareschema.SubmitResponse{FlareID: "../oops", PortalToken: "x"}
	_, err := SaveTokenAt(dir, bad)
	if err == nil {
		t.Fatal("err = nil, want path-escape rejection")
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "oops.json")); err == nil {
		t.Fatal("file written outside storage dir")
	}
}
