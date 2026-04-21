package configstore

import (
	"fmt"
	"sort"
	"strings"

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
// < N.
//
// Most migrations run inside a single auto-managed transaction (the
// default) so a crash can never leave user_version out of sync with
// the data. A small minority need to run connection-scoped PRAGMAs
// that SQLite ignores inside an open transaction — principally
// `PRAGMA foreign_keys = OFF` around a 12-step table rebuild. Those
// migrations set selfTxn = true; runMigrations then invokes run
// directly on s.db without a wrapping Transaction(), leaving the
// migration to open + commit its own BEGIN/COMMIT pair and update
// user_version before that COMMIT so the success gate is still
// atomic.
type migration struct {
	version  int
	name     string
	phase    migrationPhase
	selfTxn  bool
	run      func(tx *gorm.DB) error
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
//	4 — messages_partial_retry_index: partial index that backs the
//	    retry-due scan over outstanding DM outbound messages. GORM
//	    AutoMigrate cannot express a partial index, so this runs as
//	    raw SQL post-AutoMigrate.
//	5 — messages_thread_rollup_index: composite index used by the
//	    conversation-rollup query. Plain AutoMigrate would create this
//	    from the struct tags, but the migration-list entry lets us
//	    evolve the index shape independently and keeps the intent
//	    centralized with the partial index above.
//	6 — messages_retry_max_attempts_default: rewrite any retry_max_attempts
//	    row still carrying the old seeded default (5) to the new default
//	    (4). Channel-friendlier APRS etiquette — 1 initial + 3 retries is
//	    closer to mainstream client norms than the original 5 attempts.
//	    Only touches rows equal to the old default so operators who
//	    explicitly chose another value keep it.
//	7 — messages_kind_backfill: backfill legacy messages.kind rows to
//	    'text'. AutoMigrate adds the column with a DEFAULT clause, but
//	    SQLite ALTER TABLE ADD COLUMN DEFAULT only applies to rows
//	    inserted AFTER the ALTER — legacy rows end up with NULL. This
//	    migration rewrites them to 'text' explicitly so every downstream
//	    reader can trust the column.
//	8 — channels_nullable_input_device: relax channels.input_device_id
//	    from NOT NULL to NULL so a KISS-TNC-only channel can exist
//	    without an audio modem. SQLite cannot drop a NOT NULL constraint
//	    with ALTER TABLE, so this is a full 12-step table rebuild. Runs
//	    in the pre-AutoMigrate phase because AutoMigrate cannot detect
//	    a nullability change; by the time AutoMigrate inspects the
//	    table, it must already match the Go struct shape. See
//	    .context/2026-04-20-kiss-tcp-client-and-channel-backing.md D6/D10.
//	9 — kiss_interfaces_tx_flags: add allow_tx_from_governor and
//	    needs_reconfig columns to kiss_interfaces. Plain ALTER TABLE
//	    ADD COLUMN migrations — default 0 for existing rows so the
//	    Phase 3 TX dispatcher does not silently enable the governor→KISS
//	    TX path on interfaces that users configured before the feature
//	    existed. Runs in the pre-AutoMigrate phase because SQLite's
//	    ALTER TABLE ADD COLUMN can only install the default on
//	    newly-inserted rows — doing this before AutoMigrate lets us
//	    spell the default explicitly, and a later backfill would
//	    overwrite any operator edits made between the two migrations.
//	    See design decision D4 in
//	    .context/2026-04-20-kiss-tcp-client-and-channel-backing.md.
//	10 — kiss_interfaces_tcp_client_fields: add remote_host, remote_port,
//	    reconnect_init_ms, reconnect_max_ms to kiss_interfaces. Plain
//	    ALTER TABLE ADD COLUMN migrations with explicit defaults
//	    (''/0/1000/300000) so existing non-tcp-client rows remain
//	    harmless: only rows with interface_type='tcp-client' consult
//	    these columns (Phase 4 of the KISS TCP-client plan).
var schemaMigrations = []migration{
	{version: 1, name: "beacon_compress_default", phase: postAutoMigrate, run: migrateBeaconCompressDefault},
	{version: 2, name: "channel_device_fields", phase: preAutoMigrate, run: migrateChannelDeviceFields},
	{version: 3, name: "drop_channel_tx_timing", phase: preAutoMigrate, run: migrateDropChannelTxTiming},
	{version: 4, name: "messages_partial_retry_index", phase: postAutoMigrate, run: migrateMessagesPartialRetryIndex},
	{version: 5, name: "messages_thread_rollup_index", phase: postAutoMigrate, run: migrateMessagesThreadRollupIndex},
	{version: 6, name: "messages_retry_max_attempts_default", phase: postAutoMigrate, run: migrateMessagesRetryMaxAttemptsDefault},
	{version: 7, name: "messages_kind_backfill", phase: postAutoMigrate, run: migrateMessagesKindBackfill},
	{version: 8, name: "channels_nullable_input_device", phase: preAutoMigrate, selfTxn: true, run: migrateChannelsNullableInputDevice},
	{version: 9, name: "kiss_interfaces_tx_flags", phase: preAutoMigrate, run: migrateKissInterfacesTxFlags},
	{version: 10, name: "kiss_interfaces_tcp_client_fields", phase: preAutoMigrate, run: migrateKissInterfacesTcpClientFields},
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
		var err error
		if m.selfTxn {
			// Self-managed transactions: the migration body is
			// responsible for BEGIN/COMMIT and for bumping
			// user_version inside that same transaction. The
			// wrapper only runs the body and checks the error.
			err = m.run(s.db)
		} else {
			err = s.db.Transaction(func(tx *gorm.DB) error {
				if err := m.run(tx); err != nil {
					return err
				}
				return tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", m.version)).Error
			})
		}
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

// migrateMessagesPartialRetryIndex creates the partial index that backs
// the retry-due scan in pkg/messages. A partial index on the tuple used
// by the scan keeps the index tiny (tens of rows at most — only
// outstanding DM outbound messages) and lets the planner skip the
// WHERE-clause filtering entirely. GORM AutoMigrate cannot express a
// partial index, so we issue the raw SQL here. `CREATE INDEX IF NOT
// EXISTS` is idempotent across upgrade and fresh-install paths.
func migrateMessagesPartialRetryIndex(tx *gorm.DB) error {
	return tx.Exec(
		`CREATE INDEX IF NOT EXISTS idx_msg_retry ON messages(next_retry_at) ` +
			`WHERE ack_state='none' AND direction='out' AND thread_kind='dm' AND deleted_at IS NULL`,
	).Error
}

// migrateMessagesThreadRollupIndex creates the composite index used by
// the conversation-rollup query. The index covers the GROUP BY
// (thread_kind, thread_key) with created_at as the trailing key so the
// "last message" scan is an index-range seek per group rather than a
// full sort.
func migrateMessagesThreadRollupIndex(tx *gorm.DB) error {
	return tx.Exec(
		`CREATE INDEX IF NOT EXISTS idx_msg_thread ON messages(thread_kind, thread_key, created_at)`,
	).Error
}

// migrateMessagesRetryMaxAttemptsDefault lowers rows still carrying
// the original seeded default (5) to the new default (4). Scoped to
// the old value so anything the operator explicitly set (3, 7, etc.)
// survives untouched. Idempotent.
func migrateMessagesRetryMaxAttemptsDefault(tx *gorm.DB) error {
	return tx.Exec(
		`UPDATE message_preferences SET retry_max_attempts = 4 WHERE retry_max_attempts = 5`,
	).Error
}

// migrateMessagesKindBackfill rewrites every legacy messages.kind to
// 'text'. AutoMigrate adds the column with DEFAULT 'text' NOT NULL,
// but SQLite's ALTER TABLE ADD COLUMN ... DEFAULT only applies the
// default to newly-inserted rows; existing rows get NULL (or an empty
// string when SQLite reinterprets the NOT NULL constraint silently),
// and relying on the GORM tag to paper over that at read time is
// exactly the "defensive render-time fallback" the plan forbids.
//
// UPDATE … WHERE kind IS NULL OR kind = '' is idempotent: once the
// column is fully populated, the statement is a no-op.
func migrateMessagesKindBackfill(tx *gorm.DB) error {
	return tx.Exec(
		`UPDATE messages SET kind = 'text' WHERE kind IS NULL OR kind = ''`,
	).Error
}

// migrateKissInterfacesTxFlags adds two nullable-default-false BOOL
// columns to kiss_interfaces:
//
//   - allow_tx_from_governor: when true and Mode == tnc, the Phase 3 TX
//     dispatcher registers this interface as a KissTnc backend. Default
//     false on existing rows so deployments that pre-date Phase 3 do not
//     silently start transmitting to their TNC-mode interfaces.
//   - needs_reconfig: Phase 5 referential-cascade flag. Declared here so
//     the column exists before Phase 5 lands.
//
// Fresh-database path: the kiss_interfaces table doesn't exist yet
// (AutoMigrate creates it after this preAutoMigrate pass), so we just
// bump user_version and return. AutoMigrate will create the table with
// both columns directly from the Go struct.
func migrateKissInterfacesTxFlags(tx *gorm.DB) error {
	var tableExists int
	if err := tx.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='kiss_interfaces'").Scan(&tableExists).Error; err != nil {
		return fmt.Errorf("probe kiss_interfaces: %w", err)
	}
	if tableExists == 0 {
		return nil
	}

	for _, col := range []struct {
		name, sql string
	}{
		{"allow_tx_from_governor", "ALTER TABLE kiss_interfaces ADD COLUMN allow_tx_from_governor NUMERIC NOT NULL DEFAULT 0"},
		{"needs_reconfig", "ALTER TABLE kiss_interfaces ADD COLUMN needs_reconfig NUMERIC NOT NULL DEFAULT 0"},
	} {
		var present int
		if err := tx.Raw("SELECT COUNT(*) FROM pragma_table_info('kiss_interfaces') WHERE name=?", col.name).Scan(&present).Error; err != nil {
			return fmt.Errorf("probe %s: %w", col.name, err)
		}
		if present > 0 {
			continue
		}
		if err := tx.Exec(col.sql).Error; err != nil {
			return fmt.Errorf("add %s: %w", col.name, err)
		}
	}
	return nil
}

// migrateKissInterfacesTcpClientFields adds the outbound-dial columns
// used by InterfaceType == "tcp-client" (Phase 4 of the KISS TCP-client
// plan). All four columns are NOT NULL with explicit defaults so
// existing rows end up with documented, harmless values:
//
//   - remote_host TEXT NOT NULL DEFAULT ''
//   - remote_port INTEGER NOT NULL DEFAULT 0
//   - reconnect_init_ms INTEGER NOT NULL DEFAULT 1000
//   - reconnect_max_ms INTEGER NOT NULL DEFAULT 300000
//
// Server-listen (interface_type="tcp"), serial, and bluetooth rows
// ignore these columns; only tcp-client rows consult them. The DTO
// validator enforces RemotePort > 0 and RemoteHost != "" for
// tcp-client, so a legacy row accidentally getting interface_type
// flipped to tcp-client without these values would fail the 400 gate
// rather than try to dial an empty string.
//
// Fresh-database path: the kiss_interfaces table doesn't exist yet
// (AutoMigrate creates it after this preAutoMigrate pass), so we just
// return. AutoMigrate will create the table with all columns directly
// from the Go struct.
func migrateKissInterfacesTcpClientFields(tx *gorm.DB) error {
	var tableExists int
	if err := tx.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='kiss_interfaces'").Scan(&tableExists).Error; err != nil {
		return fmt.Errorf("probe kiss_interfaces: %w", err)
	}
	if tableExists == 0 {
		return nil
	}

	for _, col := range []struct {
		name, sql string
	}{
		{"remote_host", "ALTER TABLE kiss_interfaces ADD COLUMN remote_host TEXT NOT NULL DEFAULT ''"},
		{"remote_port", "ALTER TABLE kiss_interfaces ADD COLUMN remote_port INTEGER NOT NULL DEFAULT 0"},
		{"reconnect_init_ms", "ALTER TABLE kiss_interfaces ADD COLUMN reconnect_init_ms INTEGER NOT NULL DEFAULT 1000"},
		{"reconnect_max_ms", "ALTER TABLE kiss_interfaces ADD COLUMN reconnect_max_ms INTEGER NOT NULL DEFAULT 300000"},
	} {
		var present int
		if err := tx.Raw("SELECT COUNT(*) FROM pragma_table_info('kiss_interfaces') WHERE name=?", col.name).Scan(&present).Error; err != nil {
			return fmt.Errorf("probe %s: %w", col.name, err)
		}
		if present > 0 {
			continue
		}
		if err := tx.Exec(col.sql).Error; err != nil {
			return fmt.Errorf("add %s: %w", col.name, err)
		}
	}
	return nil
}

// migrateChannelsNullableInputDevice relaxes channels.input_device_id
// from NOT NULL to NULL via the SQLite 12-step table-rebuild pattern.
// This is required because SQLite's ALTER TABLE cannot drop a NOT NULL
// constraint (nor modify FK declarations) — the column has to be
// recreated in a fresh table, rows copied over, old table dropped, new
// table renamed into place, and indexes recreated. See
// https://www.sqlite.org/lang_altertable.html#otheralter and design
// decision D10 in .context/2026-04-20-kiss-tcp-client-and-channel-backing.md.
//
// This migration is registered with selfTxn=true because the 12-step
// recipe requires `PRAGMA foreign_keys = OFF` to take effect at
// connection scope, and SQLite silently ignores that pragma while any
// transaction is open. The body opens its own BEGIN / COMMIT pair and
// bumps user_version inside that same transaction so the success gate
// stays atomic.
//
// Down-migration: migrateChannelsNullableInputDeviceDown reverses the
// shape. It aborts when any existing row holds NULL in input_device_id
// (the NOT NULL re-add would lose data), forcing the operator to
// either delete the KISS-only channels or fix them up before rolling
// back. This matches the plan's "rollback is symmetric but lossy"
// contract.
func migrateChannelsNullableInputDevice(db *gorm.DB) error {
	// Fresh-database no-op: the channels table doesn't exist yet.
	// AutoMigrate (which runs *after* this preAutoMigrate pass) will
	// create the table directly from the Go struct, which already
	// declares InputDeviceID as *uint32 (nullable). There is no
	// legacy shape to rewrite, so we just bump user_version and
	// return. user_version gets set inside the transaction below on
	// the upgrade path; on the fresh-DB path we set it directly.
	var exists int
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='channels'").Scan(&exists).Error; err != nil {
		return fmt.Errorf("probe channels table: %w", err)
	}
	if exists == 0 {
		return db.Exec("PRAGMA user_version = 8").Error
	}

	// Column copy list is computed dynamically from the live
	// channels table — migrations 2 and 3 already reshape the table
	// before us, but future ADD COLUMN migrations could land before
	// someone upgrades past version 8 on a database that predates
	// them, and a hard-coded list would silently drop data. Using
	// pragma_table_info lets us copy every column we find, whatever
	// the user_version chain happened to land on.
	cols, err := channelsTableColumns(db, "channels")
	if err != nil {
		return fmt.Errorf("read channels columns: %w", err)
	}
	// The input_device_id column must exist; otherwise an earlier
	// migration never ran. Fail loudly rather than rebuilding a
	// table without the column of interest.
	if !containsString(cols, "input_device_id") {
		return fmt.Errorf("channels.input_device_id column missing; migrations 2+ must have run first")
	}
	colList := joinColumns(cols)

	// Open our own transaction. We disable FK enforcement first
	// (connection-scoped; must come before BEGIN) so the DROP
	// channels + RENAME channels_new channels round-trip doesn't
	// violate the PttConfig FK mid-flight.
	if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		return fmt.Errorf("disable foreign keys: %w", err)
	}

	// Re-enable on any exit path. The deferred exec ignores errors
	// because we're already unwinding; the real health check is
	// PRAGMA foreign_key_check inside the transaction.
	defer func() { _ = db.Exec("PRAGMA foreign_keys = ON").Error }()

	if err := db.Exec("BEGIN").Error; err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// On any error after BEGIN, rollback.
	commit := false
	defer func() {
		if !commit {
			_ = db.Exec("ROLLBACK").Error
		}
	}()

	// 1. Create channels_new with input_device_id nullable. The FK
	//    shape matches the post-Phase-1 channels schema: FK to
	//    audio_devices(id) with ON DELETE RESTRICT / ON UPDATE
	//    RESTRICT, but now nullable. All other columns copy their
	//    NOT NULL / DEFAULT settings from the Go struct. Columns
	//    added by future migrations will be re-added by AutoMigrate
	//    after this migration completes — the column-copy list
	//    carries existing data forward; shape divergence on future
	//    columns is reconciled by GORM.
	createSQL := `CREATE TABLE channels_new (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		input_device_id INTEGER NULL,
		input_channel INTEGER NOT NULL DEFAULT 0,
		output_device_id INTEGER NOT NULL DEFAULT 0,
		output_channel INTEGER NOT NULL DEFAULT 0,
		modem_type TEXT NOT NULL DEFAULT 'afsk',
		bit_rate INTEGER NOT NULL DEFAULT 1200,
		mark_freq INTEGER NOT NULL DEFAULT 1200,
		space_freq INTEGER NOT NULL DEFAULT 2200,
		profile TEXT NOT NULL DEFAULT 'A',
		num_slicers INTEGER NOT NULL DEFAULT 1,
		fix_bits TEXT NOT NULL DEFAULT 'none',
		fx25_encode NUMERIC NOT NULL DEFAULT 0,
		il2p_encode NUMERIC NOT NULL DEFAULT 0,
		num_decoders INTEGER NOT NULL DEFAULT 1,
		decoder_offset INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME,
		updated_at DATETIME,
		CONSTRAINT fk_channels_input_device
			FOREIGN KEY (input_device_id) REFERENCES audio_devices(id)
			ON DELETE RESTRICT ON UPDATE RESTRICT
	)`
	if err := db.Exec(createSQL).Error; err != nil {
		return fmt.Errorf("create channels_new: %w", err)
	}

	// 2. Copy every row. Use the dynamic column list so any column
	//    a prior migration added survives; AutoMigrate will reconcile
	//    the struct-vs-table shape on the post-AutoMigrate pass.
	//    INSERT only covers columns that actually exist in
	//    channels_new; unknown columns (ones added by pre-AutoMigrate
	//    logic we haven't seen) would trigger a "no such column"
	//    error, so we intersect.
	newCols, err := channelsTableColumns(db, "channels_new")
	if err != nil {
		return fmt.Errorf("read channels_new columns: %w", err)
	}
	shared := intersectStrings(cols, newCols)
	sharedList := joinColumns(shared)
	copySQL := fmt.Sprintf(`INSERT INTO channels_new (%s) SELECT %s FROM channels`,
		sharedList, sharedList)
	if err := db.Exec(copySQL).Error; err != nil {
		return fmt.Errorf("copy rows: %w", err)
	}
	_ = colList // retained for potential diagnostic logging

	// 3. Drop the old table.
	if err := db.Exec("DROP TABLE channels").Error; err != nil {
		return fmt.Errorf("drop channels: %w", err)
	}

	// 4. Rename the new table into place.
	if err := db.Exec("ALTER TABLE channels_new RENAME TO channels").Error; err != nil {
		return fmt.Errorf("rename channels_new: %w", err)
	}

	// 5. Recreate the index that the original channels table carried
	//    on input_device_id. The model declares index on
	//    input_device_id + output_device_id separately; AutoMigrate
	//    will recreate GORM-managed indexes next pass, but we seed
	//    the input_device_id index now so lookups stay fast between
	//    this migration's commit and AutoMigrate's index check.
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_channels_input_device_id ON channels(input_device_id)").Error; err != nil {
		return fmt.Errorf("create input_device_id index: %w", err)
	}
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_channels_output_device_id ON channels(output_device_id)").Error; err != nil {
		return fmt.Errorf("create output_device_id index: %w", err)
	}

	// 6. Verify FK health before committing. foreign_key_check is a
	//    read-only PRAGMA that returns rows for each violation;
	//    empty result set means the rebuild preserved referential
	//    integrity.
	var violations []struct {
		Table  string `gorm:"column:table"`
		Rowid  int64  `gorm:"column:rowid"`
		Parent string `gorm:"column:parent"`
		Fkid   int64  `gorm:"column:fkid"`
	}
	if err := db.Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil {
		return fmt.Errorf("foreign_key_check: %w", err)
	}
	if len(violations) > 0 {
		return fmt.Errorf("foreign_key_check failed after rebuild: %+v", violations)
	}

	// 7. Bump user_version inside the same transaction so the
	//    success gate is still atomic: if the COMMIT fails, the
	//    user_version rollback takes the schema change with it.
	if err := db.Exec("PRAGMA user_version = 8").Error; err != nil {
		return fmt.Errorf("bump user_version: %w", err)
	}

	if err := db.Exec("COMMIT").Error; err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	commit = true
	return nil
}

// channelsTableColumns returns the column names of a given table, in
// cid order (the order they appear in the CREATE TABLE statement). Used
// by migrateChannelsNullableInputDevice to compute dynamic copy lists.
func channelsTableColumns(db *gorm.DB, table string) ([]string, error) {
	var names []string
	rows, err := db.Raw(fmt.Sprintf("SELECT name FROM pragma_table_info(%q) ORDER BY cid", table)).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func containsString(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}

func intersectStrings(a, b []string) []string {
	seen := make(map[string]bool, len(b))
	for _, s := range b {
		seen[s] = true
	}
	out := make([]string, 0, len(a))
	for _, s := range a {
		if seen[s] {
			out = append(out, s)
		}
	}
	return out
}

// joinColumns quotes each column name and joins with commas. We quote
// with double quotes (standard SQL identifier delimiters) so a future
// column whose name happens to collide with a SQL keyword stays safe.
func joinColumns(cols []string) string {
	var b strings.Builder
	for i, c := range cols {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(`"`)
		b.WriteString(c)
		b.WriteString(`"`)
	}
	return b.String()
}

// migrateChannelsNullableInputDeviceDown reverses migration 8 by
// rebuilding channels with input_device_id NOT NULL again. Aborts with
// an error when any existing row holds a NULL value — re-adding a
// NOT NULL constraint would either require inventing a default (bad
// for referential integrity) or silently dropping rows. Callers that
// want to roll back must first either delete KISS-only channels or
// assign them a valid input device. Not wired into the forward
// migration chain; used by TestMigrateFromPriorRelease and operators
// rolling back manually.
func migrateChannelsNullableInputDeviceDown(db *gorm.DB) error {
	var nullCount int
	if err := db.Raw("SELECT COUNT(*) FROM channels WHERE input_device_id IS NULL").Scan(&nullCount).Error; err != nil {
		return fmt.Errorf("scan null input_device_id rows: %w", err)
	}
	if nullCount > 0 {
		return fmt.Errorf("down-migration aborted: %d row(s) have NULL input_device_id; re-adding NOT NULL would lose data", nullCount)
	}
	cols, err := channelsTableColumns(db, "channels")
	if err != nil {
		return fmt.Errorf("read channels columns: %w", err)
	}
	if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		return fmt.Errorf("disable foreign keys: %w", err)
	}
	defer func() { _ = db.Exec("PRAGMA foreign_keys = ON").Error }()
	if err := db.Exec("BEGIN").Error; err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	commit := false
	defer func() {
		if !commit {
			_ = db.Exec("ROLLBACK").Error
		}
	}()

	createSQL := `CREATE TABLE channels_old (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		input_device_id INTEGER NOT NULL,
		input_channel INTEGER NOT NULL DEFAULT 0,
		output_device_id INTEGER NOT NULL DEFAULT 0,
		output_channel INTEGER NOT NULL DEFAULT 0,
		modem_type TEXT NOT NULL DEFAULT 'afsk',
		bit_rate INTEGER NOT NULL DEFAULT 1200,
		mark_freq INTEGER NOT NULL DEFAULT 1200,
		space_freq INTEGER NOT NULL DEFAULT 2200,
		profile TEXT NOT NULL DEFAULT 'A',
		num_slicers INTEGER NOT NULL DEFAULT 1,
		fix_bits TEXT NOT NULL DEFAULT 'none',
		fx25_encode NUMERIC NOT NULL DEFAULT 0,
		il2p_encode NUMERIC NOT NULL DEFAULT 0,
		num_decoders INTEGER NOT NULL DEFAULT 1,
		decoder_offset INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME,
		updated_at DATETIME,
		CONSTRAINT fk_channels_input_device
			FOREIGN KEY (input_device_id) REFERENCES audio_devices(id)
			ON DELETE RESTRICT ON UPDATE RESTRICT
	)`
	if err := db.Exec(createSQL).Error; err != nil {
		return fmt.Errorf("create channels_old: %w", err)
	}
	newCols, err := channelsTableColumns(db, "channels_old")
	if err != nil {
		return fmt.Errorf("read channels_old columns: %w", err)
	}
	shared := intersectStrings(cols, newCols)
	sharedList := joinColumns(shared)
	if err := db.Exec(fmt.Sprintf(`INSERT INTO channels_old (%s) SELECT %s FROM channels`, sharedList, sharedList)).Error; err != nil {
		return fmt.Errorf("copy rows: %w", err)
	}
	if err := db.Exec("DROP TABLE channels").Error; err != nil {
		return fmt.Errorf("drop channels: %w", err)
	}
	if err := db.Exec("ALTER TABLE channels_old RENAME TO channels").Error; err != nil {
		return fmt.Errorf("rename channels_old: %w", err)
	}
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_channels_input_device_id ON channels(input_device_id)").Error; err != nil {
		return fmt.Errorf("create input_device_id index: %w", err)
	}
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_channels_output_device_id ON channels(output_device_id)").Error; err != nil {
		return fmt.Errorf("create output_device_id index: %w", err)
	}
	var violations []struct {
		Table  string `gorm:"column:table"`
		Rowid  int64  `gorm:"column:rowid"`
		Parent string `gorm:"column:parent"`
		Fkid   int64  `gorm:"column:fkid"`
	}
	if err := db.Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil {
		return fmt.Errorf("foreign_key_check: %w", err)
	}
	if len(violations) > 0 {
		return fmt.Errorf("foreign_key_check failed: %+v", violations)
	}
	if err := db.Exec("PRAGMA user_version = 7").Error; err != nil {
		return fmt.Errorf("reset user_version: %w", err)
	}
	if err := db.Exec("COMMIT").Error; err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	commit = true
	return nil
}
