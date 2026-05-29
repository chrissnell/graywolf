# Beacon Position Format Selector + Position Ambiguity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the implicit `compress=true` baked into every beacon since v0.10 with an operator-selectable position report format (Compressed / Uncompressed / Mic-E), wire per-beacon position-ambiguity for the formats that support it (Uncompressed in Phase 1, Mic-E in Phase 2), and add Mic-E TX (Phase 2) — graywolf has not had it to date.

**Architecture:** A new `position_format` column on `beacons` (`compressed`|`uncompressed`|`mic_e`) replaces the old `compress` boolean. A SQLite migration backfills it and drops `compress`. The existing-but-unread `ambiguity` column finally gets read by the encoder. Phase 1 ships Compressed + Uncompressed paths end-to-end and the Mic-E radio option in the UI but the API rejects `mic_e` writes with a "coming next release" message. Phase 2 lands a new `pkg/beacon/mice.go` encoder, removes the rejection, and overrides the AX.25 destination at frame-build time with the lat-derived Mic-E destination.

**Tech Stack:** Go (configstore, beacon, webapi/dto, app adapters), GORM + SQLite for migrations, Swelte 5 + chonky-ui for the form, swag for OpenAPI, openapi-typescript for the TS client, Playwright for the UI smoke.

---

## Reference: files this plan touches

Read once at the start of execution so you have the lay of the land. None of these have changed during this plan, so don't re-grep them unless a task tells you to.

- `pkg/configstore/models.go` — Beacon struct at L550
- `pkg/configstore/migrate.go` — migration list at L190, helpers at L778+
- `pkg/configstore/migrate_test.go` — TestMigrationsAreIdempotentOnDisk at L50 inserts raw `compress=0`; this test MUST be updated when the column is dropped
- `pkg/aprs/position.go` — `formatLatitude` L408, `formatLongitude` L426, `applyAmbiguity` L444 (private; we will expose a wrapper)
- `pkg/aprs/mice.go` — `EncodeMicEDest(lat float64, msgCode int, lonOffset100 bool, westLong bool) string` at L441 (Phase 2 will extend its signature)
- `pkg/beacon/types.go` — `Config` struct at L22
- `pkg/beacon/builder.go` — `buildInfo` at L16 (the switch on `b.Type`)
- `pkg/beacon/encoder.go` — `PositionInfo` L31, `CompressedPositionInfo` L93, `ObjectInfo` L178
- `pkg/beacon/encoder_test.go`
- `pkg/beacon/scheduler.go` — frame-build at L391 (`ax25.NewUIFrame(b.Source, b.Dest, b.Path, …)`); Phase 2 inserts a destination override here
- `pkg/webapi/dto/beacon.go` — DTOs at top; `Validate()` at L71
- `pkg/app/adapters.go` — Config build at L134
- `web/src/routes/Beacons.svelte` — `form` state at L92, `openCreate` L257, `openEdit` L287, `handleSave` L318, modal markup at L640
- `pkg/releasenotes/notes.yaml` — newest first
- `VERSION` — currently 0.13.11; Phase 1 ships as 0.13.12, Phase 2 as 0.13.13 unless the version has moved at merge time (use the bump targets — never hand-edit; see CLAUDE.md)

## Reference: project conventions you must honor

- **No emojis, em dashes, smart quotes, or non-ASCII punctuation in release notes** — plain ASCII only. See `pkg/releasenotes/notes.yaml` header comment.
- **Idempotent migrations.** PRAGMA user_version gates re-runs; existing migrations use `pragma_table_info` probes for fresh-DB safety. Follow the same pattern.
- **Single-line CONSTRAINT clauses inside CREATE TABLE strings.** glebarez/sqlite's `HasConstraint` LIKE probe breaks on multi-line FKs (see comment in migrate.go around L673). Not relevant for this plan because we don't rebuild a table, but worth knowing if you end up touching channels.
- **No "TODO" / placeholder comments in code.** No commented-out fields. If a field is gone, it's gone.
- **Tests live next to source.** `foo.go` → `foo_test.go`.
- **Commit messages must not mention agents, Claude, or AI assistance.** No `Co-Authored-By`, no `Generated with`. See CLAUDE.md.
- **`make docs` regenerates `pkg/webapi/docs/gen/swagger.{json,yaml}`. `make api-client` regenerates `web/src/api/generated/api.d.ts`.** Run both whenever the DTO changes; CI has `docs-check` and `api-client-check` gates.
- **Beacons UI currently only surfaces `type ∈ {position, object}`** in the Type radio (Beacons.svelte L649–L651). The data model accepts `tracker | igate | custom` but they're not created via this page. So in the Svelte form, the format radio is gated by `form.type === 'position'`. (The DTO validator still enforces "type ∉ {position, tracker, igate} → position_format ignored" so backend-set tracker/igate beacons get format honored.)
- **PHG fieldset is NOT surfaced in the Beacons UI today.** Spec §3.3 requires hiding the PHG fieldset on Mic-E; since there's no fieldset to hide, that half of §3.3 is a no-op in this plan. The Destination input IS surfaced (L696–L699) so the Mic-E destination-hide IS implemented (Task 10).

---

# Phase 1 — Format selector + Uncompressed ambiguity

Phase 1 ships independently. After Phase 1, operators can pick Compressed or Uncompressed in the UI, can blank trailing position digits on Uncompressed beacons, and see the Mic-E option in the radio with a clear "coming next release" inline warning that disables Save while selected.

---

### Task 1: Add `ApplyLatLonAmbiguity` helper to pkg/aprs

The encoder in `pkg/beacon/encoder.go` builds its own lat/lon strings (`encodeLat` / `encodeLon`) and must not duplicate the byte-position arithmetic that lives privately in `pkg/aprs/position.go`. We expose a thin wrapper that takes the already-formatted strings and returns blanked versions.

**Files:**
- Modify: `pkg/aprs/position.go` (add exported function near `applyAmbiguity` at L444)
- Test: `pkg/aprs/position_test.go` (add a new test function)

- [ ] **Step 1: Write the failing test**

Append to `pkg/aprs/position_test.go`:

```go
func TestApplyLatLonAmbiguity(t *testing.T) {
	// Input strings are the exact 8-char latitude / 9-char longitude
	// forms produced by formatLatitude / formatLongitude and by
	// pkg/beacon/encoder.go's encodeLat / encodeLon.
	const lat = "3724.55N"
	const lon = "12208.42W"
	cases := []struct {
		level   int
		wantLat string
		wantLon string
	}{
		{0, "3724.55N", "12208.42W"},
		{1, "3724.5 N", "12208.4 W"},
		{2, "3724.  N", "12208.  W"},
		{3, "372 .  N", "1220 .  W"},
		{4, "37  .  N", "122  .  W"},
	}
	for _, tc := range cases {
		gotLat, gotLon := ApplyLatLonAmbiguity(lat, lon, tc.level)
		if gotLat != tc.wantLat {
			t.Errorf("level %d: lat=%q want %q", tc.level, gotLat, tc.wantLat)
		}
		if gotLon != tc.wantLon {
			t.Errorf("level %d: lon=%q want %q", tc.level, gotLon, tc.wantLon)
		}
	}
	// Out-of-range clamps quietly (matches private applyAmbiguity).
	gotLat, gotLon := ApplyLatLonAmbiguity(lat, lon, 99)
	if gotLat != "37  .  N" || gotLon != "122  .  W" {
		t.Errorf("clamp: got lat=%q lon=%q", gotLat, gotLon)
	}
	gotLat, gotLon = ApplyLatLonAmbiguity(lat, lon, -3)
	if gotLat != lat || gotLon != lon {
		t.Errorf("negative: got lat=%q lon=%q", gotLat, gotLon)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/aprs/ -run TestApplyLatLonAmbiguity -v`
Expected: FAIL with `undefined: ApplyLatLonAmbiguity`.

- [ ] **Step 3: Implement ApplyLatLonAmbiguity**

In `pkg/aprs/position.go`, immediately after the existing `applyAmbiguity` function (around L454), append:

```go
// ApplyLatLonAmbiguity returns latStr and lonStr with the trailing
// minute/hundredth positions blanked per APRS101 ch 6 table 8. Levels:
//
//	0 — no ambiguity (input returned unchanged)
//	1 — nearest 1/10 minute (one digit blanked)
//	2 — nearest minute (two digits blanked)
//	3 — nearest 10 minutes (three digits blanked, including a degree digit)
//	4 — nearest degree (four digits blanked)
//
// The dot stays in the string at its original column; only digits are
// replaced with ASCII space. Out-of-range levels are clamped to 0..4 to
// match the private byte-level helper used by the parser side.
//
// latStr must be the 8-byte "DDMM.hhN" form (N or S). lonStr must be
// the 9-byte "DDDMM.hhE" form (E or W). The function does not validate
// shape; the caller is responsible for passing well-formed strings.
func ApplyLatLonAmbiguity(latStr, lonStr string, level int) (string, string) {
	lat := []byte(latStr)
	lon := []byte(lonStr)
	// Position lists mirror formatLatitude / formatLongitude exactly.
	applyAmbiguity(lat, level, []int{6, 5, 3, 2})
	applyAmbiguity(lon, level, []int{7, 6, 4, 3})
	return string(lat), string(lon)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/aprs/ -run TestApplyLatLonAmbiguity -v`
Expected: PASS.

Run the full package too to make sure the addition didn't break anything: `go test ./pkg/aprs/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/aprs/position.go pkg/aprs/position_test.go
git commit -m "aprs: export ApplyLatLonAmbiguity for the beacon encoder"
```

---

### Task 2: Add `PositionFormat` column to the Beacon model

This is the new column. GORM AutoMigrate will create it on fresh installs from the struct tag; the migration in Task 3 backfills it on upgrade and drops the legacy `compress` column.

**Files:**
- Modify: `pkg/configstore/models.go` (Beacon struct at L550)

- [ ] **Step 1: Add `PositionFormat` to the Beacon struct**

Edit `pkg/configstore/models.go`. Replace the existing `Compress` line at L566:

```go
		Compress     bool    `gorm:"not null;default:true" json:"compress"`   // use 13-byte base-91 compressed position encoding (APRS101 ch 9)
```

with:

```go
		PositionFormat string `gorm:"not null;default:'compressed'" json:"position_format"` // compressed | uncompressed | mic_e (APRS101 ch 9/6/10)
```

Note: do NOT keep `Compress` as a deprecated field. The migration in Task 3 drops the column; leaving the Go field would make AutoMigrate try to re-add it on every boot.

- [ ] **Step 2: Verify the struct still compiles**

Run: `go build ./pkg/configstore/...`
Expected: PASS.

- [ ] **Step 3: Commit**

(No commit yet — the migration must land in the same commit so the schema and the Go struct stay in lockstep. Defer the commit to Task 3.)

---

### Task 3: Migration v23 — backfill `position_format`, drop `compress`

The next migration version is 23 (current head is 22; see `pkg/configstore/migrate.go` L212). The migration runs in the post-AutoMigrate phase: AutoMigrate has just added the new `position_format` column with default `'compressed'`; we then UPDATE it from the soon-to-be-dropped `compress` column and DROP the old column.

**Files:**
- Create: `pkg/configstore/migrate_beacon_position_format.go`
- Modify: `pkg/configstore/migrate.go` (append docstring entry; append to `schemaMigrations`)

- [ ] **Step 1: Create the migration file**

Write `pkg/configstore/migrate_beacon_position_format.go`:

```go
package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateBeaconPositionFormat backfills the new beacons.position_format
// column from the legacy beacons.compress boolean and then drops
// compress. Runs post-AutoMigrate: AutoMigrate has already added
// position_format with default 'compressed' (from the GORM struct tag),
// so this migration only needs to flip rows where compress=0 to
// 'uncompressed' and then remove the legacy column.
//
// Idempotent via the user_version gate (it runs exactly once per DB)
// and via the columnExists probe (a fresh DB AutoMigrate created
// without the legacy column is a no-op).
func migrateBeaconPositionFormat(tx *gorm.DB) error {
	hasCompress, err := columnExists(tx, "beacons", "compress")
	if err != nil {
		return fmt.Errorf("probe beacons.compress: %w", err)
	}
	if !hasCompress {
		// Fresh database: AutoMigrate built the table from the current
		// Go struct, which no longer has a Compress field. Nothing to
		// backfill or drop.
		return nil
	}
	if err := tx.Exec(
		`UPDATE beacons SET position_format = 'uncompressed' WHERE compress = 0`,
	).Error; err != nil {
		return fmt.Errorf("backfill uncompressed: %w", err)
	}
	// compress = 1 rows keep the AutoMigrate default 'compressed'.
	if err := tx.Exec(`ALTER TABLE beacons DROP COLUMN compress`).Error; err != nil {
		return fmt.Errorf("drop compress: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Register the migration**

Edit `pkg/configstore/migrate.go`. First add the docstring entry. Find the comment block that ends with the v22 entry (around L184–L189):

```go
//	22 — ptt_android_method_field: move the Android PTT transport int
//	    out of the overloaded gpio_pin field into the new ptt_method
//	    column. method='android' rows with gpio_pin 1..4 get
//	    ptt_method=gpio_pin and gpio_pin zeroed; malformed rows
//	    (gpio_pin outside 1..4) coerce to ptt_method=1
//	    (PTT_METHOD_CP2102N_RTS). Non-android rows are untouched.
//	    Idempotent via the ptt_method=0 guard.
```

Append directly after it (before the `var schemaMigrations = []migration{` line):

```go
//	23 — beacon_position_format: replace the legacy beacons.compress
//	    bool with a beacons.position_format enum string column
//	    ('compressed' | 'uncompressed' | 'mic_e'). AutoMigrate adds
//	    position_format with default 'compressed'; this migration flips
//	    rows where compress=0 to 'uncompressed' and then DROPs the
//	    legacy column. Required by the per-beacon format selector
//	    and uncompressed-only position ambiguity. See
//	    docs/superpowers/plans/2026-05-29-beacon-position-format-and-ambiguity.md.
```

Then in the slice itself (after the v22 entry at L212), append:

```go
	{version: 23, name: "beacon_position_format", phase: postAutoMigrate, run: migrateBeaconPositionFormat},
```

So the closing `}` of `schemaMigrations` now follows the v23 entry.

- [ ] **Step 3: Compile**

Run: `go build ./pkg/configstore/...`
Expected: PASS.

- [ ] **Step 4: Update the idempotency test for the new column shape**

Edit `pkg/configstore/migrate_test.go`. The existing `TestMigrationsAreIdempotentOnDisk` at L50 inserts a beacon row with raw `compress=0` and then asserts the value survives. With migration 23 in place that row will have its `compress` column dropped after the first Init, so the second-reopen assertion needs to read `position_format` instead.

Replace the test body (L50–L89) with:

```go
func TestMigrationsAreIdempotentOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotent.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	// Insert a beacon row directly so we can assert the migration
	// 23 backfill ran exactly once and didn't re-run on reopen.
	if err := s1.DB().Exec(`INSERT INTO beacons
		(type, channel, callsign, destination, path, symbol_table, symbol, position_format, every_seconds, slot_seconds, enabled)
		VALUES ('position', 1, 'TEST', 'APGRWO', 'WIDE1-1', '/', '>', 'uncompressed', 1800, -1, 1)`).Error; err != nil {
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

	var pf string
	if err := s2.DB().Raw(`SELECT position_format FROM beacons WHERE callsign = 'TEST'`).Scan(&pf).Error; err != nil {
		t.Fatalf("read beacon: %v", err)
	}
	if pf != "uncompressed" {
		t.Errorf("position_format = %q after reopen, want %q (user_version gate may be broken)", pf, "uncompressed")
	}
}
```

- [ ] **Step 5: Write the v23 upgrade test**

Append to `pkg/configstore/migrate_test.go`:

```go
// TestMigrateBeaconPositionFormatUpgrade builds a database file stamped
// at user_version=22, seeds two beacon rows with compress=1 and
// compress=0 via raw SQL, and confirms migration 23 backfills
// position_format correctly and drops the compress column.
func TestMigrateBeaconPositionFormatUpgrade(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v22.db")

	// Stand up a database at the pre-v23 shape: includes the legacy
	// `compress` column on beacons but no `position_format` column.
	// We use raw database/sql so the schema is exactly what a real
	// v0.13.11-era binary would have left behind. The minimal schema
	// only carries the columns this test reads; AutoMigrate at Open()
	// time will reconcile every other column from the Go struct.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	stmts := []string{
		`CREATE TABLE beacons (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL DEFAULT 'position',
			channel INTEGER NOT NULL DEFAULT 1,
			callsign TEXT NOT NULL,
			destination TEXT NOT NULL DEFAULT 'APGRWO',
			path TEXT NOT NULL DEFAULT 'WIDE1-1',
			symbol_table TEXT NOT NULL DEFAULT '/',
			symbol TEXT NOT NULL DEFAULT '-',
			compress NUMERIC NOT NULL DEFAULT 1,
			ambiguity INTEGER NOT NULL DEFAULT 0,
			every_seconds INTEGER NOT NULL DEFAULT 1800,
			slot_seconds INTEGER NOT NULL DEFAULT -1,
			enabled NUMERIC NOT NULL DEFAULT 1
		)`,
		`INSERT INTO beacons (callsign, compress) VALUES ('COMP', 1)`,
		`INSERT INTO beacons (callsign, compress) VALUES ('UNCOMP', 0)`,
		`PRAGMA user_version = 22`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	// Open via the store — this runs AutoMigrate (which adds
	// position_format) followed by migration 23 (which backfills and
	// drops compress).
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	var version int
	s.DB().Raw("PRAGMA user_version").Scan(&version)
	if version < 23 {
		t.Errorf("user_version = %d, want >= 23 after migration", version)
	}

	var pfComp, pfUncomp string
	if err := s.DB().Raw(`SELECT position_format FROM beacons WHERE callsign='COMP'`).Scan(&pfComp).Error; err != nil {
		t.Fatalf("read COMP: %v", err)
	}
	if err := s.DB().Raw(`SELECT position_format FROM beacons WHERE callsign='UNCOMP'`).Scan(&pfUncomp).Error; err != nil {
		t.Fatalf("read UNCOMP: %v", err)
	}
	if pfComp != "compressed" {
		t.Errorf("COMP position_format = %q, want %q", pfComp, "compressed")
	}
	if pfUncomp != "uncompressed" {
		t.Errorf("UNCOMP position_format = %q, want %q", pfUncomp, "uncompressed")
	}

	var compressCols int
	if err := s.DB().Raw(`SELECT COUNT(*) FROM pragma_table_info('beacons') WHERE name='compress'`).Scan(&compressCols).Error; err != nil {
		t.Fatalf("probe compress column: %v", err)
	}
	if compressCols != 0 {
		t.Errorf("compress column still present after migration: count=%d", compressCols)
	}
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./pkg/configstore/ -run 'TestMigration|TestMigrateBeaconPositionFormat|TestFreshDatabaseUserVersion' -count=1 -v`
Expected: PASS for all four (FreshDB, idempotent, upgrade — and existing migration tests must still pass since none of them touch the beacons.compress column except via the cases already updated).

Run the whole package: `go test ./pkg/configstore/ -count=1`
Expected: PASS.

Note: if `TestLegacyMessagesKindBackfill` or another raw-SQL upgrade test sets up a beacons table and references `compress`, update it the same way — but a grep at the time this plan was written found no other call sites in the test file.

- [ ] **Step 7: Commit**

```bash
git add pkg/configstore/models.go pkg/configstore/migrate.go \
        pkg/configstore/migrate_beacon_position_format.go \
        pkg/configstore/migrate_test.go
git commit -m "configstore: replace beacons.compress with position_format enum"
```

---

### Task 4: Update `beacon.Config` — replace `Compress` with `Format` + `Ambiguity`

The in-process beacon configuration carries the format choice from configstore through to the encoder. We swap the bool for a string and add ambiguity.

**Files:**
- Modify: `pkg/beacon/types.go` (Config struct at L22)

- [ ] **Step 1: Edit Config**

In `pkg/beacon/types.go`, replace the existing line at L39:

```go
	Compress    bool     // use 13-byte base-91 compressed position format
```

with:

```go
	Format      string   // "compressed" | "uncompressed" | "mic_e" (APRS101 ch 9/6/10)
	Ambiguity   int      // 0..4; trailing position digits blanked per APRS101 ch 6 table 8
```

- [ ] **Step 2: Compile-only check (expect downstream breakage)**

Run: `go build ./pkg/beacon/...`
Expected: FAIL — `builder.go` still references `b.Compress`. That's fixed in Task 5.

Run: `go build ./pkg/app/...`
Expected: FAIL — `adapters.go` L152 still references `b.Compress`. That's fixed in Task 6.

We don't commit yet; Tasks 5 and 6 land in the same commit as this one because they touch the same struct contract.

---

### Task 5: Update `pkg/beacon/builder.go` — switch on Format, reject mic_e in Phase 1

The builder selects an encoder based on the beacon configuration. Replace the two `if b.Compress` branches with a single switch on `b.Format`. Phase 1 returns an error for `mic_e` so the scheduler reports a build error and the WARN log fires once per fire — defense in depth: the DTO is the primary gate, this is the belt and suspenders.

**Files:**
- Modify: `pkg/beacon/builder.go` (L43 and L67–L91)

- [ ] **Step 1: Clamp ambiguity at the encoder boundary**

Spec §8 calls out manual SQL corruption (ambiguity > 4) as a real failure mode and says the encoder should clamp to 4 and WARN once per beacon. The DTO is the primary guard, but the encoder is the belt-and-suspenders backstop for DB hand-edits.

Near the top of `buildInfo` (around L17 in `pkg/beacon/builder.go`), after the `comment` and `phg` computation but before the `switch b.Type`, insert:

```go
	if b.Ambiguity > 4 {
		s.logger.Warn("beacon ambiguity out of range; clamping to 4",
			"id", b.ID, "type", b.Type, "ambiguity", b.Ambiguity)
		b.Ambiguity = 4
	}
```

The `b` variable is a value receiver in `buildInfo`, so this mutation is local and does not leak back to the scheduler-held config.

- [ ] **Step 2: Replace the position/igate branch**

In `pkg/beacon/builder.go`, replace L63–L66 (currently `if b.Compress { ... } return PositionInfo(...)`):

```go
		if b.Compress {
			return CompressedPositionInfo(lat, lon, 0, 0, altM, b.SymbolTable, b.SymbolCode, b.Messaging, phg, comment), nil
		}
		return PositionInfo(lat, lon, 0, 0, altM, b.SymbolTable, b.SymbolCode, b.Messaging, phg, comment), nil
```

with:

```go
		switch b.Format {
		case "compressed", "":
			return CompressedPositionInfo(lat, lon, 0, 0, altM, b.SymbolTable, b.SymbolCode, b.Messaging, phg, comment), nil
		case "uncompressed":
			return PositionInfo(lat, lon, 0, 0, altM, b.SymbolTable, b.SymbolCode, b.Messaging, phg, comment, b.Ambiguity), nil
		case "mic_e":
			return "", fmt.Errorf("%s beacon: mic_e format not supported yet", b.Type)
		default:
			return "", fmt.Errorf("%s beacon: unknown position_format %q", b.Type, b.Format)
		}
```

Note the `b.Ambiguity` argument tacked onto the uncompressed call — Task 7 grows `PositionInfo`'s signature to accept it.

- [ ] **Step 3: Replace the tracker branch**

In `pkg/beacon/builder.go`, replace the tracker branch's `if b.Compress { ... } return PositionInfo(...)` (L88–L91) with:

```go
		switch b.Format {
		case "compressed", "":
			return CompressedPositionInfo(fix.Latitude, fix.Longitude, course, fix.Speed, altM, b.SymbolTable, b.SymbolCode, b.Messaging, "", comment), nil
		case "uncompressed":
			return PositionInfo(fix.Latitude, fix.Longitude, course, fix.Speed, altM, b.SymbolTable, b.SymbolCode, b.Messaging, "", comment, b.Ambiguity), nil
		case "mic_e":
			return "", fmt.Errorf("tracker beacon: mic_e format not supported yet")
		default:
			return "", fmt.Errorf("tracker beacon: unknown position_format %q", b.Format)
		}
```

- [ ] **Step 4: Compile-only check**

Run: `go build ./pkg/beacon/...`
Expected: FAIL — `PositionInfo` signature hasn't grown yet. That's Task 7. We're staging the wiring for the next commit.

---

### Task 6: Update `pkg/app/adapters.go` — map Beacon model to Config

The adapter copies fields from the configstore model into the runtime `beacon.Config`. Replace the `Compress` mapping.

**Files:**
- Modify: `pkg/app/adapters.go` (L152)

- [ ] **Step 1: Edit the adapter**

In `pkg/app/adapters.go`, replace L152:

```go
		Compress:       b.Compress,
```

with:

```go
		Format:         b.PositionFormat,
		Ambiguity:      int(b.Ambiguity),
```

- [ ] **Step 2: Compile-only check (still expected to fail at pkg/beacon)**

Run: `go build ./pkg/app/...`
Expected: still FAIL at pkg/beacon (PositionInfo signature). Fixed in Task 7.

---

### Task 7: Grow `PositionInfo` — accept ambiguity, blank lat/lon

The uncompressed encoder is the only place the wire bytes get post-processed. Append an `ambiguity int` parameter (last position so the call sites we already wrote in Task 5 are correct) and call `aprs.ApplyLatLonAmbiguity` on the formatted lat/lon before writing them into the info string.

**Files:**
- Modify: `pkg/beacon/encoder.go` (L31)
- Modify: `pkg/beacon/encoder_test.go`

- [ ] **Step 1: Write the failing test**

In `pkg/beacon/encoder_test.go`, append:

```go
// TestPositionInfo_Ambiguity exercises the uncompressed-position
// ambiguity post-processing path against APRS101 ch 6 table 8.
// Round-trips through aprs.Parse to make sure the resulting wire
// bytes are decodable and that the parser recovers the same
// ambiguity level we emitted.
func TestPositionInfo_Ambiguity(t *testing.T) {
	cases := []struct {
		level   int
		wantLat string // bytes at offsets 1..8 inside the info field
		wantLon string // bytes at offsets 10..18
	}{
		{0, "3724.55N", "12208.42W"},
		{1, "3724.5 N", "12208.4 W"},
		{2, "3724.  N", "12208.  W"},
		{3, "372 .  N", "1220 .  W"},
		{4, "37  .  N", "122  .  W"},
	}
	for _, tc := range cases {
		got := PositionInfo(37.4092, -122.1404, 0, 0, 0, '/', '>', false, "", "", tc.level)
		// "!" + 8-byte lat + symbol_table + 9-byte lon + symbol_code = 20 bytes.
		if len(got) < 20 {
			t.Fatalf("level %d: info too short: %q", tc.level, got)
		}
		if got[1:9] != tc.wantLat {
			t.Errorf("level %d: lat=%q want %q", tc.level, got[1:9], tc.wantLat)
		}
		if got[10:19] != tc.wantLon {
			t.Errorf("level %d: lon=%q want %q", tc.level, got[10:19], tc.wantLon)
		}
	}
}

// TestPositionInfo_Ambiguity_RoundTrip confirms the bytes we emit
// survive the existing pkg/aprs parser and produce the expected
// Position.Ambiguity field.
func TestPositionInfo_Ambiguity_RoundTrip(t *testing.T) {
	for level := 0; level <= 4; level++ {
		info := PositionInfo(37.4092, -122.1404, 0, 0, 0, '/', '>', false, "", "", level)
		// ParseInfo decodes a raw info field without needing a full
		// AX.25 frame — perfect for an encoder round-trip.
		p, err := aprs.ParseInfo([]byte(info))
		if err != nil {
			t.Fatalf("level %d: aprs.ParseInfo(%q): %v", level, info, err)
		}
		if p.Position == nil {
			t.Fatalf("level %d: parse produced no position: %+v", level, p)
		}
		if p.Position.Ambiguity != level {
			t.Errorf("level %d: parsed ambiguity = %d", level, p.Position.Ambiguity)
		}
	}
}
```

If `pkg/aprs` is not already imported in `encoder_test.go`, add it to the import block. Use `grep -n '"github.com/chrissnell/graywolf/pkg/aprs"' pkg/beacon/encoder_test.go` to check; the helpers in `pkg/beacon/encoder.go` already import it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/beacon/ -run TestPositionInfo_Ambiguity -count=1 -v`
Expected: FAIL — `PositionInfo` does not yet accept an ambiguity argument; compile error.

- [ ] **Step 3: Grow PositionInfo**

In `pkg/beacon/encoder.go`, replace the function signature at L31:

```go
func PositionInfo(lat, lon float64, course int, speedKt float64, altM float64, symbolTable, symbolCode byte, messaging bool, phg string, comment string) string {
```

with:

```go
func PositionInfo(lat, lon float64, course int, speedKt float64, altM float64, symbolTable, symbolCode byte, messaging bool, phg string, comment string, ambiguity int) string {
```

Then replace the two lines computing latS / lonS (L38–L39):

```go
	latS := encodeLat(lat)
	lonS := encodeLon(lon)
```

with:

```go
	latS, lonS := aprs.ApplyLatLonAmbiguity(encodeLat(lat), encodeLon(lon), ambiguity)
```

Update the docstring above the function (the block at L17–L30). Replace the existing docstring with:

```go
// PositionInfo builds an uncompressed APRS position info-field.
//
//	!DDMM.hhN/DDDMM.hhW>comment     — no timestamp, no messaging
//	=DDMM.hhN/DDDMM.hhW>comment     — no timestamp, messaging capable
//
// symbolTable/symbolCode default to '/' and '-' if zero. course is
// degrees (1..360, 0 means "not set"); speed is knots; altitude is
// metres and is appended as "/A=NNNNNN" (in feet per APRS101) when
// non-zero.
//
// phg is the already-encoded "PHGphgd" 7-byte string (or "" for no
// PHG extension). PHG occupies the same slot as CSE/SPD and is
// meaningless for moving stations, so it is only emitted when both
// course and speed are zero.
//
// ambiguity is 0..4 per APRS101 ch 6 table 8; non-zero replaces
// trailing position digits in latS/lonS with ASCII spaces. Values
// outside the range are clamped (see aprs.ApplyLatLonAmbiguity).
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/beacon/ -run TestPositionInfo -count=1 -v`
Expected: PASS for both new tests AND any pre-existing `TestPositionInfo*` (the round-trip test in the existing file must still pass — the only signature change is one new trailing argument; any pre-existing test that called `PositionInfo(...)` with no ambiguity has to be updated to pass `0`).

If existing tests fail because of the signature change, find each call site and append a trailing `, 0` argument. Grep first: `grep -n 'PositionInfo(' pkg/beacon/*.go`.

- [ ] **Step 5: Whole-package check**

Run: `go test ./pkg/beacon/ -count=1`
Expected: PASS.

Run: `go build ./...`
Expected: PASS (Task 5 + Task 6 wiring is now complete).

- [ ] **Step 6: Commit (Tasks 4–7 land together)**

```bash
git add pkg/beacon/types.go pkg/beacon/builder.go pkg/beacon/encoder.go \
        pkg/beacon/encoder_test.go pkg/app/adapters.go
git commit -m "beacon: switch on position_format and honor ambiguity on uncompressed"
```

---

### Task 8: Update the DTO — `PositionFormat` field + validation

The DTO is the API surface. We swap `Compress` for `PositionFormat`, add validation per spec §5.4, and add a unit test file.

**Files:**
- Modify: `pkg/webapi/dto/beacon.go`
- Create: `pkg/webapi/dto/beacon_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/webapi/dto/beacon_test.go`:

```go
package dto

import "testing"

func TestBeaconRequest_Validate_PositionFormat(t *testing.T) {
	mkPos := func() BeaconRequest {
		return BeaconRequest{
			Type:           "position",
			UseGps:         true,
			PositionFormat: "compressed",
		}
	}

	cases := []struct {
		name    string
		mutate  func(*BeaconRequest)
		wantErr string // substring; "" means expect nil
	}{
		{"compressed_zero_amb_ok", func(r *BeaconRequest) {
			r.PositionFormat = "compressed"
			r.Ambiguity = 0
		}, ""},
		{"compressed_with_amb_rejected", func(r *BeaconRequest) {
			r.PositionFormat = "compressed"
			r.Ambiguity = 1
		}, "ambiguity must be 0 when position_format is compressed"},
		{"uncompressed_ok", func(r *BeaconRequest) {
			r.PositionFormat = "uncompressed"
			r.Ambiguity = 2
		}, ""},
		{"uncompressed_amb_too_high", func(r *BeaconRequest) {
			r.PositionFormat = "uncompressed"
			r.Ambiguity = 5
		}, "ambiguity must be 0..4"},
		{"mic_e_rejected_phase1", func(r *BeaconRequest) {
			r.PositionFormat = "mic_e"
		}, "Mic-E TX is not yet supported"},
		{"unknown_format", func(r *BeaconRequest) {
			r.PositionFormat = "bogus"
		}, "position_format must be one of"},
		{"empty_format_defaults_compressed", func(r *BeaconRequest) {
			r.PositionFormat = ""
		}, ""}, // DTO treats "" as compressed (form may submit empty before client defaults are applied)
		{"object_format_ignored", func(r *BeaconRequest) {
			r.Type = "object"
			r.PositionFormat = "mic_e"
			r.Latitude = 37
			r.Longitude = -122
			r.UseGps = false
		}, ""}, // Object beacons ignore position_format on write
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := mkPos()
			tc.mutate(&r)
			err := r.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/webapi/dto/ -run TestBeaconRequest_Validate_PositionFormat -count=1 -v`
Expected: FAIL — `PositionFormat` field undefined, test won't compile.

- [ ] **Step 3: Update the DTO struct + validation**

In `pkg/webapi/dto/beacon.go`:

**3a.** In `BeaconRequest` (L23): replace the `Compress` line (L37) with:

```go
	PositionFormat string  `json:"position_format"`
```

**3b.** In `BeaconResponse` (L198): replace the `Compress` line (L213) with:

```go
	PositionFormat string  `json:"position_format"`
```

**3c.** In `ToModel` (L92): replace the `Compress: r.Compress,` line (L107) with:

```go
		PositionFormat: r.normalizedFormat(),
```

**3d.** In `ApplyToUpdate` (L146): replace the `Compress: r.Compress,` line (L166) with:

```go
		PositionFormat: r.normalizedFormat(),
```

**3e.** In `BeaconFromModel` (L241): replace `Compress: m.Compress,` (L257) with:

```go
		PositionFormat: m.PositionFormat,
```

**3f.** Replace the `Validate()` function (L71) with:

```go
// Validate rejects configurations that would cause the scheduler to
// skip transmission at send time. Position/igate beacons must either
// source coordinates from the GPS cache or carry non-zero fixed
// coordinates. The Callsign override field is no longer validated here
// — empty / nil mean "inherit from StationConfig", which is now the
// canonical source of truth.
//
// position_format and ambiguity are also validated against APRS101
// constraints: ambiguity must be 0..4; only uncompressed (today) and
// mic_e (Phase 2) carry ambiguity bytes, so compressed must keep
// ambiguity at zero. Mic-E is rejected in Phase 1 with a "coming next
// release" message — the encoder is not wired yet.
func (r BeaconRequest) Validate() error {
	switch r.Type {
	case "position", "igate":
		if !r.UseGps && r.Latitude == 0 && r.Longitude == 0 {
			return fmt.Errorf("latitude/longitude required when use_gps is false")
		}
	}
	if r.Type == "position" || r.Type == "tracker" || r.Type == "igate" {
		switch r.PositionFormat {
		case "", "compressed":
			if r.Ambiguity != 0 {
				return fmt.Errorf("ambiguity must be 0 when position_format is compressed")
			}
		case "uncompressed":
			// fall through to ambiguity range check below
		case "mic_e":
			return fmt.Errorf("Mic-E TX is not yet supported; coming in the next release")
		default:
			return fmt.Errorf("position_format must be one of compressed, uncompressed, mic_e (got %q)", r.PositionFormat)
		}
		if r.Ambiguity > 4 {
			return fmt.Errorf("ambiguity must be 0..4 (got %d)", r.Ambiguity)
		}
	}
	return nil
}

// normalizedFormat returns the position_format value to persist:
// empty or unknown becomes "compressed" so the DB column never holds a
// surprise string. Validate() rejects unknown values up front so this
// helper only papers over the empty-string default the form may emit
// before client-side defaults bind.
func (r BeaconRequest) normalizedFormat() string {
	switch r.PositionFormat {
	case "compressed", "uncompressed", "mic_e":
		return r.PositionFormat
	default:
		return "compressed"
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/webapi/dto/ -count=1 -v`
Expected: PASS for the new test AND any pre-existing tests in the package.

If a pre-existing DTO test (e.g. in `igate_test.go`, `messages_test.go`) builds a `BeaconRequest` literal with the old `Compress` field, update it. Grep first: `grep -n 'Compress' pkg/webapi/dto/*.go`.

- [ ] **Step 5: Whole-build sanity**

Run: `go build ./...`
Expected: PASS.

Run: `go test ./pkg/webapi/... -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/webapi/dto/beacon.go pkg/webapi/dto/beacon_test.go
git commit -m "dto: validate beacon position_format and ambiguity per APRS101"
```

---

### Task 9: Regenerate swagger + TypeScript client

The Swagger spec is generated from the DTO struct tags by `swag`, and the TS client comes from the spec. CI's `docs-check` and `api-client-check` gate on these being committed; regenerate now so the rest of Phase 1 (the UI) can rely on the typed shape.

**Files:**
- Regenerate: `pkg/webapi/docs/gen/swagger.json`
- Regenerate: `pkg/webapi/docs/gen/swagger.yaml`
- Regenerate: `web/src/api/generated/api.d.ts`

- [ ] **Step 1: Regenerate the OpenAPI spec**

Run: `make docs`
Expected: PASS. Writes new swagger.json and swagger.yaml.

If swag is not installed, install the pinned version (see memory `feedback_swag_pinned_version`): `go install github.com/swaggo/swag/cmd/swag@v1.16.4`.

- [ ] **Step 2: Verify the spec changed in the right places**

Run: `git diff pkg/webapi/docs/gen/swagger.yaml | head -60`
Expected: shows `compress` removed, `position_format` added, in the beacon-request and beacon-response definitions. No other unexpected drift (if there is, stop and investigate).

- [ ] **Step 3: Regenerate the TS client**

Run: `make api-client`
Expected: PASS. Writes a new `web/src/api/generated/api.d.ts`.

- [ ] **Step 4: Verify the TS client matches**

Run: `git diff web/src/api/generated/api.d.ts | head -30`
Expected: `compress?: boolean` removed, `position_format?: string` added, in BeaconRequest/BeaconResponse definitions.

- [ ] **Step 5: CI gate checks**

Run: `make docs-check api-client-check`
Expected: PASS for both.

- [ ] **Step 6: Commit**

```bash
git add pkg/webapi/docs/gen/swagger.json pkg/webapi/docs/gen/swagger.yaml \
        web/src/api/generated/api.d.ts
git commit -m "webapi: regenerate swagger + TS client for position_format"
```

---

### Task 10: Beacons.svelte — form radio, ambiguity sub-block, Mic-E gating

The user-facing change. Add the new "Position report format" section to the modal between the existing Symbol field and the Position-source field. Wire ambiguity. Show the Mic-E "coming next release" warning and disable Save while it's selected.

**Files:**
- Modify: `web/src/routes/Beacons.svelte`

- [ ] **Step 1: Add format + ambiguity state to the form snapshot**

In `web/src/routes/Beacons.svelte`, locate the `let form = $state({...})` block at L92. Add two new fields after `overlay: ''` (around L96):

```js
    position_format: 'compressed', ambiguity: 0,
```

The resulting `form` initializer should look like:

```js
  let form = $state({
    type: 'position', object_name: '',
    channel: '', callsign: '', callsign_override: false,
    destination: 'APGRWO', path: 'WIDE1-1,WIDE2-1',
    symbol_table: '/', symbol: '-', overlay: '',
    position_format: 'compressed', ambiguity: 0,
    pos_source: 'gps', latitude: '', longitude: '', alt_ft: '',
    comment: '', interval: '600', send_to_aprs_is: false, enabled: true,
  });
```

- [ ] **Step 2: Reset format + ambiguity on openCreate**

In `openCreate` (L257), after `form.overlay = '';` (L273), insert:

```js
    form.position_format = 'compressed';
    form.ambiguity = 0;
```

- [ ] **Step 3: Hydrate format + ambiguity on openEdit**

In `openEdit` (L287), the existing `Object.assign(form, row, { ... })` already copies arbitrary row fields onto the form by spread, but we want explicit handling of the two fields so legacy rows (which may carry `null` or undefined for `ambiguity`) get sane defaults. After the `overlay: row.overlay || '',` line (L305) inside the `Object.assign` call, insert:

```js
        position_format: row.position_format || 'compressed',
        ambiguity: row.ambiguity ?? 0,
```

- [ ] **Step 4: Add a Save-disable gate for Mic-E + a derived "show format" flag**

In `web/src/routes/Beacons.svelte`, locate the `let saveBlocked = $derived(!!txBlock && !txBlockAllowsSave);` line at L137. Replace it with:

```js
  // Phase 1 gate: the API rejects mic_e writes. Mirror the rejection in
  // the form so the operator sees the Save-disable before the network
  // round trip — and so they can't accidentally fire a 400 every time
  // they tab through the radio.
  let saveBlocked = $derived(
    (!!txBlock && !txBlockAllowsSave) ||
    (form.position_format === 'mic_e' && form.type === 'position'),
  );
  // The format radio + ambiguity sub-block apply only to types that
  // carry an APRS101 ch 6/9/10 position field. Object and custom
  // beacons hide the whole section. This matches the spec §3.1 rule
  // (radio visible for type ∈ {position, tracker, igate}); the
  // beacons UI today only exposes 'position' and 'object', so the
  // practical gate is "show on position".
  let showFormat = $derived(form.type === 'position');
  let showAmbiguity = $derived(
    showFormat &&
    (form.position_format === 'uncompressed' || form.position_format === 'mic_e'),
  );
  let useAmbiguity = $derived(form.ambiguity > 0);
```

- [ ] **Step 5: Insert the new section into the modal markup**

In `web/src/routes/Beacons.svelte`, find the closing `</FormField>` of the Symbol section (the `Symbol` FormField runs L703–L720). Immediately after that FormField (so the new section sits between Symbol and Comment), insert the markup below. Use the existing `FormField` / `RadioGroup` / `Radio` / `Checkbox` patterns visible higher up in the file — they're already imported.

```svelte
      {#if showFormat}
        <FormField label="Position report format" id="bcn-pos-fmt"
          hint="How this beacon's position is encoded on the air. Compressed is shortest and most precise. Uncompressed and Mic-E can carry deliberately coarse positions via ambiguity.">
          <RadioGroup bind:value={form.position_format}>
            <div class="pos-source-row">
              <Radio value="compressed" label="Compressed (highest precision)" />
              <Radio value="uncompressed" label="Uncompressed (standard precision)" />
              <Radio value="mic_e" label="Mic-E (most efficient)" />
            </div>
          </RadioGroup>
          {#if form.position_format === 'mic_e'}
            <div class="mic-e-coming-soon" role="alert">
              Mic-E TX support is shipping in the next release. Save with
              Uncompressed for now.
            </div>
          {/if}
        </FormField>
        {#if showAmbiguity}
          <FormField label="Position ambiguity" id="bcn-ambiguity"
            hint="Blank trailing digits so the position is published deliberately coarsely. Useful for QTH privacy or group meetups.">
            <label class="callsign-override-label" for="bcn-amb-toggle">
              <Checkbox id="bcn-amb-toggle" checked={useAmbiguity}
                onCheckedChange={(v) => { form.ambiguity = v ? Math.max(1, form.ambiguity) : 0; }} />
              <span>Use position ambiguity</span>
            </label>
            {#if useAmbiguity}
              <select bind:value={form.ambiguity} class="bcn-amb-select">
                <option value={1}>Block ({altUnit === 'feet' ? '~600 ft' : '~185 m'})</option>
                <option value={2}>Neighborhood ({altUnit === 'feet' ? '~1 mi' : '~1.85 km'})</option>
                <option value={3}>Town ({altUnit === 'feet' ? '~11 mi' : '~18.5 km'})</option>
                <option value={4}>Region ({altUnit === 'feet' ? '~69 mi' : '~111 km'})</option>
              </select>
            {/if}
          </FormField>
        {/if}
      {/if}
```

Note on the ambiguity dropdown numeric values: Svelte 5 `bind:value` on a `<select>` with `<option value={N}>` preserves the number type (unlike string-valued options). Verify in Step 8.

Note on the Checkbox `onCheckedChange`: this matches the pattern documented in memory `feedback_master_toggle_autosave` — flipping the checkbox writes to the form immediately, no separate apply step.

Add two new CSS rules to the `<style>` block at the bottom of the file (anywhere inside the existing `<style>` is fine; the file isn't strict about ordering):

```css
  .mic-e-coming-soon {
    margin-top: 0.5rem;
    padding: 0.5rem 0.75rem;
    border-left: 3px solid var(--color-warning, #c47900);
    background: rgba(196, 121, 0, 0.08);
    font-size: 0.9rem;
    line-height: 1.4;
  }
  .bcn-amb-select {
    margin-top: 0.5rem;
    padding: 0.4rem 0.5rem;
    border: 1px solid var(--color-border, #444);
    border-radius: 4px;
    background: var(--color-input-bg, #1e1e1e);
    color: inherit;
    width: 100%;
    max-width: 360px;
  }
```

- [ ] **Step 6: Hide the Destination input when Mic-E is selected**

Per spec §3.3: the Mic-E destination is auto-computed from lat/lon per transmission. The Destination input field at L696–L699 must be hidden when `form.position_format === 'mic_e'`, with a hint replacing it.

Wrap the existing Destination `<FormField>` block (L696–L699):

```svelte
      <FormField label="Destination" id="bcn-dest"
        hint="APRS tocall identifying the originating software. Leave as APGRWO unless you know you need to change it.">
        <Input id="bcn-dest" bind:value={form.destination} placeholder="APGRWO" />
      </FormField>
```

with:

```svelte
      {#if showFormat && form.position_format === 'mic_e'}
        <FormField label="Destination" id="bcn-dest"
          hint="Auto-computed from latitude for Mic-E. Not editable.">
          <div class="bcn-dest-autocomp">Auto-computed for Mic-E</div>
        </FormField>
      {:else}
        <FormField label="Destination" id="bcn-dest"
          hint="APRS tocall identifying the originating software. Leave as APGRWO unless you know you need to change it.">
          <Input id="bcn-dest" bind:value={form.destination} placeholder="APGRWO" />
        </FormField>
      {/if}
```

And add the CSS rule to the `<style>` block alongside the others added in Step 5:

```css
  .bcn-dest-autocomp {
    padding: 0.4rem 0.6rem;
    background: var(--color-input-disabled-bg, #2a2a2a);
    border: 1px dashed var(--color-border, #444);
    border-radius: 4px;
    color: var(--color-text-muted, #aaa);
    font-style: italic;
  }
```

Spec §3.3 also mentions hiding the PHG fieldset on Mic-E. The Beacons UI today does NOT surface PHG fields in the form (verified via grep at plan-writing time — only altitude-`height` shows up as `hint` text, not a PHG control). No work needed; this is a no-op until PHG is added to the UI.

- [ ] **Step 7: Strip the Mic-E rejection from the post body (defensive)**

In `handleSave` (L318), the existing `data = { ...form, ... }` (L381) includes `position_format` automatically via the spread. No extra change needed — but verify by reading the function once. The DTO will reject `mic_e` with a 400 even if the Save-disable misses, so the path is doubly safe.

- [ ] **Step 8: Apply Save-disabled state to the modal button (already wired)**

The modal's Save button at L791 already binds `disabled={saveBlocked}`. We extended `saveBlocked` in Step 4, so the Mic-E case is already covered. No extra change needed; verify by reading L786–L794 once.

- [ ] **Step 9: Smoke the build**

Run: `make web` (or, if the project doesn't define that target: `cd web && npm run build`)
Expected: PASS. No TypeScript / Svelte compilation errors.

Look at the diff: `git diff web/src/routes/Beacons.svelte | head -120`
Expected: changes confined to the three regions (`form` initializer, `openCreate`/`openEdit`, derived state, modal markup, CSS). No accidental edits elsewhere.

- [ ] **Step 10: Commit**

```bash
git add web/src/routes/Beacons.svelte
git commit -m "ui/beacons: position report format radio + ambiguity controls"
```

---

### Task 11: Playwright smoke test

Confirm the form actually exposes the new controls in a real browser, that Mic-E disables Save, and that the ambiguity sub-block follows the format radio.

**Files:**
- Look for existing Playwright tests: `find web -name '*.spec.ts' -o -name '*.spec.js' 2>/dev/null` and `find . -maxdepth 4 -name 'playwright.config.*' 2>/dev/null` to see the project's Playwright layout. If there's an existing pattern (e.g. `web/tests/e2e/beacons.spec.ts`), follow it; if not, this task is the place to set one up.

- [ ] **Step 1: Identify the project's Playwright surface**

Run: `find . -maxdepth 4 -type f \( -name 'playwright.config.*' -o -path '*tests/e2e/*' \) 2>/dev/null | head -20`

If a Playwright surface exists, use it. If it does not, write a minimal headed smoke that the operator runs locally — for graywolf today there's no committed E2E suite, so the practical outcome of this task is a manual operator check. In that case, add the manual smoke as a short list to `pkg/releasenotes/notes.yaml` body? No — manual smokes don't belong in release notes. Instead, document the smoke in the PR description.

- [ ] **Step 2: Smoke as a checklist (operator-run; commit nothing)**

Before merge, run the local app and confirm:

1. Open Beacons page → New beacon → modal opens.
2. Type radio defaults to "Position"; format radio shows "Compressed" selected; no ambiguity sub-block.
3. Click "Uncompressed" → format radio updates; ambiguity sub-block appears with the checkbox unchecked.
4. Check "Use position ambiguity" → dropdown appears, defaulting to "Block".
5. Click "Mic-E" → inline warning appears, Save button disables, ambiguity sub-block stays visible (Mic-E supports it).
6. Click back to "Compressed" → warning disappears, ambiguity sub-block hides, Save enables.
7. Save an Uncompressed + Block beacon → row appears in the list; reopen → format and ambiguity round-trip.
8. Switch type to "Object" → format radio + ambiguity hide entirely.

If any step fails, fix the underlying bug before merging; the bug is in Task 10, not here.

- [ ] **Step 3: No commit**

Manual smokes don't produce committed artifacts. If at this point you've discovered a Playwright suite that should host an automated smoke, write one and commit it; otherwise move on.

---

### Task 12: Phase 1 release note

The release-note style is strict ASCII, plain English, no internals. The bump targets (`make bump-point`) refuse to run if no entry exists for the new version (see CLAUDE.md). We prepend to the top of `pkg/releasenotes/notes.yaml`.

**Files:**
- Modify: `pkg/releasenotes/notes.yaml`

- [ ] **Step 1: Pre-flight — confirm the next version**

Run: `cat VERSION`
Expected: `0.13.11` (or whatever's current at merge time). Next patch is +1 on the third digit.

- [ ] **Step 2: Insert the release note**

Edit `pkg/releasenotes/notes.yaml`. Immediately after the header comment block and before the first existing `- version:` entry (currently `"0.13.11"`), insert:

```yaml
- version: "0.13.12"
  date: "YYYY-MM-DD"
  style: info
  schema_version: 1
  link: "#/beacons"
  title: "Beacons: pick the position report format and use position ambiguity"
  body: |
    You can now pick the format each beacon uses on the air -- Compressed
    is the densest and most precise, Uncompressed is the standard form,
    and Mic-E is the most efficient. Mic-E is coming in the next release;
    pick it in the form to see it, but for now save with Compressed or
    Uncompressed.

    Uncompressed beacons can also use position ambiguity to publish a
    deliberately coarse position. Pick Block, Neighborhood, Town, or
    Region in the beacon form. Useful for home QTH privacy or for group
    meetups where address-level precision isn't wanted. Existing beacons
    keep transmitting exactly the same packets they always did.
```

Replace `YYYY-MM-DD` with today's date in ISO form when committing. (You can leave it as the placeholder if the bump target rewrites it; check the latest entry in the file for the format the bump expects.)

Sanity-check: no emojis, no em dashes, no smart quotes. Re-read the body before committing.

- [ ] **Step 3: Compile the release-notes package (parses the YAML)**

Run: `go build ./pkg/releasenotes/...`
Expected: PASS.

Run: `go test ./pkg/releasenotes/...`
Expected: PASS. If a unit test parses every entry, it will catch a malformed entry.

- [ ] **Step 4: Commit**

```bash
git add pkg/releasenotes/notes.yaml
git commit -m "releasenotes: position format selector + ambiguity for v0.13.12"
```

---

### Task 13: Update the wiki for the new column + UI

Per project rule in CLAUDE.md: changes that add or rename a column, change a UI section, or shift "what file do I touch to change X" must be reflected in `docs/wiki/` in the same change.

**Files:**
- Modify: `docs/wiki/...` — the page that documents the beacons subsystem

- [ ] **Step 1: Locate the beacons wiki page**

Run: `ls docs/wiki/`
Then: `grep -l -i 'beacon' docs/wiki/*.md 2>/dev/null`

If there's a page that describes the beacons subsystem (likely something like `beacons.md` or a section inside a broader services page), update it. If the wiki has nothing on beacons, that's a wiki gap — write a short page covering: data model column list including `position_format` and `ambiguity`, the UI entry point (`web/src/routes/Beacons.svelte`), the encoder entry point (`pkg/beacon/builder.go` → `PositionInfo` / `CompressedPositionInfo`), and the migration history reference (currently `pkg/configstore/migrate.go` v23).

The "wiki-worthy" bar from CLAUDE.md is "anything a future session would otherwise have to grep for, read multiple files to assemble, or learn the hard way by breaking" — the new `position_format` column clears that bar.

- [ ] **Step 2: Edit (or create) the wiki page**

Write the smallest faithful update. Don't restate component internals (the code is authoritative for that); document topology and navigation. Specifically:

- Add `position_format` (and the existing `ambiguity` if not already listed) to the column list of the beacons table.
- Note that the column replaced the legacy `compress` bool in migration 23.
- Add the format radio and ambiguity sub-block to the UI surface description.
- If the page documents wire format choices, add the three options.

Do NOT duplicate the spec's "format-by-format wire behavior" — link to the spec or to APRS101 chapter references instead. The handbook at `docs/handbook/` is the operator-facing source; this wiki is for the next developer.

- [ ] **Step 3: Commit**

```bash
git add docs/wiki/
git commit -m "wiki: document beacons position_format column and UI controls"
```

---

### Task 14: Phase 1 final verification + PR

- [ ] **Step 1: Full project build + test**

Run: `go build ./...`
Expected: PASS.

Run: `go test ./... -count=1`
Expected: PASS. If anything outside `pkg/{configstore,beacon,webapi,app,aprs,releasenotes}` fails, investigate before merging.

Run: `cd web && npm run build`
Expected: PASS.

Run: `make docs-check api-client-check`
Expected: PASS for both.

- [ ] **Step 2: Push the branch and open a PR**

Branch policy per memory `feedback_commit_push`: "commit" implies push to remote. Push the branch and open the PR using `gh pr create`. PR body should:

- Reference the spec at `docs/superpowers/specs/2026-05-29-beacon-position-format-and-ambiguity-design.md`
- Close issue #146
- List the manual Playwright-style smoke from Task 11 in the test plan

- [ ] **Step 3: Wait for CI to be green, then merge**

Per release workflow in CLAUDE.md, Phase 1 ships as a patch release after merge — the bump target writes the tag and pushes; CI image build follows.

---

# Phase 2 — Mic-E encoder

Phase 2 lands after Phase 1 has merged. It adds `pkg/beacon/mice.go`, removes the API rejection, and re-enables the Save button when Mic-E is selected. It also overrides the AX.25 destination in the scheduler with the lat-derived Mic-E destination.

The risk model from the spec: Phase 1 already containing the format radio + Mic-E option + the rejection means Phase 2 changes are confined to (a) the new encoder file, (b) one line in the builder switch, (c) one new branch in the scheduler frame-build, (d) the DTO unblock, and (e) the UI un-disable.

---

### Task 15: Extend `aprs.EncodeMicEDest` to accept ambiguity

The existing destination encoder (`pkg/aprs/mice.go` L441) returns the six-char destination. Mic-E ambiguity is applied by replacing latitude digits in the destination with the K/L/Z space variants per APRS101 ch 10. The encoder grows an `ambiguity int` parameter.

**Files:**
- Modify: `pkg/aprs/mice.go` (signature at L441)
- Modify: `pkg/aprs/mice_test.go` (existing call sites + new ambiguity round-trip)

- [ ] **Step 1: Write the failing test**

In `pkg/aprs/mice_test.go`, append:

```go
// TestEncodeMicEDest_Ambiguity exercises the K/L/Z space-variant
// replacement on the latitude digits in the destination callsign per
// APRS101 ch 10 table 9.
func TestEncodeMicEDest_Ambiguity(t *testing.T) {
	// lat=37.4092 N, msgCode=0, no lon offset, west = true
	// Without ambiguity: encodes to a deterministic 6-char string.
	base := EncodeMicEDest(37.4092, 0, false, true, 0)
	if len(base) != 6 {
		t.Fatalf("unexpected length: %q", base)
	}
	for level := 1; level <= 4; level++ {
		got := EncodeMicEDest(37.4092, 0, false, true, level)
		if len(got) != 6 {
			t.Fatalf("level %d: unexpected length: %q", level, got)
		}
		// Compare digit-by-digit: the trailing N digits (counting from
		// the end of the lat-digits region) must be a K/L/Z variant
		// rather than the digit or P-variant. The exact mapping rules
		// live in the encoder.
		// Smoke: the blanked positions can never match the unblanked
		// destination at the same position.
		blankedCount := 0
		for i := 0; i < 6; i++ {
			if got[i] != base[i] {
				blankedCount++
			}
		}
		if blankedCount < 1 {
			t.Errorf("level %d: no positions changed vs unambiguous dest", level)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/aprs/ -run TestEncodeMicEDest_Ambiguity -count=1 -v`
Expected: FAIL — signature only takes 4 arguments.

- [ ] **Step 3: Grow the signature**

In `pkg/aprs/mice.go` at L441, replace the function definition with the new signature that accepts `ambiguity int`. Replace the digit-to-byte loop to substitute K (no-bit), L (north-bit), Z (offset-bit) per APRS101 ch 10 when the ambiguity level says to blank that position.

```go
func EncodeMicEDest(lat float64, msgCode int, lonOffset100 bool, westLong bool, ambiguity int) string {
	north := lat >= 0
	if lat < 0 {
		lat = -lat
	}
	deg := int(lat)
	minF := (lat - float64(deg)) * 60.0
	minWhole := int(minF)
	minFrac := int((minF - float64(minWhole)) * 100.0)
	digits := [6]int{deg / 10, deg % 10, minWhole / 10, minWhole % 10, minFrac / 10, minFrac % 10}
	bits := [3]bool{
		msgCode&0x4 != 0,
		msgCode&0x2 != 0,
		msgCode&0x1 != 0,
	}
	// Ambiguity replaces trailing latitude digits with the K/L/Z
	// space variants. The byte position index for each blanked digit
	// mirrors the formatLatitude blanking order so a destination
	// encoder produces a destination the parser side accepts at the
	// same level.
	//
	//   level 1 → blank digit at index 5 (1/100 minute)
	//   level 2 → also blank index 4   (1/10 minute)
	//   level 3 → also blank index 3   (units of minute)
	//   level 4 → also blank index 2   (tens of minute)
	//
	// The variant byte depends on which bit-bearing slot the position
	// is. Indexes 0..2 carry message bits; 3 carries N/S; 4 carries
	// the longitude-offset; 5 carries E/W. APRS101 ch 10 table 9:
	//   K = no message bit, no other high bit
	//   L = north-flag substitute (index 3)
	//   Z = longitude-offset substitute (index 4)
	//   (and there is no defined variant for index 5 because ambiguity
	//   never blanks the E/W indicator)
	blankFrom := 6 - ambiguity
	if blankFrom < 2 {
		blankFrom = 2
	}
	if ambiguity <= 0 {
		blankFrom = 6
	}
	out := make([]byte, 6)
	for i := 0; i < 6; i++ {
		d := byte(digits[i])
		var c byte
		highBit := false
		switch i {
		case 0, 1, 2:
			highBit = bits[i]
		case 3:
			highBit = north
		case 4:
			highBit = lonOffset100
		case 5:
			highBit = westLong
		}
		blanked := i >= blankFrom && i <= 5
		switch {
		case blanked && i == 4:
			c = 'Z'
		case blanked && i == 3:
			c = 'L'
		case blanked:
			c = 'K'
		case highBit:
			c = 'P' + d
		default:
			c = '0' + d
		}
		out[i] = c
	}
	return string(out)
}
```

Update the existing call sites in `pkg/aprs/mice_test.go` (L24, L64, L102 — `grep -n 'EncodeMicEDest(' pkg/aprs/mice_test.go`) to pass a trailing `0` argument:

```go
EncodeMicEDest(tc.lat, tc.msg, tc.offset, tc.west, 0)
```

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/aprs/ -run TestEncodeMicEDest -count=1 -v`
Expected: PASS — existing tests with appended `, 0` still pass, plus the new ambiguity test.

Run the full package: `go test ./pkg/aprs/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/aprs/mice.go pkg/aprs/mice_test.go
git commit -m "aprs: EncodeMicEDest accepts ambiguity level (K/L/Z variants)"
```

---

### Task 16: Implement `pkg/beacon/mice.go`

The Mic-E info-field encoder per APRS101 ch 10. Returns the info bytes and the lat-derived destination — the caller (Task 17) is responsible for swapping the destination at frame-build time.

**Files:**
- Create: `pkg/beacon/mice.go`
- Create: `pkg/beacon/mice_test.go`

- [ ] **Step 1: Write the failing test for the canonical example packet**

Create `pkg/beacon/mice_test.go`:

```go
package beacon

import (
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
)

// TestMicEPositionInfo_RoundTrip encodes a position via the new Mic-E
// encoder and parses it back via pkg/aprs to confirm the wire bytes
// survive a full encode → parse round trip. Mic-E requires the parser
// to see the destination callsign (it carries the latitude digits and
// hemisphere/offset bits), so we build a real AX.25 UI frame and call
// aprs.Parse rather than aprs.ParseInfo.
func TestMicEPositionInfo_RoundTrip(t *testing.T) {
	cases := []struct {
		name      string
		lat, lon  float64
		course    int
		speedKt   float64
		altM      float64
		messaging bool
		ambiguity int
		symTable  byte
		symCode   byte
		comment   string
	}{
		{"fixed_west", 37.4092, -122.1404, 0, 0, 0, false, 0, '/', '>', ""},
		{"fixed_east", 37.4092, 122.1404, 0, 0, 0, false, 0, '/', '>', ""},
		{"southern_west", -33.8688, -151.2093, 0, 0, 0, false, 0, '/', '>', ""},
		{"southern_east", -33.8688, 151.2093, 0, 0, 0, false, 0, '/', '>', ""},
		{"messaging_alt", 37.4092, -122.1404, 0, 0, 100, true, 0, '/', '>', ""},
		{"tracker_motion", 37.4092, -122.1404, 90, 30, 0, false, 0, '/', '>', ""},
		{"with_comment", 37.4092, -122.1404, 0, 0, 0, false, 0, '/', '>', "graywolf"},
		{"amb_block", 37.4092, -122.1404, 0, 0, 0, false, 1, '/', '>', ""},
		{"amb_neighborhood", 37.4092, -122.1404, 0, 0, 0, false, 2, '/', '>', ""},
		{"amb_town", 37.4092, -122.1404, 0, 0, 0, false, 3, '/', '>', ""},
		{"amb_region", 37.4092, -122.1404, 0, 0, 0, false, 4, '/', '>', ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := MicEPositionInfo(tc.lat, tc.lon, tc.course, tc.speedKt, tc.altM, tc.symTable, tc.symCode, tc.messaging, tc.ambiguity, tc.comment)
			destCall := MicEDestination(tc.lat, tc.lon, tc.ambiguity)
			destAddr, err := ax25.ParseAddress(destCall)
			if err != nil {
				t.Fatalf("ax25.ParseAddress(%q): %v", destCall, err)
			}
			srcAddr, _ := ax25.ParseAddress("N0CALL")
			frame, err := ax25.NewUIFrame(srcAddr, destAddr, nil, []byte(info))
			if err != nil {
				t.Fatalf("NewUIFrame: %v", err)
			}
			p, err := aprs.Parse(frame)
			if err != nil {
				t.Fatalf("aprs.Parse: %v (info=%q dest=%q)", err, info, destCall)
			}
			if p.MicE == nil || p.Position == nil {
				t.Fatalf("no Mic-E position parsed: %+v", p)
			}
			tolerance := []float64{0.001, 0.01, 0.1, 1.0, 10.0}[tc.ambiguity]
			if absf(p.Position.Latitude-tc.lat) > tolerance {
				t.Errorf("lat: got %v want %v (tol %v)", p.Position.Latitude, tc.lat, tolerance)
			}
			if absf(p.Position.Longitude-tc.lon) > tolerance {
				t.Errorf("lon: got %v want %v (tol %v)", p.Position.Longitude, tc.lon, tolerance)
			}
			if p.Position.Ambiguity != tc.ambiguity {
				t.Errorf("ambiguity: got %d want %d", p.Position.Ambiguity, tc.ambiguity)
			}
			if tc.course != 0 || tc.speedKt != 0 {
				if absf(float64(p.Position.Course-tc.course)) > 1 {
					t.Errorf("course: got %d want %d", p.Position.Course, tc.course)
				}
			}
			if tc.comment != "" && !strings.Contains(p.Comment, tc.comment) {
				t.Errorf("comment: got %q want substring %q", p.Comment, tc.comment)
			}
		})
	}
}

func absf(x float64) float64 { if x < 0 { return -x }; return x }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/beacon/ -run TestMicEPositionInfo -count=1 -v`
Expected: FAIL — `MicEPositionInfo` and `MicEDestination` undefined.

- [ ] **Step 3: Implement `pkg/beacon/mice.go`**

Write `pkg/beacon/mice.go`. This file implements APRS101 ch 10. The reference encoder lives only in the parser as a decoder, so the encode side is new code. Follow APRS101 directly — the spec covers the exact byte layout in §4.3.

```go
package beacon

import (
	"fmt"
	"math"
	"strings"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// MicEMessageOffDuty is the Mic-E message code emitted for every
// graywolf-originated Mic-E beacon. APRS101 ch 10 table 8 defines codes
// M0..M7; M0 ("Off Duty") is the most innocuous standard code and the
// spec deferred operator-selectable codes to a future plan.
const MicEMessageOffDuty = 0

// MicEDestination returns the 6-character AX.25 destination callsign
// for a Mic-E transmission from a position fix. Ambiguity blanks
// trailing latitude digits per APRS101 ch 10 table 9 (K/L/Z variants);
// it is applied identically to the uncompressed path so a Mic-E beacon
// and an uncompressed beacon at the same ambiguity level publish the
// same effective precision.
//
// The callsign is built by aprs.EncodeMicEDest; this wrapper exists
// because the wire side and the per-beacon "did I need the longitude
// offset?" choice are computed together with the info field.
func MicEDestination(lat, lon float64, ambiguity int) string {
	_, lonOffset100, westLong := micELonFields(lon)
	return aprs.EncodeMicEDest(lat, MicEMessageOffDuty, lonOffset100, westLong, ambiguity)
}

// MicEPositionInfo builds an APRS101 ch 10 Mic-E info field for the
// given fix. The caller is responsible for swapping the AX.25
// destination with MicEDestination at frame-build time; the bytes
// returned here cover only the info portion (data-type indicator
// through to the comment).
//
// course is degrees (0..360, 0 means "no course"); speedKt is knots;
// altM is metres (emitted as a 4-byte base-91 "}xxx" extension when
// non-zero per ch 10). ambiguity (0..4) blanks the trailing longitude
// minutes / hundredths positions with ASCII space; the destination's
// latitude blanking happens in MicEDestination.
//
// Mic-E preempts PHG — there is no PHG slot in the wire format. The
// caller drops PHG silently before invoking this encoder.
func MicEPositionInfo(lat, lon float64, course int, speedKt float64, altM float64, symbolTable, symbolCode byte, messaging bool, ambiguity int, comment string) string {
	if symbolTable == 0 {
		symbolTable = '/'
	}
	if symbolCode == 0 {
		symbolCode = '-'
	}
	// Data-type indicator: backtick = messaging-capable, apostrophe =
	// not messaging-capable (Yaesu/other styles in APRS101 ch 10).
	dti := byte('\'')
	if messaging {
		dti = '`'
	}

	lonDeg, lonOffset100, westLong := micELonFields(lon)
	// Encode longitude bytes per APRS101 ch 10:
	//   byte 0: degrees, with the hemisphere-offset rules applied
	//   byte 1: minutes (whole)
	//   byte 2: hundredths of minutes
	_ = westLong // already consumed by MicEDestination via lon sign
	lonBytes := micELonBytes(lonDeg, lon, lonOffset100, ambiguity)

	// Encode speed and course as APRS101 ch 10 three-byte triplet.
	csBytes := micESpeedCourse(speedKt, course)

	var sb strings.Builder
	sb.WriteByte(dti)
	sb.Write(lonBytes[:])
	sb.Write(csBytes[:])
	sb.WriteByte(symbolCode)
	sb.WriteByte(symbolTable)
	if altM != 0 {
		sb.WriteString(micEAltitudeExt(altM))
	}
	if comment != "" {
		sb.WriteString(comment)
	}
	return sb.String()
}

// micELonFields returns the longitude degree value (with the +100
// adjustment applied when the source longitude falls in the +/-100..180
// band), whether that adjustment was needed, and whether the longitude
// is west. Mirrors the parser-side logic in pkg/aprs/mice.go so a
// round-trip is byte-clean.
func micELonFields(lon float64) (deg int, offset100 bool, west bool) {
	west = lon < 0
	absLon := lon
	if absLon < 0 {
		absLon = -absLon
	}
	d := int(absLon)
	if d >= 100 {
		return d - 100, true, west
	}
	return d, false, west
}

// micELonBytes returns the 3-byte longitude info-field bytes per
// APRS101 ch 10. Ambiguity blanks the trailing positions in the
// minute / hundredths bytes with ASCII space 0x20.
func micELonBytes(degAdjusted int, lon float64, offset100 bool, ambiguity int) [3]byte {
	// Degrees byte: value + 28 unless < 10 degrees, in which case
	// + 28 + 80 (APRS101 ch 10 table 7).
	d := degAdjusted
	if !offset100 && d < 10 {
		d += 100
	}
	degByte := byte(d + 28)

	absLon := lon
	if absLon < 0 {
		absLon = -absLon
	}
	// Recover the minutes portion. The whole-degree integer may be
	// >= 100 (offset100 case); subtract that off to get the fractional
	// degrees from which minutes are computed.
	wholeDeg := int(absLon)
	if absLon-float64(wholeDeg) >= 0.999999 { // tiny float carry guard
		wholeDeg++
	}
	minF := (absLon - float64(wholeDeg)) * 60.0
	minWhole := int(minF)
	minFrac := int(math.Round((minF - float64(minWhole)) * 100.0))
	if minFrac >= 100 {
		minFrac = 99
	}

	// Minutes byte: value + 28; if value < 10, also + 60 to keep the
	// byte printable and outside the control range (APRS101 ch 10
	// table 7).
	m := minWhole
	if m < 10 {
		m += 60
	}
	minByte := byte(m + 28)
	hundByte := byte(minFrac + 28)

	// Ambiguity: APRS101 ch 10 says blank with ASCII space (0x20) the
	// matching positions in the longitude bytes; the parser side
	// (pkg/aprs/mice.go) accepts these.
	if ambiguity >= 1 {
		hundByte = 0x20
	}
	if ambiguity >= 2 {
		minByte = 0x20
	}
	if ambiguity >= 3 {
		// Town and Region don't have an extra longitude byte to blank
		// because the degree byte carries information shared with the
		// destination; per APRS101 we leave the degree byte alone and
		// rely on the destination-side K/L variants to carry the
		// blanking signal. No-op here.
	}
	return [3]byte{degByte, minByte, hundByte}
}

// micESpeedCourse encodes speed (knots) and course (degrees) into the
// three-byte triplet per APRS101 ch 10. The base-10 packing uses
// chr1 = SP/10 + 28, chr2 = (SP%10)*10 + DC/100 + 28, chr3 = DC%100 + 28
// where SP is speed in knots and DC is course in degrees.
func micESpeedCourse(speedKt float64, course int) [3]byte {
	sp := int(math.Round(speedKt))
	if sp < 0 {
		sp = 0
	}
	if sp > 799 {
		sp = 799
	}
	if course < 0 {
		course = 0
	}
	if course > 360 {
		course = course % 360
	}
	if course == 0 {
		course = 360
	}
	dc := course
	chr1 := byte(sp/10) + 28
	chr2 := byte((sp%10)*10+dc/100) + 28
	chr3 := byte(dc%100) + 28
	return [3]byte{chr1, chr2, chr3}
}

// micEAltitudeExt returns the 4-byte altitude extension "}xxx" where
// xxx is base-91-encoded metres + 10000 (APRS101 ch 10). The leading
// "}" is the type-byte marker the parser side scans for.
func micEAltitudeExt(altM float64) string {
	v := int(math.Round(altM)) + 10000
	if v < 0 {
		v = 0
	}
	if v > 91*91*91-1 {
		v = 91*91*91 - 1
	}
	out := make([]byte, 4)
	out[0] = '}'
	out[1] = byte(v/(91*91)) + 33
	out[2] = byte((v/91)%91) + 33
	out[3] = byte(v%91) + 33
	return string(out)
}

// Keep fmt-imported to avoid an unused-import linter complaint while
// the implementation is iterated on. Remove this line once the
// implementation is final.
var _ = fmt.Sprintf
```

This implementation follows the spec's structural guidance. Verify against `pkg/aprs/mice.go`'s decoder — if any byte position disagrees, the round-trip test will catch it.

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/beacon/ -run TestMicEPositionInfo -count=1 -v`
Expected: PASS.

If any sub-test fails, the issue is almost certainly a byte-offset disagreement with `pkg/aprs/mice.go`'s decoder. Read the decoder side carefully (`grep -n 'parseMice\|decodeMicE' pkg/aprs/mice.go`) and adjust the encoder to match. The decoder is the ground truth here.

- [ ] **Step 5: Remove the dummy fmt import line**

Once tests pass, delete the trailing `var _ = fmt.Sprintf` and remove `"fmt"` from the import block if it's no longer used.

- [ ] **Step 6: Commit**

```bash
git add pkg/beacon/mice.go pkg/beacon/mice_test.go
git commit -m "beacon: Mic-E position encoder per APRS101 ch 10"
```

---

### Task 17: Wire Mic-E into the builder + scheduler destination override

Phase 1's builder rejects `mic_e` with an error. Phase 2 replaces that rejection with the call to `MicEPositionInfo`. The scheduler frame-build also gets a one-line override that swaps the AX.25 destination with the Mic-E lat-derived destination.

**Files:**
- Modify: `pkg/beacon/builder.go`
- Modify: `pkg/beacon/scheduler.go`

- [ ] **Step 1: Wire `MicEPositionInfo` in the builder**

In `pkg/beacon/builder.go`, in the position/igate switch from Task 5:

```go
		case "mic_e":
			return "", fmt.Errorf("%s beacon: mic_e format not supported yet", b.Type)
```

replace with:

```go
		case "mic_e":
			if phg != "" {
				s.logger.Debug("PHG dropped from Mic-E beacon", "id", b.ID, "type", b.Type)
			}
			return MicEPositionInfo(lat, lon, 0, 0, altM, b.SymbolTable, b.SymbolCode, b.Messaging, b.Ambiguity, comment), nil
```

In the tracker switch:

```go
		case "mic_e":
			return "", fmt.Errorf("tracker beacon: mic_e format not supported yet")
```

replace with:

```go
		case "mic_e":
			return MicEPositionInfo(fix.Latitude, fix.Longitude, course, fix.Speed, altM, b.SymbolTable, b.SymbolCode, b.Messaging, b.Ambiguity, comment), nil
```

- [ ] **Step 2: Override the destination in the scheduler**

In `pkg/beacon/scheduler.go`, find the frame-build at L391:

```go
	frame, err := ax25.NewUIFrame(b.Source, b.Dest, b.Path, []byte(info))
```

Replace with:

```go
	dest := b.Dest
	if b.Format == "mic_e" {
		lat, lon := b.Lat, b.Lon
		if b.UseGps && s.cache != nil {
			if fix, ok := s.cache.Get(); ok {
				lat, lon = fix.Latitude, fix.Longitude
			}
		}
		micEDestCall := MicEDestination(lat, lon, b.Ambiguity)
		parsed, perr := ax25.ParseAddress(micEDestCall)
		if perr != nil {
			s.logger.Warn("beacon mic_e dest parse", "id", b.ID, "call", micEDestCall, "err", perr)
			if eo, ok := s.observer.(ErrorObserver); ok && eo != nil {
				eo.OnEncodeError(name)
			}
			return &SendNowError{Kind: SendNowErrorEncode, Err: perr}
		}
		dest = parsed
	}
	frame, err := ax25.NewUIFrame(b.Source, dest, b.Path, []byte(info))
```

Then update the TNC2-format call a few lines down (currently L426):

```go
		line := formatTNC2(b.Source, b.Dest, b.Path, info)
```

to use the resolved `dest`:

```go
		line := formatTNC2(b.Source, dest, b.Path, info)
```

- [ ] **Step 3: Run tests**

Run: `go test ./pkg/beacon/ -count=1`
Expected: PASS. The scheduler tests don't exercise Mic-E specifically yet, but they must keep passing (Compressed / Uncompressed remain default and unchanged).

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add pkg/beacon/builder.go pkg/beacon/scheduler.go
git commit -m "beacon: wire Mic-E encoder through builder and scheduler"
```

---

### Task 18: Remove the DTO Mic-E rejection

Drop the Phase 1 rejection so the API accepts `position_format = mic_e` now that the encoder is wired.

**Files:**
- Modify: `pkg/webapi/dto/beacon.go`
- Modify: `pkg/webapi/dto/beacon_test.go`

- [ ] **Step 1: Remove the rejection branch**

In `pkg/webapi/dto/beacon.go`, inside `Validate()`, remove the case:

```go
		case "mic_e":
			return fmt.Errorf("Mic-E TX is not yet supported; coming in the next release")
```

so the switch only carries `compressed`/`uncompressed`/`mic_e` as valid and the default branch catches unknown values.

- [ ] **Step 2: Update the test**

In `pkg/webapi/dto/beacon_test.go`, change the `mic_e_rejected_phase1` case to a Phase 2 accept:

```go
		{"mic_e_accepted", func(r *BeaconRequest) {
			r.PositionFormat = "mic_e"
			r.Ambiguity = 2
		}, ""},
```

- [ ] **Step 3: Run tests**

Run: `go test ./pkg/webapi/dto/ -count=1 -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add pkg/webapi/dto/beacon.go pkg/webapi/dto/beacon_test.go
git commit -m "dto: accept Mic-E position_format now that the encoder is wired"
```

---

### Task 19: Beacons.svelte — remove Mic-E "coming soon" UI block

The format radio stays. The inline warning goes away. The Save-disable Mic-E branch goes away.

**Files:**
- Modify: `web/src/routes/Beacons.svelte`

- [ ] **Step 1: Remove the inline warning**

In `web/src/routes/Beacons.svelte`, delete the block added in Phase 1 Task 10:

```svelte
          {#if form.position_format === 'mic_e'}
            <div class="mic-e-coming-soon" role="alert">
              Mic-E TX support is shipping in the next release. Save with
              Uncompressed for now.
            </div>
          {/if}
```

Also delete the matching `.mic-e-coming-soon` CSS rule.

- [ ] **Step 2: Remove the Mic-E saveBlocked branch**

In the `let saveBlocked = $derived(...)` block, remove the Mic-E condition:

```js
  let saveBlocked = $derived(
    (!!txBlock && !txBlockAllowsSave) ||
    (form.position_format === 'mic_e' && form.type === 'position'),
  );
```

restore to:

```js
  let saveBlocked = $derived(!!txBlock && !txBlockAllowsSave);
```

- [ ] **Step 3: Build**

Run: `cd web && npm run build`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add web/src/routes/Beacons.svelte
git commit -m "ui/beacons: remove Mic-E coming-soon gate, Mic-E is live"
```

---

### Task 20: Regenerate swagger + TS client for Phase 2 (validation message change)

The DTO error string changed — sometimes that propagates into the OpenAPI examples. Regenerate to be safe.

- [ ] **Step 1: Regen**

Run: `make docs api-client`
Expected: PASS.

- [ ] **Step 2: Diff check**

Run: `git diff pkg/webapi/docs/gen/ web/src/api/generated/ | head -40`
Expected: minimal or empty diff. The DTO struct shape didn't change, only the validation message; swag may or may not surface it.

- [ ] **Step 3: Commit (only if there's a diff)**

```bash
git add pkg/webapi/docs/gen/ web/src/api/generated/
git commit -m "webapi: regenerate swagger + TS client for Mic-E unblock"
```

If there's no diff, skip the commit.

---

### Task 21: Operator on-air verification

This is a manual gate, not a test. Per spec §7.2:

- Operator transmits a Mic-E beacon on a known channel.
- Captures with another graywolf and a non-graywolf receiver (APRSdroid, YAAC, or aprs.fi via an iGate).
- Confirms both decode it correctly: callsign, position within ambiguity tolerance, symbol, comment, course/speed if applicable.
- For tracker beacons: also confirm motion fields decode correctly across multiple fixes.

If decoding fails on any receiver, do NOT ship Phase 2. Diagnose the encode mismatch and iterate on `pkg/beacon/mice.go`.

- [ ] **Step 1: Set up a Mic-E beacon in the UI**

Edit a non-critical position beacon. Pick "Mic-E (most efficient)" in the format radio. Save.

- [ ] **Step 2: Confirm transmission**

Watch the Send Now button fire, see the info log entry, confirm the receiving side(s) decode.

- [ ] **Step 3: Iterate if needed**

Any byte mismatch lands as a follow-up commit on the Phase 2 branch.

---

### Task 22: Phase 2 release note

The spec calls this one a `cta` style — operators with tracker beacons should consider switching their format to Mic-E.

**Files:**
- Modify: `pkg/releasenotes/notes.yaml`

- [ ] **Step 1: Insert the entry**

Prepend to the top of `pkg/releasenotes/notes.yaml`, before the Phase 1 entry from Task 12:

```yaml
- version: "0.13.13"
  date: "YYYY-MM-DD"
  style: cta
  schema_version: 1
  link: "#/beacons"
  title: "Beacons: Mic-E transmit is now live"
  body: |
    Mic-E is the most efficient APRS position format on the air, and is
    the standard for mobile and tracker stations. If you have a tracker
    beacon, consider switching its format to Mic-E in the beacon form
    -- shorter packets, less channel time, same precision. Fixed-station
    beacons can use Mic-E too if you want; for most fixed stations
    Compressed is still a fine pick.
```

Replace `YYYY-MM-DD` with today's date.

- [ ] **Step 2: Compile**

Run: `go build ./pkg/releasenotes/...` and `go test ./pkg/releasenotes/...`
Expected: PASS for both.

- [ ] **Step 3: Commit**

```bash
git add pkg/releasenotes/notes.yaml
git commit -m "releasenotes: Mic-E TX live in v0.13.13"
```

---

### Task 23: Wiki update for Phase 2

The wiki page touched in Task 13 mentions Mic-E as "coming soon" or "supported via the format radio". Update it to remove that hedge and to point at `pkg/beacon/mice.go`.

**Files:**
- Modify: `docs/wiki/...` (whichever page Task 13 touched)

- [ ] **Step 1: Edit the wiki**

- Remove any "Mic-E TX is not yet supported" hedging.
- Add `pkg/beacon/mice.go` to the encoder file map.
- Note the scheduler-side destination override (one-line entry; "Mic-E swaps the AX.25 destination with the lat-derived destination at frame-build time").

- [ ] **Step 2: Commit**

```bash
git add docs/wiki/
git commit -m "wiki: Mic-E TX is live; update encoder file map"
```

---

### Task 24: Phase 2 verification + PR

- [ ] **Step 1: Full build and test**

Run: `go build ./...`
Expected: PASS.

Run: `go test ./... -count=1`
Expected: PASS.

Run: `cd web && npm run build`
Expected: PASS.

Run: `make docs-check api-client-check`
Expected: PASS.

- [ ] **Step 2: Push and open PR**

Push the Phase 2 branch. The PR body should:

- Reference the spec.
- Link to the Phase 1 PR.
- Describe the on-air verification from Task 21 (which receivers were used; sample packets).
- Note this is a `cta` release.

- [ ] **Step 3: Merge when CI is green, then run the bump target**

Per CLAUDE.md release workflow: `make bump-point` will pick up the release note from Task 22, push the tag, and CI builds the image.

---

## Phasing summary

| Phase | What ships | Independent? |
|--|--|--|
| Phase 1 (Tasks 1–14) | New `position_format` column + migration, encoder honors ambiguity for uncompressed, UI format radio + ambiguity sub-block, API rejects mic_e, UI shows "coming soon" warning + Save-disable on mic_e | Yes — ships as v0.13.12 |
| Phase 2 (Tasks 15–24) | `pkg/beacon/mice.go` encoder, scheduler destination override, DTO unblocks mic_e, UI un-disables Save, operator on-air verification, ships as v0.13.13 | Requires Phase 1 merged |

Each phase is one PR. Each phase has its own release-note entry.
