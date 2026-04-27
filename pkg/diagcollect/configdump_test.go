package diagcollect

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// helper: open a fresh in-memory-ish configstore against a tempfile.
func openSeededStore(t *testing.T) *configstore.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := configstore.Open(filepath.Join(dir, "graywolf.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCollectConfig_DumpsKnownTypes(t *testing.T) {
	s := openSeededStore(t)
	// Seed at least one row in a singleton so the dump has known keys to
	// assert against.
	if err := s.UpsertStationConfig(context.Background(), configstore.StationConfig{
		Callsign: "N0CALL",
	}); err != nil {
		t.Fatalf("UpsertStationConfig: %v", err)
	}

	got := CollectConfig(context.Background(), s)
	if len(got.Items) == 0 {
		t.Fatal("expected at least one dotted-key item")
	}
	// Station callsign should be present and NOT redacted (APRS callsign).
	found := false
	for _, it := range got.Items {
		if strings.Contains(it.Key, "station") && strings.Contains(strings.ToLower(it.Key), "callsign") {
			found = true
			if it.Value != "N0CALL" {
				t.Fatalf("callsign value %q, want N0CALL", it.Value)
			}
		}
	}
	if !found {
		t.Fatalf("station.*.callsign not found in dump:\nfirst items: %+v", got.Items[:min(len(got.Items), 8)])
	}
}

func TestCollectConfig_DropsAPRSPasscodeKey(t *testing.T) {
	// StationConfig has no Passcode field (removed; column retained in DB
	// for forward-safety only). Verify that if any key with "passcode" in
	// its name were ever emitted, shouldDropKey would catch it. The live
	// dump from a seeded store should produce zero passcode keys.
	s := openSeededStore(t)
	if err := s.UpsertStationConfig(context.Background(), configstore.StationConfig{
		Callsign: "N0CALL",
	}); err != nil {
		t.Fatalf("UpsertStationConfig: %v", err)
	}
	got := CollectConfig(context.Background(), s)
	for _, it := range got.Items {
		if strings.Contains(strings.ToLower(it.Key), "passcode") {
			t.Fatalf("passcode key not dropped: %+v", it)
		}
	}
	// Also verify shouldDropKey directly for the canonical APRS passcode path.
	if !shouldDropKey("station.passcode") {
		t.Fatal("shouldDropKey(station.passcode) = false, want true")
	}
	if !shouldDropKey("igate.passcode") {
		t.Fatal("shouldDropKey(igate.passcode) = false, want true")
	}
}

func TestCollectConfig_RedactsSecretLikeKeys(t *testing.T) {
	cases := []struct {
		key, value, wantValue string
	}{
		{"maps.api_key", "real-key-value", "[REDACTED]"},
		{"updates.feed_token", "tk_abcd", "[REDACTED]"},
		{"webauth.session_secret", "s3cr3t", "[REDACTED]"},
		{"aprs.password", "literal", "[REDACTED]"},
		{"theme.preferred_color", "blue", "blue"}, // negative
		{"aprs.callsign", "N0CALL", "N0CALL"},     // negative
	}
	for _, c := range cases {
		got := scrubKeyValue(c.key, c.value)
		if got != c.wantValue {
			t.Fatalf("scrubKeyValue(%q, %q) = %q, want %q", c.key, c.value, got, c.wantValue)
		}
	}
}

func TestCollectConfig_StoreNilEmitsIssue(t *testing.T) {
	got := CollectConfig(context.Background(), nil)
	if len(got.Items) != 0 {
		t.Fatalf("expected empty items, got %d", len(got.Items))
	}
	if len(got.Issues) != 1 || got.Issues[0].Kind != "config_db_unavailable" {
		t.Fatalf("expected one config_db_unavailable issue, got %+v", got.Issues)
	}
}

func TestCollectConfig_OrderStable(t *testing.T) {
	s := openSeededStore(t)
	a := CollectConfig(context.Background(), s).Items
	b := CollectConfig(context.Background(), s).Items
	if len(a) != len(b) {
		t.Fatalf("len differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("order differs at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
