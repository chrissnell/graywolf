package configstore

import (
	"fmt"
	"sort"

	"gorm.io/gorm"
)

// migrationPhase distinguishes migrations that must run before
// AutoMigrate (e.g. renames of columns AutoMigrate can't recognize) from
// migrations that touch data after the schema is current.
type migrationPhase int

const (
	preAutoMigrate migrationPhase = iota
	postAutoMigrate
)

// migration is one atomic schema or data change, identified by a
// monotonic version number that matches PRAGMA user_version after it
// runs. Versions are assigned in append-only order and never reused,
// so a database that has seen migration N has also seen all migrations
// < N. Each migration runs inside a single transaction so a crash can
// never leave user_version out of sync with the data.
type migration struct {
	version int
	name    string
	phase   migrationPhase
	run     func(tx *gorm.DB) error
}

// schemaMigrations is the append-only list of schema/data migrations
// applied to a graywolf configstore database. Never renumber, reorder,
// or delete entries — only append. The slice order below is purely
// documentary; runMigrations sorts by version before executing. The
// ordering documented here is the authoritative history of the PRAGMA
// user_version number:
//
//	1 — beacon_compress_default: force compress=1 on legacy beacon
//	    rows that pre-date the encoder actually honoring the column.
//	2 — channel_device_fields: copy the legacy audio_device_id/
//	    audio_channel columns into the input_device_id/input_channel +
//	    output_device_id/output_channel split and drop the old columns.
//	    Runs in the pre-AutoMigrate phase so AutoMigrate sees the new
//	    column names on a schema that matches the Go model instead of
//	    trying to add them on top of the old ones.
//	3 — drop_channel_tx_timing: remove vestigial tx_delay_ms and
//	    tx_tail_ms columns from channels table; these values now live
//	    exclusively in the tx_timings table.
var schemaMigrations = []migration{
	{version: 1, name: "beacon_compress_default", phase: postAutoMigrate, run: migrateBeaconCompressDefault},
	{version: 2, name: "channel_device_fields", phase: preAutoMigrate, run: migrateChannelDeviceFields},
	{version: 3, name: "drop_channel_tx_timing", phase: preAutoMigrate, run: migrateDropChannelTxTiming},
}

// runMigrations applies every pending migration in the given phase,
// bumping PRAGMA user_version after each success. It is safe to call
// repeatedly: migrations whose version is already <= user_version are
// skipped. Migrations in one phase run in ascending version order.
func (s *Store) runMigrations(phase migrationPhase) error {
	var current int
	if err := s.db.Raw("PRAGMA user_version").Scan(&current).Error; err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	var phaseMigrations []migration
	for _, m := range schemaMigrations {
		if m.phase == phase {
			phaseMigrations = append(phaseMigrations, m)
		}
	}
	sort.Slice(phaseMigrations, func(i, j int) bool {
		return phaseMigrations[i].version < phaseMigrations[j].version
	})

	for _, m := range phaseMigrations {
		if current >= m.version {
			continue
		}
		err := s.db.Transaction(func(tx *gorm.DB) error {
			if err := m.run(tx); err != nil {
				return err
			}
			return tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", m.version)).Error
		})
		if err != nil {
			return fmt.Errorf("migration %d (%s): %w", m.version, m.name, err)
		}
		current = m.version
	}
	return nil
}

// migrateBeaconCompressDefault flips every beacon row to compress=1.
// Earlier versions defaulted the column to false but never wired it to
// the encoder, so any stored 0 is a legacy artifact, not an operator
// choice. Runs exactly once per database.
func migrateBeaconCompressDefault(tx *gorm.DB) error {
	return tx.Exec("UPDATE beacons SET compress = 1 WHERE compress = 0").Error
}

// migrateChannelDeviceFields reshapes the legacy single audio_device_id/
// audio_channel pair into the new input_device_id/input_channel/
// output_device_id/output_channel split.
//
// It runs in the pre-AutoMigrate phase because GORM's AutoMigrate would
// otherwise try to ALTER TABLE ADD a NOT NULL column with no DEFAULT,
// which SQLite rejects on a non-empty table. By adding the new columns
// ourselves (with explicit defaults) and dropping the old ones here,
// the channels table already has the new shape by the time AutoMigrate
// runs — AutoMigrate then sees matching columns and leaves them alone.
//
// No-op on a fresh database where the old columns never existed, or
// on a database that an older binary already migrated before the
// user_version gate was introduced.
func migrateChannelDeviceFields(tx *gorm.DB) error {
	var legacyCount int
	if err := tx.Raw("SELECT COUNT(*) FROM pragma_table_info('channels') WHERE name='audio_device_id'").Scan(&legacyCount).Error; err != nil {
		return fmt.Errorf("probe legacy columns: %w", err)
	}
	if legacyCount == 0 {
		return nil
	}

	stmts := []string{
		"ALTER TABLE channels ADD COLUMN input_device_id INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE channels ADD COLUMN input_channel INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE channels ADD COLUMN output_device_id INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE channels ADD COLUMN output_channel INTEGER NOT NULL DEFAULT 0",
		// Copy every row unconditionally. The previous guard
		// (WHERE input_device_id = 0) would silently skip a row that a
		// partially-applied migration had already touched; with a
		// proper user_version gate we only reach this code on a DB
		// that has never seen this migration, so "copy everything" is
		// the honest thing to do.
		"UPDATE channels SET input_device_id = audio_device_id, input_channel = audio_channel",
		"ALTER TABLE channels DROP COLUMN audio_device_id",
		"ALTER TABLE channels DROP COLUMN audio_channel",
	}
	for _, stmt := range stmts {
		if err := tx.Exec(stmt).Error; err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	return nil
}

// migrateDropChannelTxTiming removes the vestigial tx_delay_ms and
// tx_tail_ms columns from the channels table. These values now live
// exclusively in the tx_timings table; the Channel model no longer
// carries them. Runs pre-AutoMigrate so the table shape matches the
// Go struct before GORM inspects it.
func migrateDropChannelTxTiming(tx *gorm.DB) error {
	for _, col := range []string{"tx_delay_ms", "tx_tail_ms"} {
		var count int
		if err := tx.Raw("SELECT COUNT(*) FROM pragma_table_info('channels') WHERE name=?", col).Scan(&count).Error; err != nil {
			return fmt.Errorf("probe %s: %w", col, err)
		}
		if count == 0 {
			continue
		}
		if err := tx.Exec("ALTER TABLE channels DROP COLUMN " + col).Error; err != nil {
			return fmt.Errorf("drop %s: %w", col, err)
		}
	}
	return nil
}
