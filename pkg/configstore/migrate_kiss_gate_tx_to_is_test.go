package configstore

import (
	"path/filepath"
	"testing"
)

// TestMigrateKissGateTxToIs verifies the migration adds the gate_tx_to_is
// column with default 0, leaves existing rows alone, and is idempotent.
func TestMigrateKissGateTxToIs(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "kiss_gate.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	// Insert a row using the pre-migration shape — exclude gate_tx_to_is
	// from the INSERT so the DB default is what's tested. (Open() ran
	// every migration including ours; we simulate "legacy row" by just
	// not specifying the column.)
	if err := store.DB().Exec(
		`INSERT INTO kiss_interfaces(name, interface_type, mode, channel, broadcast, enabled,
		 tnc_ingress_rate_hz, tnc_ingress_burst, created_at, updated_at)
		 VALUES ('legacy', 'tcp', 'modem', 1, 1, 1, 50, 100, datetime('now'), datetime('now'))`,
	).Error; err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	var got bool
	if err := store.DB().Raw(
		`SELECT gate_tx_to_is FROM kiss_interfaces WHERE name='legacy'`,
	).Scan(&got).Error; err != nil {
		t.Fatalf("scan column: %v", err)
	}
	if got {
		t.Fatalf("gate_tx_to_is=%v, want false (default 0) on legacy row", got)
	}

	// Idempotence: running the migration body again must be a no-op.
	if err := migrateKissGateTxToIs(store.db); err != nil {
		t.Fatalf("second run: %v", err)
	}
}
