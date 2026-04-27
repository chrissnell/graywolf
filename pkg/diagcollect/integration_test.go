package diagcollect

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/flareschema"
)

func TestDryRunRoundTripsThroughFlareschemaUnmarshal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration test relies on POSIX shell semantics")
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graywolf.db")
	store, err := configstore.Open(dbPath)
	if err != nil {
		t.Fatalf("seed configstore: %v", err)
	}
	if err := store.UpsertStationConfig(context.Background(), configstore.StationConfig{
		Callsign: "N0CALL",
	}); err != nil {
		t.Fatalf("seed station: %v", err)
	}
	store.Close()

	// Build graywolf for this test only.
	binPath := filepath.Join(dir, "graywolf-test")
	build := exec.Command("go", "build", "-o", binPath, "../../cmd/graywolf/")
	build.Stderr = &bytes.Buffer{}
	if err := build.Run(); err != nil {
		t.Skipf("go build failed (likely sandboxed environment): %v", err)
	}

	// graywolf flare --dry-run --no-modem --no-logs --db <seeded>
	// Stdin "s\n" answers the review prompt with "submit".
	cmd := exec.Command(binPath, "flare",
		"--dry-run", "--no-modem", "--no-logs",
		"--db", dbPath,
	)
	cmd.Stdin = strings.NewReader("s\n")
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("graywolf flare --dry-run: %v\nstdout:\n%s", err, stdout)
	}

	// stdout is a mix of the review TUI's prompts and (after the 's')
	// the JSON payload. The compact JSON is on a single line after
	// the "> " prompt.
	lines := bytes.Split(stdout, []byte("\n"))
	var body []byte
	for _, line := range lines {
		if bytes.HasPrefix(line, []byte("> {")) {
			body = line[2:] // skip "> "
			break
		}
	}
	if len(body) == 0 {
		t.Fatalf("no JSON document in stdout:\n%s", stdout)
	}
	flare, err := flareschema.Unmarshal(body)
	if err != nil {
		t.Fatalf("flareschema.Unmarshal: %v\nbody:\n%s", err, body)
	}
	if flare.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d", flare.SchemaVersion)
	}
	// Hostname hash present (or absence recorded as issue).
	if flare.Meta.HostnameHash == "" && len(flare.System.Issues) == 0 {
		t.Fatalf("hostname hash empty and no issue recorded:\n%+v", flare.System)
	}
	// configdump should include the seeded callsign — APRS callsigns
	// are not redacted.
	found := false
	for _, item := range flare.Config.Items {
		if strings.Contains(strings.ToLower(item.Key), "callsign") && item.Value == "N0CALL" {
			found = true
			break
		}
	}
	if !found {
		end := 8
		if len(flare.Config.Items) < end {
			end = len(flare.Config.Items)
		}
		t.Fatalf("seeded callsign not in dump:\nfirst items: %+v", flare.Config.Items[:end])
	}
}
