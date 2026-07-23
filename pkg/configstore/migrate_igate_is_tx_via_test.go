package configstore

import (
	"path/filepath"
	"testing"
)

// TestMigrateIGateIsTxVia verifies the post-migration schema has is_tx_via
// (empty default) and no max_msg_hops, and that the drop step is
// idempotent and survives a re-added legacy column (issue #489).
func TestMigrateIGateIsTxVia(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "igate_via.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	// Post-migration shape: is_tx_via present, max_msg_hops gone.
	hasVia, err := columnExists(store.db, "i_gate_configs", "is_tx_via")
	if err != nil {
		t.Fatalf("probe is_tx_via: %v", err)
	}
	if !hasVia {
		t.Fatal("is_tx_via column missing after Open")
	}
	hasHops, err := columnExists(store.db, "i_gate_configs", "max_msg_hops")
	if err != nil {
		t.Fatalf("probe max_msg_hops: %v", err)
	}
	if hasHops {
		t.Fatal("max_msg_hops column should have been dropped by migration 27")
	}

	// Default: a freshly seeded singleton reads is_tx_via = '' (direct).
	var via string
	if err := store.db.Raw(`SELECT is_tx_via FROM i_gate_configs WHERE id=1`).Scan(&via).Error; err != nil {
		t.Fatalf("scan is_tx_via: %v", err)
	}
	if via != "" {
		t.Fatalf("is_tx_via = %q, want empty (direct)", via)
	}

	// Idempotence on the already-migrated schema: no-op, no error.
	if err := migrateIGateIsTxVia(store.db); err != nil {
		t.Fatalf("idempotent run on migrated schema: %v", err)
	}

	// Simulate a legacy database that still carries max_msg_hops and
	// confirm the migration drops it.
	if err := store.db.Exec(
		`ALTER TABLE i_gate_configs ADD COLUMN max_msg_hops INTEGER NOT NULL DEFAULT 2`,
	).Error; err != nil {
		t.Fatalf("re-add legacy column: %v", err)
	}
	if err := migrateIGateIsTxVia(store.db); err != nil {
		t.Fatalf("drop legacy column: %v", err)
	}
	hasHops, err = columnExists(store.db, "i_gate_configs", "max_msg_hops")
	if err != nil {
		t.Fatalf("re-probe max_msg_hops: %v", err)
	}
	if hasHops {
		t.Fatal("max_msg_hops still present after migration re-run")
	}
}
