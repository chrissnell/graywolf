package diagcollect

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// collectorRunnerStub is the test double for diagcollect.Runner —
// returns the canned JSON each --list-* flag should produce.
type collectorRunnerStub struct {
	audio []byte
	usb   []byte
	cm108 []byte
}

func (c collectorRunnerStub) Run(bin, flag string) ([]byte, *flareschema.CollectorIssue) {
	switch flag {
	case "--list-audio":
		return c.audio, nil
	case "--list-usb":
		return c.usb, nil
	case "--list-cm108":
		return c.cm108, nil
	}
	return nil, &flareschema.CollectorIssue{Kind: "unknown_flag", Message: flag}
}

func TestCollect_PopulatesAllSections(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "graywolf.db")
	store, err := configstore.Open(dbPath)
	if err != nil {
		t.Fatalf("open configstore: %v", err)
	}
	defer store.Close()
	if err := store.UpsertStationConfig(context.Background(), configstore.StationConfig{
		Callsign: "N0CALL",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	runner := collectorRunnerStub{
		audio: []byte(`{"hosts":[]}`),
		usb:   []byte(`{"devices":[]}`),
		cm108: []byte(`[]`),
	}
	flare, err := Collect(Options{
		ConfigStore:     store,
		ConfigDBPath:    dbPath,
		ModemBinaryPath: "/fake/modem",
		Runner:          runner,
		Version:         "0.99.0",
		GitCommit:       "deadbeef",
		ModemVersion:    "v0.99.0-deadbeef",
		LogLimit:        50,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if flare.SchemaVersion != 1 {
		t.Fatalf("schema version: %d", flare.SchemaVersion)
	}
	if flare.Meta.GraywolfVersion != "0.99.0" {
		t.Fatalf("Meta.GraywolfVersion = %q", flare.Meta.GraywolfVersion)
	}
	if flare.Meta.HostnameHash == "" && len(flare.System.Issues) == 0 {
		t.Fatal("HostnameHash empty AND no system issue")
	}
	if len(flare.Config.Items) == 0 {
		t.Fatal("no config items collected")
	}
}

func TestCollect_NoModemSkipsAudioUSBCM108(t *testing.T) {
	flare, err := Collect(Options{
		NoModem:  true,
		LogLimit: 50,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(flare.AudioDevices.Hosts) != 0 {
		t.Fatalf("AudioDevices populated despite NoModem: %+v", flare.AudioDevices)
	}
	if len(flare.USBTopology.Devices) != 0 {
		t.Fatalf("USBTopology populated despite NoModem: %+v", flare.USBTopology)
	}
	hasIssue := func(issues []flareschema.CollectorIssue) bool {
		for _, i := range issues {
			if i.Kind == "skipped_no_modem" {
				return true
			}
		}
		return false
	}
	if !hasIssue(flare.AudioDevices.Issues) ||
		!hasIssue(flare.USBTopology.Issues) ||
		!hasIssue(flare.CM108.Issues) {
		t.Fatalf("expected skipped_no_modem issue in audio/usb/cm108")
	}
}

func TestCollect_NoLogsSkipsLogSection(t *testing.T) {
	flare, _ := Collect(Options{NoLogs: true})
	if len(flare.Logs.Entries) != 0 {
		t.Fatalf("logs entries populated despite NoLogs: %d", len(flare.Logs.Entries))
	}
	hasSkip := false
	for _, i := range flare.Logs.Issues {
		if i.Kind == "skipped_no_logs" {
			hasSkip = true
		}
	}
	if !hasSkip {
		t.Fatal("expected skipped_no_logs issue")
	}
}

func TestCollect_RedactionAppliedAfter(t *testing.T) {
	flare, _ := Collect(Options{
		User: flareschema.User{
			Notes: "hit me at chris@example.com",
		},
	})
	if !strings.Contains(flare.User.Notes, "[EMAIL]") {
		t.Fatalf("user notes not scrubbed: %q", flare.User.Notes)
	}
}
