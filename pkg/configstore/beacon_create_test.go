package configstore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestCreateBeacon_DisabledIsAtomic: when the write that keeps a beacon
// created with Enabled=false disabled fails, no beacon row may remain (an
// enabled leftover would be scheduled on the next reload).
func TestCreateBeacon_DisabledIsAtomic(t *testing.T) {
	// A file database of its own: the trigger below must not reach any other test.
	s, err := Open(filepath.Join(t.TempDir(), "graywolf.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.db.Exec(`CREATE TRIGGER reject_enabled_update BEFORE UPDATE OF enabled ON beacons
		BEGIN SELECT RAISE(ABORT, 'test: enabled update rejected'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	b := &Beacon{Type: "position", Channel: 1, Callsign: "N0CAL", Latitude: 37.5, Longitude: -122, Enabled: false}
	err = s.CreateBeacon(context.Background(), b)
	if err == nil {
		t.Fatal("CreateBeacon succeeded although the enabled update was rejected")
	}
	if !strings.Contains(err.Error(), "test: enabled update rejected") {
		t.Fatalf("CreateBeacon failed before the enabled update: %v", err)
	}
	var n int64
	if err := s.db.Model(&Beacon{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d beacon row(s) left after the failed create, want 0", n)
	}
}
