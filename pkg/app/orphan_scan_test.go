package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// TestBootstrapOrphanScanLogs exercises the Phase 5 orphan-scan wiring
// path in wireServices: a store with a dangling soft-FK reference
// should produce a warn-level log line per affected table with the
// stable referrer-type token and the count.
//
// We build the scan behavior outside App so we don't have to stand up
// the full wiring + modem subprocess; the assertion is that the
// logger.Warn call graywolf emits at bootstrap reaches the sink with
// the contract-shaped key/value pairs. wireServices itself is tested
// end-to-end by the smoke suite.
func TestBootstrapOrphanScanLogs(t *testing.T) {
	ctx := context.Background()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Seed a beacon + a kiss interface with dangling channel refs. We
	// use raw SQL to bypass the DTO / store-level validators since the
	// intent is to simulate a legacy DB with orphans.
	if err := store.DB().Exec(`INSERT INTO beacons (type, channel, callsign, destination, path, enabled) VALUES (?, ?, ?, ?, ?, ?)`,
		"position", 42, "N0CALL", "APGRWO", "WIDE1-1", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Exec(`INSERT INTO kiss_interfaces (name, interface_type, listen_addr, channel, enabled, mode) VALUES (?, ?, ?, ?, ?, ?)`,
		"orphan-kiss", "tcp", "0.0.0.0:1", 42, true, "modem").Error; err != nil {
		t.Fatal(err)
	}

	// Collect warn-level logs into a buffer; slog JSON handler emits
	// one NDJSON line per event.
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Replicate the exact scan + log emission wireServices does.
	orphans, err := store.CountOrphanChannelRefs(ctx)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for table, n := range orphans {
		logger.Warn("orphaned channel references at startup",
			"table", table, "orphan_count", n)
	}

	// Parse every emitted line; assert one per affected table with a
	// correct count.
	type line struct {
		Msg     string `json:"msg"`
		Table   string `json:"table"`
		Count   int    `json:"orphan_count"`
		Level   string `json:"level"`
	}
	seen := map[string]int{}
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var l line
		if err := json.Unmarshal([]byte(raw), &l); err != nil {
			t.Fatalf("parse log line %q: %v", raw, err)
		}
		if l.Msg != "orphaned channel references at startup" {
			continue
		}
		if l.Level != "WARN" {
			t.Errorf("expected WARN level, got %q", l.Level)
		}
		seen[l.Table] = l.Count
	}
	if seen[configstore.ReferrerTypeBeacon] != 1 {
		t.Errorf("beacon count in logs = %d, want 1 (seen=%+v)", seen[configstore.ReferrerTypeBeacon], seen)
	}
	if seen[configstore.ReferrerTypeKissInterface] != 1 {
		t.Errorf("kiss_interface count in logs = %d, want 1 (seen=%+v)", seen[configstore.ReferrerTypeKissInterface], seen)
	}
}
