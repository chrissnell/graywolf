package configstore

import (
	"context"
	"errors"
	"testing"

	"github.com/chrissnell/graywolf/pkg/callsign"
)

// TestGetStationConfigEmptyStore asserts the contract: an empty store
// returns a zero-value StationConfig with no error so callers can key
// off an empty Callsign without a separate nil check.
func TestGetStationConfigEmptyStore(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	got, err := s.GetStationConfig(ctx)
	if err != nil {
		t.Fatalf("GetStationConfig: %v", err)
	}
	if got.ID != 0 || got.Callsign != "" {
		t.Fatalf("expected zero-value StationConfig, got %+v", got)
	}
}

// TestUpsertStationConfigNormalizes asserts the store-boundary
// normalization contract: callers may pass raw user input and the
// stored value is trimmed + uppercased.
func TestUpsertStationConfigNormalizes(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.UpsertStationConfig(ctx, StationConfig{Callsign: "  ke7xyz-9  "}); err != nil {
		t.Fatalf("UpsertStationConfig: %v", err)
	}
	got, err := s.GetStationConfig(ctx)
	if err != nil {
		t.Fatalf("GetStationConfig: %v", err)
	}
	if got.Callsign != "KE7XYZ-9" {
		t.Fatalf("expected normalized KE7XYZ-9, got %q", got.Callsign)
	}

	// A second upsert with ID=0 must update in place.
	if err := s.UpsertStationConfig(ctx, StationConfig{Callsign: "w1aw"}); err != nil {
		t.Fatalf("second UpsertStationConfig: %v", err)
	}
	got2, err := s.GetStationConfig(ctx)
	if err != nil {
		t.Fatalf("GetStationConfig: %v", err)
	}
	if got2.ID != got.ID {
		t.Fatalf("expected ID preserved, got %d vs %d", got2.ID, got.ID)
	}
	if got2.Callsign != "W1AW" {
		t.Fatalf("expected W1AW, got %q", got2.Callsign)
	}

	var count int64
	if err := s.DB().Model(&StationConfig{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one station_configs row, got %d", count)
	}
}

// TestResolveStationCallsign covers the three documented outcomes:
// happy path returns the normalized callsign, empty returns
// ErrCallsignEmpty, N0CALL returns ErrCallsignN0Call. Each branch uses
// errors.Is so callers can reliably discriminate.
func TestResolveStationCallsign(t *testing.T) {
	ctx := context.Background()

	t.Run("happy", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.UpsertStationConfig(ctx, StationConfig{Callsign: "ke7xyz-9"}); err != nil {
			t.Fatalf("UpsertStationConfig: %v", err)
		}
		got, err := s.ResolveStationCallsign(ctx)
		if err != nil {
			t.Fatalf("ResolveStationCallsign: %v", err)
		}
		if got != "KE7XYZ-9" {
			t.Fatalf("expected KE7XYZ-9, got %q", got)
		}
	})

	t.Run("empty", func(t *testing.T) {
		s := newTestStore(t)
		// No row at all.
		_, err := s.ResolveStationCallsign(ctx)
		if !errors.Is(err, callsign.ErrCallsignEmpty) {
			t.Fatalf("expected ErrCallsignEmpty, got %v", err)
		}
		// Empty row (explicitly upserted).
		if err := s.UpsertStationConfig(ctx, StationConfig{Callsign: "   "}); err != nil {
			t.Fatalf("UpsertStationConfig: %v", err)
		}
		_, err = s.ResolveStationCallsign(ctx)
		if !errors.Is(err, callsign.ErrCallsignEmpty) {
			t.Fatalf("expected ErrCallsignEmpty for whitespace-only row, got %v", err)
		}
	})

	t.Run("n0call", func(t *testing.T) {
		s := newTestStore(t)
		if err := s.UpsertStationConfig(ctx, StationConfig{Callsign: "N0CALL-7"}); err != nil {
			t.Fatalf("UpsertStationConfig: %v", err)
		}
		_, err := s.ResolveStationCallsign(ctx)
		if !errors.Is(err, callsign.ErrCallsignN0Call) {
			t.Fatalf("expected ErrCallsignN0Call, got %v", err)
		}
	})
}

// TestSeedStationConfigEmptyDB asserts the base case: nothing to seed
// from means no row is inserted (user lands on a blank page at first
// boot).
func TestSeedStationConfigEmptyDB(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// The store's own Migrate already ran seedStationConfig against an
	// empty DB via OpenMemory. Confirm it was a no-op, and that calling
	// it again explicitly is still a no-op.
	if err := s.seedStationConfig(ctx); err != nil {
		t.Fatalf("seedStationConfig: %v", err)
	}
	got, err := s.GetStationConfig(ctx)
	if err != nil {
		t.Fatalf("GetStationConfig: %v", err)
	}
	if got.ID != 0 {
		t.Fatalf("expected no row on empty DB, got %+v", got)
	}
}

// TestSeedStationConfigIdempotent asserts the idempotency gate: once a
// StationConfig row exists, the seeder must not overwrite it even if
// the underlying candidates change.
func TestSeedStationConfigIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Plant a user-set row first.
	if err := s.UpsertStationConfig(ctx, StationConfig{Callsign: "VANITY-1"}); err != nil {
		t.Fatalf("UpsertStationConfig: %v", err)
	}
	// Now add a beacon that *would* be chosen if the seeder ran on an
	// empty DB.
	if err := s.CreateBeacon(ctx, &Beacon{
		Type: "position", Channel: 1, Callsign: "KE7XYZ-9",
		Path: "WIDE1-1", SymbolTable: "/", Symbol: ">",
		EverySeconds: 1800, Enabled: true,
	}); err != nil {
		t.Fatalf("CreateBeacon: %v", err)
	}
	if err := s.UpsertDigipeaterConfig(ctx, &DigipeaterConfig{MyCall: "KE7XYZ-9"}); err != nil {
		t.Fatalf("UpsertDigipeaterConfig: %v", err)
	}

	if err := s.seedStationConfig(ctx); err != nil {
		t.Fatalf("seedStationConfig: %v", err)
	}
	got, err := s.GetStationConfig(ctx)
	if err != nil {
		t.Fatalf("GetStationConfig: %v", err)
	}
	if got.Callsign != "VANITY-1" {
		t.Fatalf("seed clobbered user row: expected VANITY-1, got %q", got.Callsign)
	}
}

// TestSeedStationConfigMainBeaconHeuristic asserts priority 1: among
// multiple beacons, the one whose callsign matches the digi MyCall
// wins, not the first by ID.
func TestSeedStationConfigMainBeaconHeuristic(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	for _, call := range []string{"VANITY-1", "KE7XYZ-9", "MTNTOP-1"} {
		if err := s.CreateBeacon(ctx, &Beacon{
			Type: "position", Channel: 1, Callsign: call,
			Path: "WIDE1-1", SymbolTable: "/", Symbol: ">",
			EverySeconds: 1800, Enabled: true,
		}); err != nil {
			t.Fatalf("CreateBeacon %s: %v", call, err)
		}
	}
	if err := s.UpsertDigipeaterConfig(ctx, &DigipeaterConfig{MyCall: "KE7XYZ-9"}); err != nil {
		t.Fatalf("UpsertDigipeaterConfig: %v", err)
	}

	if err := s.seedStationConfig(ctx); err != nil {
		t.Fatalf("seedStationConfig: %v", err)
	}
	got, err := s.GetStationConfig(ctx)
	if err != nil {
		t.Fatalf("GetStationConfig: %v", err)
	}
	if got.Callsign != "KE7XYZ-9" {
		t.Fatalf("expected main-beacon heuristic to pick KE7XYZ-9, got %q", got.Callsign)
	}
}

// TestSeedStationConfigFallbackFirstBeacon: no beacon matches the digi
// or iGate callsign, so priority 2 picks the first beacon by ID.
func TestSeedStationConfigFallbackFirstBeacon(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	for _, call := range []string{"VANITY-1", "TACTIC-1"} {
		if err := s.CreateBeacon(ctx, &Beacon{
			Type: "position", Channel: 1, Callsign: call,
			Path: "WIDE1-1", SymbolTable: "/", Symbol: ">",
			EverySeconds: 1800, Enabled: true,
		}); err != nil {
			t.Fatalf("CreateBeacon %s: %v", call, err)
		}
	}
	// Digi callsign matches nothing in the beacon list.
	if err := s.UpsertDigipeaterConfig(ctx, &DigipeaterConfig{MyCall: "OTHER-1"}); err != nil {
		t.Fatalf("UpsertDigipeaterConfig: %v", err)
	}

	if err := s.seedStationConfig(ctx); err != nil {
		t.Fatalf("seedStationConfig: %v", err)
	}
	got, err := s.GetStationConfig(ctx)
	if err != nil {
		t.Fatalf("GetStationConfig: %v", err)
	}
	if got.Callsign != "VANITY-1" {
		t.Fatalf("expected first-by-ID VANITY-1, got %q", got.Callsign)
	}
}

// TestSeedStationConfigFallbackDigi: no beacons exist, so priority 3
// picks DigipeaterConfig.MyCall.
func TestSeedStationConfigFallbackDigi(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.UpsertDigipeaterConfig(ctx, &DigipeaterConfig{MyCall: "KE7XYZ-9"}); err != nil {
		t.Fatalf("UpsertDigipeaterConfig: %v", err)
	}

	if err := s.seedStationConfig(ctx); err != nil {
		t.Fatalf("seedStationConfig: %v", err)
	}
	got, err := s.GetStationConfig(ctx)
	if err != nil {
		t.Fatalf("GetStationConfig: %v", err)
	}
	if got.Callsign != "KE7XYZ-9" {
		t.Fatalf("expected digi fallback KE7XYZ-9, got %q", got.Callsign)
	}
}

// TestSeedStationConfigFallbackIGate: no beacons, no digi, but the
// i_gate_configs.callsign column carries a legacy value. Priority 4
// picks it up via raw SQL.
func TestSeedStationConfigFallbackIGate(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Plant an IGate row, then write the legacy callsign column
	// directly — UpsertIGateConfig would zero it on the way in.
	if err := s.UpsertIGateConfig(ctx, &IGateConfig{Enabled: false, Server: "x", Port: 1}); err != nil {
		t.Fatalf("UpsertIGateConfig: %v", err)
	}
	if err := s.DB().Exec("UPDATE i_gate_configs SET callsign = ?", "KE7XYZ-9").Error; err != nil {
		t.Fatalf("seed legacy callsign column: %v", err)
	}

	if err := s.seedStationConfig(ctx); err != nil {
		t.Fatalf("seedStationConfig: %v", err)
	}
	got, err := s.GetStationConfig(ctx)
	if err != nil {
		t.Fatalf("GetStationConfig: %v", err)
	}
	if got.Callsign != "KE7XYZ-9" {
		t.Fatalf("expected iGate fallback KE7XYZ-9, got %q", got.Callsign)
	}
}

// TestSeedStationConfigAllN0CALL: every candidate is N0CALL, so the
// seeder is a no-op and the user gets a blank page.
func TestSeedStationConfigAllN0CALL(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.CreateBeacon(ctx, &Beacon{
		Type: "position", Channel: 1, Callsign: "N0CALL",
		Path: "WIDE1-1", SymbolTable: "/", Symbol: ">",
		EverySeconds: 1800, Enabled: true,
	}); err != nil {
		t.Fatalf("CreateBeacon: %v", err)
	}
	if err := s.CreateBeacon(ctx, &Beacon{
		Type: "position", Channel: 1, Callsign: "n0call-7",
		Path: "WIDE1-1", SymbolTable: "/", Symbol: ">",
		EverySeconds: 1800, Enabled: true,
	}); err != nil {
		t.Fatalf("CreateBeacon: %v", err)
	}
	if err := s.UpsertDigipeaterConfig(ctx, &DigipeaterConfig{MyCall: "N0CALL"}); err != nil {
		t.Fatalf("UpsertDigipeaterConfig: %v", err)
	}
	if err := s.UpsertIGateConfig(ctx, &IGateConfig{Enabled: false, Server: "x", Port: 1}); err != nil {
		t.Fatalf("UpsertIGateConfig: %v", err)
	}
	if err := s.DB().Exec("UPDATE i_gate_configs SET callsign = ?", "N0CALL").Error; err != nil {
		t.Fatalf("seed legacy callsign: %v", err)
	}

	if err := s.seedStationConfig(ctx); err != nil {
		t.Fatalf("seedStationConfig: %v", err)
	}
	got, err := s.GetStationConfig(ctx)
	if err != nil {
		t.Fatalf("GetStationConfig: %v", err)
	}
	if got.ID != 0 {
		t.Fatalf("expected no seed on all-N0CALL DB, got %+v", got)
	}
}

// TestSeedStationConfigOverrideNormalization: after seeding, any
// beacon or digi row that equals the seeded callsign (case-insensitive)
// is cleared to ""; rows with other values are untouched. This is the
// migration-time normalization the plan calls for so the UI does not
// show redundant overrides on every row the operator had set to their
// station call.
func TestSeedStationConfigOverrideNormalization(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Beacon A matches the station call (mixed case) — should be cleared.
	if err := s.CreateBeacon(ctx, &Beacon{
		Type: "position", Channel: 1, Callsign: "ke7xyz-9",
		Path: "WIDE1-1", SymbolTable: "/", Symbol: ">",
		EverySeconds: 1800, Enabled: true,
	}); err != nil {
		t.Fatalf("CreateBeacon A: %v", err)
	}
	// Beacon B is a genuine vanity override — should be preserved.
	if err := s.CreateBeacon(ctx, &Beacon{
		Type: "position", Channel: 1, Callsign: "MTNTOP-1",
		Path: "WIDE1-1", SymbolTable: "/", Symbol: ">",
		EverySeconds: 1800, Enabled: true,
	}); err != nil {
		t.Fatalf("CreateBeacon B: %v", err)
	}
	// Digi matches — should be cleared.
	if err := s.UpsertDigipeaterConfig(ctx, &DigipeaterConfig{MyCall: "KE7XYZ-9"}); err != nil {
		t.Fatalf("UpsertDigipeaterConfig: %v", err)
	}

	if err := s.seedStationConfig(ctx); err != nil {
		t.Fatalf("seedStationConfig: %v", err)
	}

	got, err := s.GetStationConfig(ctx)
	if err != nil {
		t.Fatalf("GetStationConfig: %v", err)
	}
	if got.Callsign != "KE7XYZ-9" {
		t.Fatalf("expected seed KE7XYZ-9, got %q", got.Callsign)
	}

	beacons, err := s.ListBeacons(ctx)
	if err != nil {
		t.Fatalf("ListBeacons: %v", err)
	}
	if len(beacons) != 2 {
		t.Fatalf("expected 2 beacons, got %d", len(beacons))
	}
	for _, b := range beacons {
		switch b.ID {
		case 1:
			if b.Callsign != "" {
				t.Errorf("beacon 1: expected cleared (equal to station), got %q", b.Callsign)
			}
		case 2:
			if b.Callsign != "MTNTOP-1" {
				t.Errorf("beacon 2: expected preserved MTNTOP-1, got %q", b.Callsign)
			}
		default:
			t.Errorf("unexpected beacon id %d", b.ID)
		}
	}
	digi, err := s.GetDigipeaterConfig(ctx)
	if err != nil {
		t.Fatalf("GetDigipeaterConfig: %v", err)
	}
	if digi == nil {
		t.Fatalf("expected digipeater config to persist through normalization")
	}
	if digi.MyCall != "" {
		t.Errorf("digi MyCall: expected cleared (equal to station), got %q", digi.MyCall)
	}
}

// TestUpsertIGateConfigZeroesCallsignAndPasscode: the orphaned columns
// that remain in the schema for downgrade-safety are zeroed on every
// UpsertIGateConfig, even when the caller provides no hint. Verifies
// both the fresh-save case and the overwrite case (row already holds
// non-empty values from a pre-Phase-2 install).
func TestUpsertIGateConfigZeroesCallsignAndPasscode(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Fresh save: caller can't even express callsign/passcode on the
	// struct anymore, but the columns must be zeroed regardless.
	if err := s.UpsertIGateConfig(ctx, &IGateConfig{Enabled: true, Server: "rotate.aprs2.net", Port: 14580}); err != nil {
		t.Fatalf("UpsertIGateConfig: %v", err)
	}

	var row struct {
		Callsign string
		Passcode string
	}
	if err := s.DB().Raw("SELECT callsign, passcode FROM i_gate_configs LIMIT 1").Scan(&row).Error; err != nil {
		t.Fatalf("scan orphan columns: %v", err)
	}
	if row.Callsign != "" || row.Passcode != "" {
		t.Fatalf("expected orphan columns zeroed, got callsign=%q passcode=%q", row.Callsign, row.Passcode)
	}

	// Simulate a legacy install by writing non-empty values directly
	// into the orphan columns, then run another upsert and assert they
	// get wiped.
	if err := s.DB().Exec("UPDATE i_gate_configs SET callsign = 'KE7XYZ-9', passcode = '22181'").Error; err != nil {
		t.Fatalf("seed legacy values: %v", err)
	}
	if err := s.UpsertIGateConfig(ctx, &IGateConfig{Enabled: false, Server: "rotate.aprs2.net", Port: 14580}); err != nil {
		t.Fatalf("second UpsertIGateConfig: %v", err)
	}
	if err := s.DB().Raw("SELECT callsign, passcode FROM i_gate_configs LIMIT 1").Scan(&row).Error; err != nil {
		t.Fatalf("scan orphan columns after second upsert: %v", err)
	}
	if row.Callsign != "" || row.Passcode != "" {
		t.Fatalf("expected orphan columns re-zeroed after upsert, got callsign=%q passcode=%q", row.Callsign, row.Passcode)
	}
}
