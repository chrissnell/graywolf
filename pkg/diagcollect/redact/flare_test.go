package redact

import (
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

func TestScrubFlare_MutatesEverySectionContainingStrings(t *testing.T) {
	f := flareschema.BuildSampleFlare()
	// Inject testable PII across every section.
	f.User.Email = "user@example.com"
	f.User.Notes = "rosie-pi acting up at 192.168.1.5"
	f.Config.Items = append(f.Config.Items, flareschema.ConfigItem{
		Key: "free.text", Value: "see chris@example.com",
	})
	f.System.OSPretty = "Debian on rosie-pi"
	f.PTT.Candidates[0].Description = "rosie-pi USB adapter"
	f.Logs.Entries[0].Msg = "ping 8.8.8.8 ok from rosie-pi"
	f.Logs.Entries[0].Attrs = map[string]any{
		"peer": "10.0.0.5",
		"user": "/home/cjs",
	}

	e := NewEngine()
	e.SetHostname("rosie-pi")
	ScrubFlare(&f, e)

	// User
	if !strings.Contains(f.User.Email, "[EMAIL]") {
		t.Fatalf("User.Email not scrubbed: %q", f.User.Email)
	}
	if strings.Contains(f.User.Notes, "rosie-pi") {
		t.Fatalf("User.Notes hostname survived: %q", f.User.Notes)
	}
	// Config
	last := f.Config.Items[len(f.Config.Items)-1]
	if !strings.Contains(last.Value, "[EMAIL]") {
		t.Fatalf("Config last item value: %q", last.Value)
	}
	// System
	if strings.Contains(f.System.OSPretty, "rosie-pi") {
		t.Fatalf("System.OSPretty hostname survived: %q", f.System.OSPretty)
	}
	// PTT candidate
	if strings.Contains(f.PTT.Candidates[0].Description, "rosie-pi") {
		t.Fatalf("PTT cand desc hostname survived: %q", f.PTT.Candidates[0].Description)
	}
	// Logs entries
	if strings.Contains(f.Logs.Entries[0].Msg, "8.8.8.8") {
		t.Fatalf("Logs.Msg public IP survived: %q", f.Logs.Entries[0].Msg)
	}
	if strings.Contains(f.Logs.Entries[0].Msg, "rosie-pi") {
		t.Fatalf("Logs.Msg hostname survived: %q", f.Logs.Entries[0].Msg)
	}
	// Logs attrs
	if v, ok := f.Logs.Entries[0].Attrs["peer"].(string); !ok || strings.Contains(v, "10.0.0.5") {
		t.Fatalf("Logs.Attrs.peer not scrubbed: %v", f.Logs.Entries[0].Attrs["peer"])
	}
	if v, ok := f.Logs.Entries[0].Attrs["user"].(string); !ok || strings.Contains(v, "/home/cjs") {
		t.Fatalf("Logs.Attrs.user not scrubbed: %v", f.Logs.Entries[0].Attrs["user"])
	}
}

func TestScrubFlare_PreservesSectionShape(t *testing.T) {
	f := flareschema.BuildSampleFlare()
	originalConfig := len(f.Config.Items)
	originalLogs := len(f.Logs.Entries)
	originalPTT := len(f.PTT.Candidates)

	e := NewEngine()
	e.SetHostname("rosie-pi")
	ScrubFlare(&f, e)

	if len(f.Config.Items) != originalConfig {
		t.Fatalf("config items count mutated: %d -> %d", originalConfig, len(f.Config.Items))
	}
	if len(f.Logs.Entries) != originalLogs {
		t.Fatalf("log entries count mutated: %d -> %d", originalLogs, len(f.Logs.Entries))
	}
	if len(f.PTT.Candidates) != originalPTT {
		t.Fatalf("ptt candidates count mutated: %d -> %d", originalPTT, len(f.PTT.Candidates))
	}
}

func TestScrubFlare_NilSafe(t *testing.T) {
	// ScrubFlare on a freshly-zeroed Flare must not panic. Defensive
	// because the orchestrator may abort collection partway through
	// and call ScrubFlare on a sparse value.
	var f flareschema.Flare
	e := NewEngine()
	ScrubFlare(&f, e)
}
