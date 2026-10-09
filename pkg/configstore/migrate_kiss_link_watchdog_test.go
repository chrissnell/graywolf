package configstore

import (
	"path/filepath"
	"testing"
)

// TestMigrateKissLinkWatchdog verifies legacy rows pick up the defaults
// ('auto', 0) and the migration is idempotent.
func TestMigrateKissLinkWatchdog(t *testing.T) {
	t.Parallel()
	store, err := Open(filepath.Join(t.TempDir(), "kiss_lw.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	if err := store.DB().Exec(
		`INSERT INTO kiss_interfaces(name, interface_type, mode, channel, broadcast, enabled,
		 tnc_ingress_rate_hz, tnc_ingress_burst, created_at, updated_at)
		 VALUES ('legacy', 'serial', 'tnc', 1, 1, 1, 50, 100, datetime('now'), datetime('now'))`,
	).Error; err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	var row struct {
		LinkWatchdog     string
		IdleReconnectSec uint32
	}
	if err := store.DB().Raw(
		`SELECT link_watchdog, idle_reconnect_sec FROM kiss_interfaces WHERE name='legacy'`,
	).Scan(&row).Error; err != nil {
		t.Fatalf("scan columns: %v", err)
	}
	if row.LinkWatchdog != "auto" || row.IdleReconnectSec != 0 {
		t.Fatalf("legacy defaults = %+v, want {auto 0}", row)
	}

	if err := migrateKissLinkWatchdog(store.db); err != nil {
		t.Fatalf("second run: %v", err)
	}
}
