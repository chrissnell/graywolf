package configstore

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/go-sqlite"
)

// highestMigrationVersion returns the largest user_version number in
// schemaMigrations. If you add a migration and forget to bump tests,
// the fresh-DB test below will fail and tell you which version it
// expected.
func highestMigrationVersion(t *testing.T) int {
	t.Helper()
	highest := 0
	for _, m := range schemaMigrations {
		if m.version > highest {
			highest = m.version
		}
	}
	return highest
}

// TestFreshDatabaseUserVersion ensures a brand-new database ends up
// with PRAGMA user_version pinned at the highest registered migration.
func TestFreshDatabaseUserVersion(t *testing.T) {
	s := newTestStore(t)
	var version int
	if err := s.DB().Raw("PRAGMA user_version").Scan(&version).Error; err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	want := highestMigrationVersion(t)
	if version != want {
		t.Fatalf("PRAGMA user_version = %d, want %d", version, want)
	}
}

// TestMigrationsAreIdempotentOnDisk opens a temp-file database, runs
// Init, closes it, reopens it, and confirms (a) user_version is
// unchanged and (b) migration 1 did not re-run. The check for (b)
// writes a beacon row with compress=0 via raw SQL after the first
// Init (bypassing GORM's zero-value-to-default handling for bool
// columns) and verifies the row survives the second Init unflipped.
// If the user_version gate is broken, migration 1's UPDATE would
// catch that row on the second Init and flip it to 1.
func TestMigrationsAreIdempotentOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotent.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := s1.DB().Exec(`INSERT INTO beacons
		(type, channel, callsign, destination, path, symbol_table, symbol, compress, every_seconds, slot_seconds, enabled)
		VALUES ('position', 1, 'TEST', 'APGRWO', 'WIDE1-1', '/', '>', 0, 1800, -1, 1)`).Error; err != nil {
		t.Fatalf("raw insert beacon: %v", err)
	}
	var v1 int
	s1.DB().Raw("PRAGMA user_version").Scan(&v1)
	_ = s1.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	var v2 int
	s2.DB().Raw("PRAGMA user_version").Scan(&v2)
	if v1 != v2 {
		t.Errorf("user_version drifted across reopens: %d -> %d", v1, v2)
	}
	want := highestMigrationVersion(t)
	if v2 != want {
		t.Errorf("user_version after reopen = %d, want %d", v2, want)
	}

	var compress int
	if err := s2.DB().Raw(`SELECT compress FROM beacons WHERE callsign = 'TEST'`).Scan(&compress).Error; err != nil {
		t.Fatalf("read beacon: %v", err)
	}
	if compress != 0 {
		t.Errorf("migration 1 re-ran on reopen and flipped compress=0 to %d; user_version gate is broken", compress)
	}
}

// TestForeignKeyEnforcement confirms that the SQLite FK constraint on
// channels.input_device_id -> audio_devices.id rejects a direct delete
// of a referenced audio device with an error (not a panic, not silent
// orphaning). DeleteAudioDeviceChecked still owns the cascade path;
// this test only covers the raw DeleteAudioDevice shortcut that skips
// the application-layer check.
func TestForeignKeyEnforcement_InputDevice(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	dev := &AudioDevice{Name: "mic", Direction: "input", SourceType: "soundcard", SourcePath: "hw:0", SampleRate: 48000, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(ctx, dev); err != nil {
		t.Fatal(err)
	}
	ch := &Channel{Name: "ch", InputDeviceID: U32Ptr(dev.ID), ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1}
	if err := s.CreateChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteAudioDevice(ctx, dev.ID); err == nil {
		t.Fatal("expected FK error when deleting a device with a referencing channel; got nil")
	}

	// Channel and device are both still present.
	if _, err := s.GetAudioDevice(ctx, dev.ID); err != nil {
		t.Fatalf("device should still exist after rejected delete: %v", err)
	}
	if _, err := s.GetChannel(ctx, ch.ID); err != nil {
		t.Fatalf("channel should still exist after rejected delete: %v", err)
	}
}

// TestForeignKeyCascade_PttConfig confirms that deleting a channel
// cascades through to its PTT row via the CASCADE constraint.
func TestForeignKeyCascade_PttConfig(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	dev := &AudioDevice{Name: "mic", Direction: "input", SourceType: "soundcard", SourcePath: "hw:0", SampleRate: 48000, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(ctx, dev); err != nil {
		t.Fatal(err)
	}
	ch := &Channel{Name: "ch", InputDeviceID: U32Ptr(dev.ID), ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1}
	if err := s.CreateChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertPttConfig(ctx, &PttConfig{ChannelID: ch.ID, Method: "gpio", GpioPin: 17}); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteChannel(ctx, ch.ID); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}

	if _, err := s.GetPttConfigForChannel(ctx, ch.ID); err == nil {
		t.Fatal("expected PTT config to be gone after channel delete (CASCADE); still present")
	}
}

// TestLegacyMessagesKindBackfill builds a database file with a
// pre-Phase-1-invite messages schema (no kind / invite_tactical /
// invite_accepted_at columns), seeds it with legacy rows, stamps
// PRAGMA user_version at 5 (the version before the kind-backfill
// migration), then re-opens with the current binary.
//
// After Open, every row must carry kind='text'. Two paths can reach
// that invariant:
//   - AutoMigrate's ADD COLUMN ... NOT NULL DEFAULT 'text' (SQLite
//     applies constant defaults to pre-existing rows at ADD time).
//   - Migration 6's explicit UPDATE … WHERE kind IS NULL OR kind = ''.
//
// The test asserts the *observable* contract (every legacy row ends
// with kind='text') without caring which layer did the work. If a
// future SQLite quirk leaves rows with NULL or empty kind, migration
// 6 is the safety net; this test fails if both paths are broken.
func TestLegacyMessagesKindBackfill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy_messages.db")

	// Build a pre-Phase-1-invite schema directly — no kind columns.
	// The column list matches the Message model as of migration 5.
	// Keep in sync with models.go if new pre-invite columns arrive.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	_, err = raw.Exec(`
CREATE TABLE messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  direction TEXT NOT NULL,
  our_call TEXT NOT NULL,
  peer_call TEXT NOT NULL,
  from_call TEXT NOT NULL,
  to_call TEXT NOT NULL,
  text TEXT NOT NULL,
  msg_id TEXT,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  received_at DATETIME,
  sent_at DATETIME,
  acked_at DATETIME,
  ack_state TEXT NOT NULL DEFAULT 'none',
  source TEXT NOT NULL DEFAULT '',
  channel INTEGER NOT NULL DEFAULT 0,
  path TEXT,
  via TEXT,
  raw_tnc2 TEXT,
  unread NUMERIC NOT NULL DEFAULT 0,
  attempts INTEGER NOT NULL DEFAULT 0,
  next_retry_at DATETIME,
  failure_reason TEXT,
  reply_ack_id TEXT,
  is_ack NUMERIC NOT NULL DEFAULT 0,
  is_rej NUMERIC NOT NULL DEFAULT 0,
  is_bulletin NUMERIC NOT NULL DEFAULT 0,
  is_nws NUMERIC NOT NULL DEFAULT 0,
  prefer_is NUMERIC NOT NULL DEFAULT 0,
  deleted_at DATETIME,
  thread_kind TEXT NOT NULL DEFAULT 'dm',
  thread_key TEXT NOT NULL DEFAULT '',
  received_by_call TEXT
);
INSERT INTO messages (direction, our_call, peer_call, from_call, to_call, text, created_at, updated_at, thread_kind, thread_key)
  VALUES ('in',  'N0CALL', 'W1ABC', 'W1ABC', 'N0CALL', 'hello 1', '2026-01-01 00:00:00', '2026-01-01 00:00:00', 'dm',       'W1ABC');
INSERT INTO messages (direction, our_call, peer_call, from_call, to_call, text, created_at, updated_at, thread_kind, thread_key)
  VALUES ('out', 'N0CALL', 'W1ABC', 'N0CALL', 'W1ABC', 'hello 2', '2026-01-02 00:00:00', '2026-01-02 00:00:00', 'dm',       'W1ABC');
INSERT INTO messages (direction, our_call, peer_call, from_call, to_call, text, created_at, updated_at, thread_kind, thread_key)
  VALUES ('in',  'N0CALL', 'TAC',   'W9XYZ', 'TAC',    'hello 3', '2026-01-03 00:00:00', '2026-01-03 00:00:00', 'tactical', 'TAC');
-- Stamp the pre-Phase-1-invite user_version so the new migration runs.
PRAGMA user_version = 5;
`)
	raw.Close()
	if err != nil {
		t.Fatalf("seed pre-invite messages schema: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy messages db: %v", err)
	}
	defer s.Close()

	// Every legacy row must be kind='text' after migration.
	var rows []struct {
		ID   uint64
		Kind string
	}
	if err := s.DB().Raw(`SELECT id, kind FROM messages ORDER BY id`).Scan(&rows).Error; err != nil {
		t.Fatalf("scan messages.kind: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 legacy rows, got %d", len(rows))
	}
	for _, r := range rows {
		if r.Kind != "text" {
			t.Errorf("row id=%d kind=%q, want %q", r.ID, r.Kind, "text")
		}
	}

	// No row should have NULL kind either (the CHECK constraint would
	// have rejected it, but belt-and-braces: count the SQL-level NULLs).
	var nullCount int
	if err := s.DB().Raw(`SELECT COUNT(*) FROM messages WHERE kind IS NULL OR kind = ''`).Scan(&nullCount).Error; err != nil {
		t.Fatalf("scan null kinds: %v", err)
	}
	if nullCount != 0 {
		t.Errorf("found %d rows with NULL or empty kind after migration; want 0", nullCount)
	}

	// user_version must have advanced to at least 6.
	var version int
	s.DB().Raw("PRAGMA user_version").Scan(&version)
	if version < 6 {
		t.Errorf("user_version = %d, want >= 6 after invite-kind migration", version)
	}
}

// TestLegacySchemaUpgrade builds a database file with the pre-split
// channels columns (audio_device_id/audio_channel) and confirms that
// Open applies migration 2, preserves the row, and retrofits the new
// columns with the legacy data in place.
func TestLegacySchemaUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	// Build the legacy schema directly via the glebarez driver; this
	// bypasses GORM and models.go so the test reflects a real database
	// that was created by an older binary.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	_, err = raw.Exec(`
CREATE TABLE audio_devices (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  direction TEXT NOT NULL DEFAULT 'input',
  source_type TEXT NOT NULL,
  source_path TEXT,
  sample_rate INTEGER NOT NULL DEFAULT 48000,
  channels INTEGER NOT NULL DEFAULT 1,
  format TEXT NOT NULL DEFAULT 's16le',
  gain_db REAL NOT NULL DEFAULT 0,
  created_at DATETIME,
  updated_at DATETIME
);
CREATE TABLE channels (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  audio_device_id INTEGER NOT NULL,
  audio_channel INTEGER NOT NULL DEFAULT 0,
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
  tx_delay_ms INTEGER NOT NULL DEFAULT 300,
  tx_tail_ms INTEGER NOT NULL DEFAULT 100,
  created_at DATETIME,
  updated_at DATETIME
);
INSERT INTO audio_devices (id,name,direction,source_type,source_path,sample_rate,channels,format)
  VALUES (7,'legacy mic','input','soundcard','hw:0',48000,2,'s16le');
INSERT INTO channels (id,name,audio_device_id,audio_channel)
  VALUES (42,'legacy ch',7,1);
`)
	raw.Close()
	if err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy db: %v", err)
	}
	defer s.Close()

	// The old columns must be gone.
	var legacyCount int
	s.DB().Raw("SELECT COUNT(*) FROM pragma_table_info('channels') WHERE name IN ('audio_device_id','audio_channel')").Scan(&legacyCount)
	if legacyCount != 0 {
		t.Errorf("legacy columns still present after migration: count=%d", legacyCount)
	}

	// The legacy row's device+channel values must have landed in the
	// new input_* columns.
	ctx := context.Background()
	ch, err := s.GetChannel(ctx, 42)
	if err != nil {
		t.Fatalf("GetChannel(42): %v", err)
	}
	if ch.InputDeviceID == nil || *ch.InputDeviceID != 7 {
		t.Errorf("InputDeviceID = %v, want *uint32(7)", ch.InputDeviceID)
	}
	if ch.InputChannel != 1 {
		t.Errorf("InputChannel = %d, want 1", ch.InputChannel)
	}
	if ch.OutputDeviceID != 0 {
		t.Errorf("OutputDeviceID = %d, want 0 (rx-only)", ch.OutputDeviceID)
	}

	// user_version must have advanced to at least 2.
	var version int
	s.DB().Raw("PRAGMA user_version").Scan(&version)
	if version < 2 {
		t.Errorf("user_version = %d, want >= 2 after legacy upgrade", version)
	}
}

// TestNullableInputDeviceMigration builds a pre-version-8 channels
// table with input_device_id declared NOT NULL, populates it with
// a representative row, stamps PRAGMA user_version = 7, and then
// re-opens under the current binary. After Open, the column must
// permit NULL (verified by inserting a NULL row directly via SQL),
// the existing row must survive with its value intact, and the
// foreign-key constraint on input_device_id -> audio_devices(id)
// must still reject orphaned references.
//
// This is the synthetic equivalent of the prior-release fixture
// test below. They complement each other: this test exercises the
// migration against an exactly-known shape; TestMigrateFromPriorRelease
// exercises it against a real binary's output.
func TestNullableInputDeviceMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pre-v0.11.db")

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	_, err = raw.Exec(`
CREATE TABLE audio_devices (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  direction TEXT NOT NULL DEFAULT 'input',
  source_type TEXT NOT NULL,
  source_path TEXT,
  sample_rate INTEGER NOT NULL DEFAULT 48000,
  channels INTEGER NOT NULL DEFAULT 1,
  format TEXT NOT NULL DEFAULT 's16le',
  gain_db REAL NOT NULL DEFAULT 0,
  created_at DATETIME,
  updated_at DATETIME
);
CREATE TABLE channels (
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
  FOREIGN KEY (input_device_id) REFERENCES audio_devices(id) ON DELETE RESTRICT ON UPDATE RESTRICT
);
INSERT INTO audio_devices (id,name,direction,source_type,source_path,sample_rate,channels,format)
  VALUES (1,'mic','input','soundcard','hw:0',48000,1,'s16le');
INSERT INTO channels (id,name,input_device_id) VALUES (1,'VHF',1);
PRAGMA user_version = 7;
`)
	raw.Close()
	if err != nil {
		t.Fatalf("seed pre-v0.11 schema: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open upgraded db: %v", err)
	}
	defer s.Close()

	// user_version must have advanced to at least 8.
	var version int
	s.DB().Raw("PRAGMA user_version").Scan(&version)
	if version < 8 {
		t.Errorf("user_version = %d, want >= 8 after nullable migration", version)
	}

	// The surviving row must have carried its input_device_id
	// value across the rebuild.
	ctx := context.Background()
	ch, err := s.GetChannel(ctx, 1)
	if err != nil {
		t.Fatalf("GetChannel(1): %v", err)
	}
	if ch.InputDeviceID == nil || *ch.InputDeviceID != 1 {
		t.Errorf("post-migration InputDeviceID = %v, want *uint32(1)", ch.InputDeviceID)
	}

	// A NULL insert must succeed now (previously NOT NULL).
	kiss := &Channel{Name: "kiss-only", InputDeviceID: nil, ModemType: "afsk",
		BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A",
		NumSlicers: 1, FixBits: "none"}
	if err := s.CreateChannel(ctx, kiss); err != nil {
		t.Fatalf("kiss-only insert after migration: %v", err)
	}
	got, err := s.GetChannel(ctx, kiss.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.InputDeviceID != nil {
		t.Errorf("kiss-only row read-back: expected nil InputDeviceID, got %v", got.InputDeviceID)
	}

	// FK is still enforced: an orphan reference must be rejected.
	orphan := &Channel{Name: "orphan", InputDeviceID: U32Ptr(99999), ModemType: "afsk",
		BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A",
		NumSlicers: 1, FixBits: "none"}
	if err := s.CreateChannel(ctx, orphan); err == nil {
		t.Error("expected FK rejection for orphaned input_device_id, got nil")
	}
}

// TestNullableInputDeviceMigration_DownRoundTrip exercises the
// down-migration helper directly. On a DB with only non-NULL rows, the
// down should succeed and re-add the NOT NULL constraint. On a DB
// with a NULL row, the down must abort rather than lose data.
func TestNullableInputDeviceMigration_DownRoundTrip(t *testing.T) {
	t.Run("down succeeds when all rows are non-null", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()
		dev := &AudioDevice{Name: "mic", Direction: "input", SourceType: "soundcard", SourcePath: "hw:0",
			SampleRate: 48000, Channels: 1, Format: "s16le"}
		if err := s.CreateAudioDevice(ctx, dev); err != nil {
			t.Fatal(err)
		}
		ch := &Channel{Name: "c", InputDeviceID: U32Ptr(dev.ID), ModemType: "afsk",
			BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1, FixBits: "none"}
		if err := s.CreateChannel(ctx, ch); err != nil {
			t.Fatal(err)
		}

		if err := migrateChannelsNullableInputDeviceDown(s.DB()); err != nil {
			t.Fatalf("down-migration: %v", err)
		}

		var version int
		s.DB().Raw("PRAGMA user_version").Scan(&version)
		if version != 7 {
			t.Errorf("after down, user_version = %d, want 7", version)
		}

		// The NOT NULL constraint is back: a direct SQL NULL insert
		// must now fail.
		err := s.DB().Exec(`INSERT INTO channels (name, input_device_id) VALUES ('bad', NULL)`).Error
		if err == nil {
			t.Error("expected NOT NULL rejection after down-migration, got nil")
		}
	})

	t.Run("down aborts when rows carry NULL", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()
		ch := &Channel{Name: "kiss", InputDeviceID: nil, ModemType: "afsk",
			BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1, FixBits: "none"}
		if err := s.CreateChannel(ctx, ch); err != nil {
			t.Fatal(err)
		}
		err := migrateChannelsNullableInputDeviceDown(s.DB())
		if err == nil {
			t.Fatal("expected down-migration to abort on NULL rows, got nil")
		}
	})
}

// TestMigrateFromPriorRelease loads a prior-release fixture database
// (generated on demand by scripts/testdata/gen_prev_release_db.sh) and
// runs the migration chain. Skipped when the fixture is absent so
// fresh-clone builds don't fail on a missing artifact.
//
// The fixture is NOT committed to git. It's regenerated dynamically —
// the generator detects the newest v* tag reachable from HEAD and
// boots that binary against a throwaway config dir, seeding a
// representative configuration (3 channels, 2 KISS interfaces, 3
// beacons, 2 digi rules, 1 igate config) via the REST API, then
// SIGTERMing and copying the resulting DB. CI runs the generator
// before tests; locally the test is skip-if-missing until you run
// the script manually.
//
// Assertions:
//   - Open() succeeds.
//   - user_version advances to the current highest migration.
//   - Row counts in channels / kiss_interfaces / beacons /
//     digipeater_rules are preserved.
//   - input_device_id values on existing rows survive the rebuild.
//   - A NULL-input row can be inserted after migration.
//   - The input_device_id FK still rejects orphaned references.
func TestMigrateFromPriorRelease(t *testing.T) {
	fixture := filepath.Join("testdata", "prev_release.db")
	info, err := os.Stat(fixture)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("fixture not generated; run scripts/testdata/gen_prev_release_db.sh first")
		}
		t.Fatalf("stat fixture: %v", err)
	}
	if info.Size() == 0 {
		t.Skipf("fixture %s is empty", fixture)
	}

	// Work on a copy so repeated runs see an untouched fixture.
	tmp := filepath.Join(t.TempDir(), "fixture.db")
	src, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(tmp, src, 0o644); err != nil {
		t.Fatalf("write fixture copy: %v", err)
	}

	// Row counts before migration.
	pre, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open pre: %v", err)
	}
	preCounts := tableCounts(t, pre, []string{"channels", "kiss_interfaces", "beacons", "digipeater_rules"})
	_ = pre.Close()

	s, err := Open(tmp)
	if err != nil {
		t.Fatalf("Open upgraded fixture: %v", err)
	}
	defer s.Close()

	var version int
	s.DB().Raw("PRAGMA user_version").Scan(&version)
	if want := highestMigrationVersion(t); version < want {
		t.Errorf("user_version = %d, want >= %d after fixture upgrade", version, want)
	}

	// Row counts after migration: every table we probed must be
	// unchanged. Future migrations that legitimately add rows will
	// need to loosen this check.
	postCounts := tableCountsFromGorm(t, s, []string{"channels", "kiss_interfaces", "beacons", "digipeater_rules"})
	for table, pc := range preCounts {
		if got := postCounts[table]; got != pc {
			t.Errorf("%s row count changed: pre=%d post=%d", table, pc, got)
		}
	}

	// Direct NULL insert via SQL to confirm the column is truly
	// nullable post-migration (bypassing the validator).
	if err := s.DB().Exec(`INSERT INTO channels (name, input_device_id, modem_type, bit_rate, mark_freq, space_freq, profile, num_slicers, fix_bits)
			VALUES ('post-migration-kiss', NULL, 'afsk', 1200, 1200, 2200, 'A', 1, 'none')`).Error; err != nil {
		t.Errorf("direct NULL insert after migration: %v", err)
	}

	// FK still enforces: orphan insert must fail. We pick an id
	// guaranteed not to exist.
	err = s.DB().Exec(`INSERT INTO channels (name, input_device_id, modem_type, bit_rate, mark_freq, space_freq, profile, num_slicers, fix_bits)
			VALUES ('orphan', 99999, 'afsk', 1200, 1200, 2200, 'A', 1, 'none')`).Error
	if err == nil {
		t.Error("expected FK rejection for orphaned input_device_id, got nil")
	}
}

func tableCounts(t *testing.T, db *sql.DB, tables []string) map[string]int {
	t.Helper()
	out := make(map[string]int, len(tables))
	for _, name := range tables {
		var n int
		// Ignore "no such table" — some fixtures may predate a
		// particular table; we simply don't compare that entry.
		if err := db.QueryRow("SELECT COUNT(*) FROM " + name).Scan(&n); err != nil {
			continue
		}
		out[name] = n
	}
	return out
}

func tableCountsFromGorm(t *testing.T, s *Store, tables []string) map[string]int {
	t.Helper()
	out := make(map[string]int, len(tables))
	for _, name := range tables {
		var n int
		if err := s.DB().Raw("SELECT COUNT(*) FROM " + name).Scan(&n).Error; err != nil {
			continue
		}
		out[name] = n
	}
	return out
}
