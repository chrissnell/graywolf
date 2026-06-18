# APRS-IS-Only Beaconing Implementation Plan (Option B: `send_path` enum)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a beacon transmit to APRS-IS only (no RF/TNC), so a station with no radio can still beacon, by replacing the boolean `send_to_aprs_is` with a single `send_path` enum: `rf` | `both` | `is_only`.

**Architecture:** Today every beacon is unconditionally submitted to RF (`s.sink.Submit`) and only *additionally* copied to APRS-IS when `send_to_aprs_is` is set, after RF succeeds. We replace that boolean with a `send_path` enum. The existing `send_to_aprs_is` column is migrated to `send_path` (`1`→`both`, `0`→`rf`) by a version-tracked schema migration and then dropped -- the same add-column / backfill / drop-column pattern migration 23 (`migrateBeaconPositionFormat`) already uses. A single enum makes the invalid "transmits nowhere" state unrepresentable.

**Tech Stack:** Go (beacon scheduler, configstore/GORM+SQLite migrations, webapi DTO + swag annotations), Svelte (web UI), generated OpenAPI spec (`make docs` / `make docs-api-html`) and TypeScript client (`make api-client`).

> **Decision history:** Option A (two booleans) was the first draft; the maintainer chose Option B (enum) because the migration framework makes the data conversion clean and the enum matches the Messages send-path UI. Third-party REST clients that send `send_to_aprs_is` will break -- the maintainer has explicitly accepted this. **Regenerating and committing the OpenAPI spec is a required, called-out task (Task 5).**

> **UI CHANGES: YES.** `web/src/routes/Beacons.svelte`:
> 1. The "Also send to APRS-IS" toggle becomes a `send_path` radio group (RF only / RF + APRS-IS / APRS-IS only), mirroring the existing `position_format` radio group in the same form.
> 2. The hard "Channel required" check is relaxed for `is_only` beacons.
> 3. The list-row badge gains an "APRS-IS only" variant.

---

## File Structure

| File | Responsibility | Action |
|------|----------------|--------|
| `pkg/configstore/models.go` | Replace `Beacon.SendToAPRSIS` with `Beacon.SendPath` | Modify |
| `pkg/configstore/migrate_beacon_send_path.go` | Migration 25: backfill `send_path` from `send_to_aprs_is`, drop old column | Create |
| `pkg/configstore/migrate.go` | Register migration 25 | Modify |
| `pkg/configstore/migrate_test.go` | Upgrade test: legacy `send_to_aprs_is` rows → `send_path` | Modify |
| `pkg/beacon/types.go` | `Config.SendPath` + `SendPath*` constants | Modify |
| `pkg/beacon/scheduler.go` | Derive RF/IS legs from `SendPath`; IS-only error handling | Modify |
| `pkg/beacon/scheduler_test.go` | Tests: rf / both / is_only / is_only-no-sink | Modify |
| `pkg/app/adapters.go` | Map `Beacon.SendPath` → `beacon.Config.SendPath` | Modify |
| `pkg/webapi/dto/beacon.go` | Replace `SendToAPRSIS` with `SendPath` (request/response/mappers/validate + swag enum tag) | Modify |
| `pkg/webapi/dto/beacon_test.go` | Validation + default-`rf` mapping tests | Create/Modify |
| `pkg/webapi/docs/gen/swagger.json` / `swagger.yaml` | Regenerated OpenAPI spec | Generated (Task 5) |
| `docs/handbook/openapi.json` / `openapi.yaml` | Published spec copy | Generated (Task 5) |
| `web/src/api/generated/api.d.ts` | Regenerated TS client | Generated (Task 5) |
| `web/src/routes/Beacons.svelte` | `send_path` radio group, channel validation, badge | Modify |
| `docs/handbook/beacons.html` | Operator docs for IS-only beacons | Modify |
| `docs/wiki/system-topology.md` | Note beacons now work on an IS-only station | Modify |

---

### Task 1: Configstore model + migration 25

**Files:**
- Modify: `pkg/configstore/models.go` (`Beacon` struct, the `SendToAPRSIS` line ~670)
- Create: `pkg/configstore/migrate_beacon_send_path.go`
- Modify: `pkg/configstore/migrate.go` (`schemaMigrations` slice, after version 24 ~line 221)
- Modify: `pkg/configstore/migrate_test.go`

- [ ] **Step 1: Write the failing migration test**

In `pkg/configstore/migrate_test.go`, add a new test modeled on `TestMigrateBeaconPositionFormatUpgrade`. Note the two gotchas copied from that test: the `CREATE TABLE` MUST be single-line (the sqlite migrator parses `sqlite_master.sql` line by line), and `seedStationConfig` blanks the first beacon's callsign, so select rows by `id`.

```go
// TestMigrateBeaconSendPathUpgrade stamps a database at user_version=24
// with legacy send_to_aprs_is values and confirms migration 25 backfills
// send_path ('both' where send_to_aprs_is=1, else 'rf') and drops the
// send_to_aprs_is column.
func TestMigrateBeaconSendPathUpgrade(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v24.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	// v24 beacons schema = current Beacon struct minus send_path, plus
	// the legacy send_to_aprs_is column. Single-line on purpose (see the
	// position_format test for the multi-line parsing gotcha). position_format
	// already exists at v24 (added by migration 23).
	stmts := []string{
		`CREATE TABLE beacons (id INTEGER PRIMARY KEY AUTOINCREMENT, type TEXT NOT NULL DEFAULT 'position', channel INTEGER NOT NULL DEFAULT 1, callsign TEXT NOT NULL, destination TEXT NOT NULL DEFAULT 'APGRWO', path TEXT NOT NULL DEFAULT 'WIDE1-1', use_gps NUMERIC DEFAULT 0, latitude REAL, longitude REAL, alt_ft REAL, ambiguity INTEGER NOT NULL DEFAULT 0, symbol_table TEXT NOT NULL DEFAULT '/', symbol TEXT NOT NULL DEFAULT '-', overlay TEXT, position_format TEXT NOT NULL DEFAULT 'compressed', messaging NUMERIC NOT NULL DEFAULT 0, comment TEXT, comment_cmd TEXT, custom_info TEXT, object_name TEXT, power INTEGER NOT NULL DEFAULT 0, height INTEGER NOT NULL DEFAULT 0, gain INTEGER NOT NULL DEFAULT 0, dir INTEGER NOT NULL DEFAULT 0, freq TEXT, tone TEXT, freq_offset TEXT, delay_seconds INTEGER NOT NULL DEFAULT 30, every_seconds INTEGER NOT NULL DEFAULT 1800, slot_seconds INTEGER NOT NULL DEFAULT -1, smart_beacon NUMERIC NOT NULL DEFAULT 0, sb_fast_speed INTEGER DEFAULT 60, sb_slow_speed INTEGER DEFAULT 5, sb_fast_rate INTEGER DEFAULT 60, sb_slow_rate INTEGER DEFAULT 1800, sb_turn_angle INTEGER DEFAULT 30, sb_turn_slope INTEGER DEFAULT 255, sb_min_turn_time INTEGER DEFAULT 5, send_to_aprs_is NUMERIC NOT NULL DEFAULT 0, enabled NUMERIC NOT NULL DEFAULT 1, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE i_gate_configs (id INTEGER PRIMARY KEY AUTOINCREMENT, enabled NUMERIC NOT NULL DEFAULT 0, server TEXT NOT NULL DEFAULT 'rotate.aprs2.net', port INTEGER NOT NULL DEFAULT 14580, callsign TEXT NOT NULL DEFAULT '', created_at DATETIME, updated_at DATETIME)`,
		`INSERT INTO beacons (callsign, send_to_aprs_is) VALUES ('RFON', 0)`,
		`INSERT INTO beacons (callsign, send_to_aprs_is) VALUES ('GATED', 1)`,
		`PRAGMA user_version = 24`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	var version int
	s.DB().Raw("PRAGMA user_version").Scan(&version)
	if version < 25 {
		t.Errorf("user_version = %d, want >= 25 after migration", version)
	}

	var spRF, spGated string
	if err := s.DB().Raw(`SELECT send_path FROM beacons WHERE id=1`).Scan(&spRF).Error; err != nil {
		t.Fatalf("read row 1 (RFON): %v", err)
	}
	if err := s.DB().Raw(`SELECT send_path FROM beacons WHERE id=2`).Scan(&spGated).Error; err != nil {
		t.Fatalf("read row 2 (GATED): %v", err)
	}
	if spRF != "rf" {
		t.Errorf("RFON send_path = %q, want %q", spRF, "rf")
	}
	if spGated != "both" {
		t.Errorf("GATED send_path = %q, want %q", spGated, "both")
	}

	// send_to_aprs_is must be gone.
	has, err := columnExists(s.DB(), "beacons", "send_to_aprs_is")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if has {
		t.Error("send_to_aprs_is column should have been dropped")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/configstore/ -run TestMigrateBeaconSendPathUpgrade -v`
Expected: FAIL — `send_path` column does not exist / migration 25 not registered.

- [ ] **Step 3: Swap the model field**

In `pkg/configstore/models.go`, find:

```go
	SendToAPRSIS  bool      `gorm:"column:send_to_aprs_is;not null;default:false" json:"send_to_aprs_is"`
	Enabled       bool      `gorm:"not null;default:true" json:"enabled"`
```

Replace with:

```go
	SendPath      string    `gorm:"column:send_path;not null;default:'rf'" json:"send_path"` // rf | both | is_only
	Enabled       bool      `gorm:"not null;default:true" json:"enabled"`
```

- [ ] **Step 4: Create the migration**

Create `pkg/configstore/migrate_beacon_send_path.go`:

```go
package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateBeaconSendPath converts the legacy boolean send_to_aprs_is into
// the send_path enum and drops the old column. AutoMigrate has already
// added send_path (default 'rf') from the struct tag, so every row reads
// 'rf' here; we only need to promote the gated beacons to 'both'.
// Idempotent: a no-op once send_to_aprs_is is gone. Mirrors
// migrateBeaconPositionFormat (migration 23).
func migrateBeaconSendPath(tx *gorm.DB) error {
	has, err := columnExists(tx, "beacons", "send_to_aprs_is")
	if err != nil {
		return fmt.Errorf("probe beacons.send_to_aprs_is: %w", err)
	}
	if !has {
		return nil
	}
	if err := tx.Exec(
		`UPDATE beacons SET send_path = 'both' WHERE send_to_aprs_is = 1`,
	).Error; err != nil {
		return fmt.Errorf("backfill send_path: %w", err)
	}
	if err := tx.Exec(`ALTER TABLE beacons DROP COLUMN send_to_aprs_is`).Error; err != nil {
		return fmt.Errorf("drop send_to_aprs_is: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Register the migration**

In `pkg/configstore/migrate.go`, find the end of the `schemaMigrations` slice:

```go
	{version: 24, name: "kiss_gate_tx_to_is", phase: postAutoMigrate, run: migrateKissGateTxToIs},
}
```

Replace with:

```go
	{version: 24, name: "kiss_gate_tx_to_is", phase: postAutoMigrate, run: migrateKissGateTxToIs},
	{version: 25, name: "beacon_send_path", phase: postAutoMigrate, run: migrateBeaconSendPath},
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./pkg/configstore/ -run TestMigrateBeaconSendPathUpgrade -v`
Expected: PASS.

- [ ] **Step 7: Run the full configstore suite**

Run: `go test ./pkg/configstore/...`
Expected: PASS. If any test references `SendToAPRSIS` on the struct, update it to `SendPath` (grep `SendToAPRSIS` under `pkg/configstore`).

- [ ] **Step 8: Commit**

```bash
git add pkg/configstore/models.go pkg/configstore/migrate_beacon_send_path.go pkg/configstore/migrate.go pkg/configstore/migrate_test.go
git commit -m "Replace beacon send_to_aprs_is boolean with send_path enum"
```

---

### Task 2: Beacon `Config.SendPath` + scheduler logic

**Files:**
- Modify: `pkg/beacon/types.go` (`Config` struct ~line 53)
- Modify: `pkg/beacon/scheduler.go` (`sendBeaconWith` ~lines 426-456)
- Modify: `pkg/beacon/scheduler_test.go`

- [ ] **Step 1: Write the failing tests**

In `pkg/beacon/scheduler_test.go`, add a fake IS sink (near the other test fakes at the top):

```go
type fakeISSink struct {
	mu    sync.Mutex
	lines []string
	err   error
}

func (f *fakeISSink) SendLine(line string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.lines = append(f.lines, line)
	return nil
}

func (f *fakeISSink) Lines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.lines...)
}

func mustParse(s string) ax25.Address {
	a, err := ax25.ParseAddress(s)
	if err != nil {
		panic(err)
	}
	return a
}

func mkPathBeacon(sendPath string) Config {
	return Config{
		ID:       7,
		Type:     TypePosition,
		Channel:  0,
		Source:   mustParse("N0CALL-9"),
		Dest:     mustParse("APGRWO"),
		Path:     []ax25.Address{mustParse("WIDE1-1")},
		Slot:     -1,
		Lat:      37.7749,
		Lon:      -122.4194,
		Format:   "compressed",
		SendPath: sendPath,
	}
}
```

And the behavior tests:

```go
func TestSendBeacon_PathRF(t *testing.T) {
	sink := newMockSink(1)
	is := &fakeISSink{}
	logger := slog.New(slog.NewTextHandler(logSink{}, nil))
	s, err := New(Options{Sink: sink, ISSink: is, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	s.sendBeacon(context.Background(), mkPathBeacon(SendPathRF))
	if got := len(sink.Frames()); got != 1 {
		t.Fatalf("RF frames = %d, want 1", got)
	}
	if got := len(is.Lines()); got != 0 {
		t.Fatalf("IS lines = %d, want 0", got)
	}
}

func TestSendBeacon_PathBoth(t *testing.T) {
	sink := newMockSink(1)
	is := &fakeISSink{}
	logger := slog.New(slog.NewTextHandler(logSink{}, nil))
	s, _ := New(Options{Sink: sink, ISSink: is, Logger: logger})
	s.sendBeacon(context.Background(), mkPathBeacon(SendPathBoth))
	if got := len(sink.Frames()); got != 1 {
		t.Fatalf("RF frames = %d, want 1", got)
	}
	if got := len(is.Lines()); got != 1 {
		t.Fatalf("IS lines = %d, want 1", got)
	}
}

func TestSendBeacon_PathISOnly(t *testing.T) {
	sink := newMockSink(0)
	is := &fakeISSink{}
	logger := slog.New(slog.NewTextHandler(logSink{}, nil))
	s, _ := New(Options{Sink: sink, ISSink: is, Logger: logger})
	s.sendBeacon(context.Background(), mkPathBeacon(SendPathISOnly))
	if got := len(sink.Frames()); got != 0 {
		t.Fatalf("RF frames = %d, want 0 (RF disabled)", got)
	}
	if got := len(is.Lines()); got != 1 {
		t.Fatalf("IS lines = %d, want 1", got)
	}
}

func TestSendBeaconImmediate_ISOnly_NoSink(t *testing.T) {
	sink := newMockSink(0)
	logger := slog.New(slog.NewTextHandler(logSink{}, nil))
	s, _ := New(Options{Sink: sink, Logger: logger}) // no ISSink
	err := s.sendBeaconImmediate(context.Background(), mkPathBeacon(SendPathISOnly))
	if err == nil {
		t.Fatal("expected error when is_only beacon has no APRS-IS sink")
	}
}
```

> Confirm the immediate entry point's exact name before writing the last test: grep `func (s \*Scheduler) sendBeaconImmediate` / `SendNow` in `scheduler.go`. Use whichever returns the `*SendNowError`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/beacon/ -run 'TestSendBeacon' -v`
Expected: FAIL — undefined `SendPathRF` / `Config.SendPath` (compile error).

- [ ] **Step 3: Add the field and constants**

In `pkg/beacon/types.go`, find:

```go
	Enabled      bool
	SendToAPRSIS bool // also send this beacon to APRS-IS (default off)
}
```

Replace with:

```go
	Enabled  bool
	// SendPath selects the transmission destination. Empty is treated as
	// SendPathRF for safety. SendPathISOnly skips RF entirely so a station
	// with no radio can beacon to APRS-IS.
	SendPath string
}

// Beacon send_path enum values (mirrors the send_path DB column).
const (
	SendPathRF     = "rf"      // RF/TNC only
	SendPathBoth   = "both"    // RF/TNC and APRS-IS
	SendPathISOnly = "is_only" // APRS-IS only, no RF
)
```

- [ ] **Step 4: Rewrite the send legs in `sendBeaconWith`**

In `pkg/beacon/scheduler.go`, replace the block from `src := txgovernor.SubmitSource{` (~line 426) through `return nil` (~line 456) — the current RF submit, observer call, and the `if b.SendToAPRSIS && s.isSink != nil` block — with:

```go
	src := txgovernor.SubmitSource{
		Kind:      "beacon",
		Detail:    fmt.Sprintf("%s/%d", b.Type, b.ID),
		Priority:  ax25.PriorityBeacon,
		SkipDedup: skipDedup,
	}

	// rf and is are derived from SendPath. Empty SendPath behaves as
	// SendPathRF (safe default for any unmigrated/zero value).
	sendRF := b.SendPath != SendPathISOnly
	sendIS := b.SendPath == SendPathBoth || b.SendPath == SendPathISOnly

	// RF/TNC leg. Skipped for is_only beacons so a radioless station can
	// still beacon.
	sent := false
	if sendRF {
		if err := s.sink.Submit(ctx, b.Channel, frame, src); err != nil {
			reason := classifySubmitError(err)
			s.logger.Warn("beacon submit", "id", b.ID, "name", name, "reason", reason, "err", err)
			if eo, ok := s.observer.(ErrorObserver); ok && eo != nil {
				eo.OnSubmitError(name, reason)
			}
			return &SendNowError{Kind: SendNowErrorSubmit, Err: err}
		}
		s.logger.Info("beacon sent", "id", b.ID, "type", b.Type, "channel", b.Channel, "info", info)
		sent = true
	}

	// APRS-IS leg. When RF also ran, an IS failure is non-fatal (the RF
	// copy already went out and an offline IS path is normal). For an
	// is_only beacon the IS leg IS the transmission, so a missing sink or
	// a send error is surfaced as a SendNowError.
	if sendIS {
		if s.isSink == nil {
			if !sendRF {
				return &SendNowError{Kind: SendNowErrorSubmit, Err: errors.New("aprs-is sink not configured for is_only beacon")}
			}
		} else {
			line := formatTNC2(b.Source, dest, b.Path, info)
			if err := s.isSink.SendLine(line); err != nil {
				s.logger.Warn("beacon aprs-is send", "id", b.ID, "name", name, "err", err)
				if !sendRF {
					return &SendNowError{Kind: SendNowErrorSubmit, Err: err}
				}
			} else {
				s.logger.Info("beacon sent to aprs-is", "id", b.ID, "line", line)
				sent = true
			}
		}
	}

	if sent && s.observer != nil {
		s.observer.OnBeaconSent(b.Type)
	}
	return nil
```

- [ ] **Step 5: Ensure `errors` is imported**

Check `pkg/beacon/scheduler.go` imports; if `"errors"` is missing, add it (or run `goimports -w pkg/beacon/scheduler.go`).

- [ ] **Step 6: Run the new tests**

Run: `go test ./pkg/beacon/ -run 'TestSendBeacon' -v`
Expected: PASS.

- [ ] **Step 7: Fix existing beacon tests for the new default**

Existing `Config{...}` literals omit `SendPath`, which is now `""` → treated as `rf` (RF on), so RF-asserting tests keep working. But any existing test that set `SendToAPRSIS: true` no longer compiles. Grep and convert:

Run: `grep -rn "SendToAPRSIS" pkg/beacon/`
For each hit, replace `SendToAPRSIS: true` with `SendPath: SendPathBoth` (and remove any `SendToAPRSIS: false`).

Run: `go test ./pkg/beacon/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add pkg/beacon/types.go pkg/beacon/scheduler.go pkg/beacon/scheduler_test.go
git commit -m "Drive beacon transmission from send_path enum"
```

---

### Task 3: App adapter mapping

**Files:**
- Modify: `pkg/app/adapters.go` (`beaconConfigFromStore`, `beacon.Config{...}` literal)

- [ ] **Step 1: Swap the mapping**

In `pkg/app/adapters.go`, find:

```go
		SendToAPRSIS:   b.SendToAPRSIS,
		Enabled:        b.Enabled,
```

Replace with:

```go
		SendPath:       b.SendPath,
		Enabled:        b.Enabled,
```

- [ ] **Step 2: Build and test**

Run: `go build ./pkg/app/... && go test ./pkg/app/...`
Expected: PASS. (Grep `SendToAPRSIS` under `pkg/app` if the build fails and convert any stragglers.)

- [ ] **Step 3: Commit**

```bash
git add pkg/app/adapters.go
git commit -m "Pass send_path from store to beacon scheduler"
```

---

### Task 4: DTO request/response, validation, swag enum annotation

**Files:**
- Modify: `pkg/webapi/dto/beacon.go`
- Create/Modify: `pkg/webapi/dto/beacon_test.go`

- [ ] **Step 1: Write the failing tests**

In `pkg/webapi/dto/beacon_test.go`:

```go
package dto

import "testing"

func TestBeaconRequest_SendPathDefaultsRF(t *testing.T) {
	r := BeaconRequest{Type: "position", Latitude: 1, Longitude: 1} // SendPath empty
	m := r.ToModel()
	if m.SendPath != "rf" {
		t.Fatalf("empty send_path should normalize to rf, got %q", m.SendPath)
	}
}

func TestBeaconRequest_SendPathISOnly(t *testing.T) {
	r := BeaconRequest{Type: "position", Latitude: 1, Longitude: 1, SendPath: "is_only"}
	m := r.ToModel()
	if m.SendPath != "is_only" {
		t.Fatalf("send_path = %q, want is_only", m.SendPath)
	}
}

func TestBeaconRequest_Validate_BadSendPath(t *testing.T) {
	r := BeaconRequest{Type: "custom", SendPath: "carrier-pigeon"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for unknown send_path")
	}
}

func TestBeaconRequest_Validate_ISOnlyOK(t *testing.T) {
	r := BeaconRequest{Type: "custom", SendPath: "is_only"}
	if err := r.Validate(); err != nil {
		t.Fatalf("is_only should validate, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/webapi/dto/ -run TestBeaconRequest -v`
Expected: FAIL — `BeaconRequest` has no field `SendPath` (compile error).

- [ ] **Step 3: Swap the request field (with swag enum tag)**

In `pkg/webapi/dto/beacon.go`, in `BeaconRequest`, find:

```go
	SendToAPRSIS   bool    `json:"send_to_aprs_is"`
	Enabled        bool    `json:"enabled"`
```

Replace with:

```go
	SendPath       string  `json:"send_path" enums:"rf,both,is_only" example:"rf"`
	Enabled        bool    `json:"enabled"`
```

- [ ] **Step 4: Add the normalize helper**

Near `normalizedFormat()` in `pkg/webapi/dto/beacon.go`, add:

```go
// normalizedSendPath returns the send_path value to persist. Empty
// (older client / unset form) becomes "rf" so the column never holds a
// surprise value. Validate() rejects unknown non-empty values up front.
func (r BeaconRequest) normalizedSendPath() string {
	switch r.SendPath {
	case "rf", "both", "is_only":
		return r.SendPath
	default:
		return "rf"
	}
}
```

- [ ] **Step 5: Set the field in `ToModel` and `ApplyToUpdate`**

In **both** `ToModel()` and `ApplyToUpdate()`, find each:

```go
		SendToAPRSIS:  r.SendToAPRSIS,
		Enabled:       r.Enabled,
```

Replace with:

```go
		SendPath:      r.normalizedSendPath(),
		Enabled:       r.Enabled,
```

- [ ] **Step 6: Swap the response field + mapping**

In `BeaconResponse`, find:

```go
	SendToAPRSIS  bool    `json:"send_to_aprs_is"`
	Enabled       bool    `json:"enabled"`
```

Replace with:

```go
	SendPath      string  `json:"send_path" enums:"rf,both,is_only" example:"rf"`
	Enabled       bool    `json:"enabled"`
```

In `BeaconFromModel`, find:

```go
		SendToAPRSIS:  m.SendToAPRSIS,
		Enabled:       m.Enabled,
```

Replace with:

```go
		SendPath:      m.SendPath,
		Enabled:       m.Enabled,
```

- [ ] **Step 7: Validate the enum**

In `func (r BeaconRequest) Validate()`, add at the top of the function body (before `switch r.Type`):

```go
	switch r.SendPath {
	case "", "rf", "both", "is_only":
		// "" normalizes to rf in normalizedSendPath
	default:
		return fmt.Errorf("send_path must be one of rf, both, is_only (got %q)", r.SendPath)
	}
```

- [ ] **Step 8: Run the tests to pass**

Run: `go test ./pkg/webapi/dto/ -run TestBeaconRequest -v`
Expected: PASS.

- [ ] **Step 9: Full webapi suite**

Run: `go test ./pkg/webapi/...`
Expected: PASS. (Grep `SendToAPRSIS` under `pkg/webapi` and convert any remaining references.)

- [ ] **Step 10: Commit**

```bash
git add pkg/webapi/dto/beacon.go pkg/webapi/dto/beacon_test.go
git commit -m "Add send_path enum to beacon API"
```

---

### Task 5: Regenerate the OpenAPI spec and TypeScript client (REQUIRED)

The spec is generated from the Go `dto.BeaconRequest` / `dto.BeaconResponse` structs by `swag` (driven from the `@ID`-annotated handlers in `pkg/webapi/beacons.go`). The committed artifacts must be regenerated so the swagger spec, the published handbook copy, and the TS client all reflect `send_path` instead of `send_to_aprs_is`. CI enforces this via `docs-check` and `api-client-check` (both wired into `make go-test`).

**Files (generated — do not hand-edit):**
- `pkg/webapi/docs/gen/swagger.json`, `pkg/webapi/docs/gen/swagger.yaml`
- `docs/handbook/openapi.json`, `docs/handbook/openapi.yaml`
- `web/src/api/generated/api.d.ts`

- [ ] **Step 1: Regenerate the swagger spec**

Run: `make docs`
Expected: rewrites `pkg/webapi/docs/gen/swagger.{json,yaml}`. Confirm `send_path` now appears and `send_to_aprs_is` is gone:

Run: `grep -c send_path pkg/webapi/docs/gen/swagger.json; grep -c send_to_aprs_is pkg/webapi/docs/gen/swagger.json`
Expected: first count > 0, second count = 0.

- [ ] **Step 2: Refresh the published handbook copy**

Run: `make docs-api-html`
Expected: copies the spec to `docs/handbook/openapi.{json,yaml}`.

- [ ] **Step 3: Regenerate the TypeScript client**

Run: `make api-client`
Expected: rewrites `web/src/api/generated/api.d.ts` with `send_path`. Confirm:

Run: `grep -c send_path web/src/api/generated/api.d.ts; grep -c send_to_aprs_is web/src/api/generated/api.d.ts`
Expected: first > 0, second = 0.

- [ ] **Step 4: Verify the drift checks pass**

Run: `make docs-check && make api-client-check`
Expected: both report "matches committed copy."

- [ ] **Step 5: Commit the generated artifacts**

```bash
git add pkg/webapi/docs/gen/swagger.json pkg/webapi/docs/gen/swagger.yaml docs/handbook/openapi.json docs/handbook/openapi.yaml web/src/api/generated/api.d.ts
git commit -m "Regenerate OpenAPI spec and API client for send_path"
```

---

### Task 6: Web UI — send_path radio group, channel validation, badge

**Files:**
- Modify: `web/src/routes/Beacons.svelte`

> Reuses the `RadioGroup` / `Radio` components already imported and used in this file for `position_format` (~line 742). No new imports needed.

- [ ] **Step 1: Default the form field**

In `web/src/routes/Beacons.svelte`, find (~line 100):

```js
    comment: '', interval: '600', send_to_aprs_is: false, enabled: true,
```

Replace with:

```js
    comment: '', interval: '600', send_path: 'rf', enabled: true,
```

In the reset block (~line 296), find:

```js
    form.send_to_aprs_is = false;
```

Replace with:

```js
    form.send_path = 'rf';
```

- [ ] **Step 2: Load `send_path` when editing**

In the edit-mapping block (~line 320, where `position_format: row.position_format || 'compressed'` is set), add alongside it:

```js
      send_path: row.send_path || 'rf',
```

- [ ] **Step 3: Replace the toggle with a radio group**

Find (~line 815):

```svelte
      <Toggle bind:checked={form.send_to_aprs_is} label="Also send to APRS-IS" />
```

Replace with:

```svelte
      <FormField label="Destination" id="bcn-send-path"
        hint="Where this beacon is transmitted. APRS-IS only needs no radio channel.">
        <RadioGroup bind:value={form.send_path}>
          <div class="pos-source-row">
            <Radio value="rf" label="RF only" />
            <Radio value="both" label="RF + APRS-IS" />
            <Radio value="is_only" label="APRS-IS only (no radio)" />
          </div>
        </RadioGroup>
      </FormField>
```

- [ ] **Step 4: Relax the channel-required check for is_only**

In `handleSave()`, find (~line 353):

```js
    const channelId = parseInt(form.channel);
    if (!Number.isFinite(channelId) || channelId <= 0) {
      toasts.error('Channel required');
      return;
    }
```

Replace with:

```js
    let channelId = parseInt(form.channel);
    if (form.send_path === 'is_only') {
      // APRS-IS-only beacon: no RF channel needed.
      if (!Number.isFinite(channelId) || channelId <= 0) channelId = 0;
    } else if (!Number.isFinite(channelId) || channelId <= 0) {
      toasts.error('Channel required');
      return;
    }
```

- [ ] **Step 5: Update the list badge**

Find (~line 527):

```svelte
            {#if b.send_to_aprs_is}
              <Badge variant="info">APRS-IS</Badge>
```

Replace with:

```svelte
            {#if b.send_path === 'is_only'}
              <Badge variant="info">APRS-IS only</Badge>
            {:else if b.send_path === 'both'}
              <Badge variant="info">APRS-IS</Badge>
```

(Leave the surrounding `{/if}` and markup intact.)

- [ ] **Step 6: Confirm no `send_to_aprs_is` references remain**

Run: `grep -n "send_to_aprs_is" web/src/routes/Beacons.svelte`
Expected: no output.

- [ ] **Step 7: Build the web bundle**

Run: `make web`
Expected: build succeeds, no references to the removed field.

- [ ] **Step 8: Commit**

```bash
git add web/src/routes/Beacons.svelte
git commit -m "Add send_path destination control to beacon form"
```

---

### Task 7: Docs — handbook + wiki

**Files:**
- Modify: `docs/handbook/beacons.html`
- Modify: `docs/wiki/system-topology.md`

- [ ] **Step 1: Handbook callout**

In `docs/handbook/beacons.html`, near the beacon destination/APRS-IS description, add (matching the file's existing callout markup):

```html
<div class="callout info">
  <span class="callout-icon">&#9432;</span>
  <div class="callout-body">
    <p>
      <strong>APRS-IS only (no radio)?</strong> Set this beacon's
      <em>Destination</em> to <em>APRS-IS only</em>. The beacon is gated
      straight to APRS-IS and no radio channel is required. Enable the
      iGate first so an APRS-IS connection exists.
    </p>
  </div>
</div>
```

- [ ] **Step 2: Wiki note**

In `docs/wiki/system-topology.md`, in the **IS-only station** section, append:

```markdown
Beacons can also run on an IS-only station: set a beacon's destination to
`is_only` and it is gated directly to APRS-IS with no RF channel. See
[`pkg/beacon/scheduler.go`](../../pkg/beacon/scheduler.go) (`sendBeaconWith`).
```

- [ ] **Step 3: Docs check**

Run: `make docs-check`
Expected: PASS (this is the spec drift check; the prose files don't affect it, but run it to be safe after Task 5).

- [ ] **Step 4: Commit**

```bash
git add docs/handbook/beacons.html docs/wiki/system-topology.md
git commit -m "Document APRS-IS-only beaconing"
```

---

### Task 8: Full verification

- [ ] **Step 1: Whole Go suite (includes docs-check + api-client-check)**

Run: `make go-test`
Expected: PASS.

- [ ] **Step 2: Build everything**

Run: `make build`
Expected: succeeds.

- [ ] **Step 3: Manual smoke (recommended)**

Start graywolf, enable the iGate, create a beacon with **Destination = APRS-IS only**, no channel. Confirm:
- The form saves without a "Channel required" error.
- The list shows the "APRS-IS only" badge.
- On the next interval, the log shows `beacon sent to aprs-is` and no `beacon sent` (RF) line.

---

## Self-Review

**Spec coverage:**
- "Beacon to APRS-IS, no RF" → Task 2 (`is_only` skips RF), Task 6 (UI).
- "Option B / enum" → Task 1 (`send_path` column + migration), Tasks 2/4 (enum end to end).
- "Use built-in migrations" → Task 1 (migration 25, same pattern as migration 23).
- "Update the OpenAPI spec" → **Task 5**, explicitly: regenerate `swagger.{json,yaml}`, `docs/handbook/openapi.{json,yaml}`, and `api.d.ts`, with greps proving `send_to_aprs_is` is gone and `docs-check`/`api-client-check` green.
- Third-party API break accepted → no compatibility shim; `send_to_aprs_is` removed from request and response.
- Edge: unknown `send_path` → rejected (Task 4 Step 7). Edge: `is_only` with no IS sink → `SendNowError` (Task 2). Edge: empty/unmigrated `send_path` → behaves as `rf` (Task 2 derive + Task 4 normalize).

**Placeholder scan:** None. Every code step shows full code; the two "grep first" notes are verification instructions over fully-specified edits.

**Type consistency:** `SendPath string` everywhere (model, Config, request, response). Constants `SendPathRF`/`SendPathBoth`/`SendPathISOnly` ("rf"/"both"/"is_only") match the DB defaults, the DTO `enums:` tag, the validation switch, and the Svelte radio values. Migration backfill: `send_to_aprs_is=1 → both`, else `rf`. Observer fires once via `sent`.

**Note for implementer:** `send_path` is a free-text column with values validated at the API layer (same approach as `position_format`); there is no DB-level CHECK constraint, consistent with the existing schema.
