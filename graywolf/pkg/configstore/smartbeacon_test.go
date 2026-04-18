package configstore

import (
	"context"
	"testing"
)

// TestSmartBeaconConfigEmptyStore asserts the singleton contract: a
// freshly-migrated store with no row returns (nil, nil) — the caller is
// expected to apply beacon.DefaultSmartBeacon()-derived defaults.
func TestSmartBeaconConfigEmptyStore(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	got, err := s.GetSmartBeaconConfig(ctx)
	if err != nil {
		t.Fatalf("GetSmartBeaconConfig: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil config on empty store, got %+v", got)
	}
}

// TestSmartBeaconConfigUpsertRoundTrip covers the happy path: upsert
// stores the row, Get reads it back, a second upsert updates in place
// (no duplicate row).
func TestSmartBeaconConfigUpsertRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	cfg := &SmartBeaconConfig{
		Enabled:     true,
		FastSpeedKt: 70,
		FastRateSec: 120,
		SlowSpeedKt: 3,
		SlowRateSec: 1200,
		MinTurnDeg:  25,
		TurnSlope:   240,
		MinTurnSec:  20,
	}
	if err := s.UpsertSmartBeaconConfig(ctx, cfg); err != nil {
		t.Fatalf("UpsertSmartBeaconConfig: %v", err)
	}
	if cfg.ID == 0 {
		t.Fatalf("expected ID assigned after upsert")
	}

	got, err := s.GetSmartBeaconConfig(ctx)
	if err != nil {
		t.Fatalf("GetSmartBeaconConfig: %v", err)
	}
	if got == nil {
		t.Fatalf("expected stored config, got nil")
	}
	if got.Enabled != true || got.FastSpeedKt != 70 || got.FastRateSec != 120 ||
		got.SlowSpeedKt != 3 || got.SlowRateSec != 1200 ||
		got.MinTurnDeg != 25 || got.TurnSlope != 240 || got.MinTurnSec != 20 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	// Second upsert with a fresh struct (ID=0) must update in place, not
	// insert a duplicate. Verifies the existing-ID-adoption branch.
	update := &SmartBeaconConfig{
		Enabled:     false,
		FastSpeedKt: 80,
		FastRateSec: 90,
		SlowSpeedKt: 4,
		SlowRateSec: 1500,
		MinTurnDeg:  28,
		TurnSlope:   250,
		MinTurnSec:  30,
	}
	if err := s.UpsertSmartBeaconConfig(ctx, update); err != nil {
		t.Fatalf("second UpsertSmartBeaconConfig: %v", err)
	}
	got2, err := s.GetSmartBeaconConfig(ctx)
	if err != nil {
		t.Fatalf("GetSmartBeaconConfig after update: %v", err)
	}
	if got2 == nil {
		t.Fatalf("expected stored config after update, got nil")
	}
	if got2.Enabled != false || got2.FastSpeedKt != 80 || got2.FastRateSec != 90 {
		t.Fatalf("update not reflected: %+v", got2)
	}
	if got2.ID != got.ID {
		t.Fatalf("expected ID preserved across upsert, got %d vs %d", got2.ID, got.ID)
	}

	// Confirm exactly one row survives — the singleton invariant.
	var count int64
	if err := s.DB().Model(&SmartBeaconConfig{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one smart_beacon_config row, got %d", count)
	}
}

// TestSeedSmartBeaconFromLegacyBeaconsDefaults asserts the seed helper
// is a no-op when no beacon carries non-default Sb* values — the
// DefaultSmartBeacon() path in the DTO layer will serve defaults.
func TestSeedSmartBeaconFromLegacyBeaconsDefaults(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Create a beacon using the legacy Sb* gorm-tag defaults so the
	// seed predicate's "any non-default?" query finds nothing.
	b := &Beacon{
		Type:          "position",
		Channel:       1,
		Callsign:      "N0CAL",
		Path:          "WIDE1-1",
		Latitude:      40,
		Longitude:     -105,
		SymbolTable:   "/",
		Symbol:        ">",
		EverySeconds:  1800,
		Enabled:       true,
		SbFastSpeed:   legacySbFastSpeed,
		SbSlowSpeed:   legacySbSlowSpeed,
		SbFastRate:    legacySbFastRate,
		SbSlowRate:    legacySbSlowRate,
		SbTurnAngle:   legacySbTurnAngle,
		SbTurnSlope:   legacySbTurnSlope,
		SbMinTurnTime: legacySbMinTurnTime,
	}
	if err := s.CreateBeacon(ctx, b); err != nil {
		t.Fatalf("CreateBeacon: %v", err)
	}

	if err := s.seedSmartBeaconFromLegacyBeacons(ctx); err != nil {
		t.Fatalf("seedSmartBeaconFromLegacyBeacons: %v", err)
	}

	got, err := s.GetSmartBeaconConfig(ctx)
	if err != nil {
		t.Fatalf("GetSmartBeaconConfig: %v", err)
	}
	if got != nil {
		t.Fatalf("expected seed to be a no-op, got %+v", got)
	}
}

// TestSeedSmartBeaconFromLegacyBeaconsMigrates asserts the seed helper
// migrates a single beacon's non-default Sb* values into the singleton
// with Enabled=false.
func TestSeedSmartBeaconFromLegacyBeaconsMigrates(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Legacy-defaults beacon (should be ignored).
	if err := s.CreateBeacon(ctx, &Beacon{
		Type: "position", Channel: 1, Callsign: "N0CAL",
		Path: "WIDE1-1", SymbolTable: "/", Symbol: ">",
		EverySeconds:  1800,
		SbFastSpeed:   legacySbFastSpeed,
		SbSlowSpeed:   legacySbSlowSpeed,
		SbFastRate:    legacySbFastRate,
		SbSlowRate:    legacySbSlowRate,
		SbTurnAngle:   legacySbTurnAngle,
		SbTurnSlope:   legacySbTurnSlope,
		SbMinTurnTime: legacySbMinTurnTime,
	}); err != nil {
		t.Fatalf("CreateBeacon (defaults): %v", err)
	}

	// Tuned beacon — FastRate differs from the legacy default.
	tuned := &Beacon{
		Type: "tracker", Channel: 1, Callsign: "N0CAL-9",
		Path: "WIDE1-1", SymbolTable: "/", Symbol: ">",
		EverySeconds:  1800,
		SmartBeacon:   true,
		SbFastSpeed:   legacySbFastSpeed,
		SbSlowSpeed:   legacySbSlowSpeed,
		SbFastRate:    45, // <- user-tuned
		SbSlowRate:    legacySbSlowRate,
		SbTurnAngle:   legacySbTurnAngle,
		SbTurnSlope:   legacySbTurnSlope,
		SbMinTurnTime: legacySbMinTurnTime,
	}
	if err := s.CreateBeacon(ctx, tuned); err != nil {
		t.Fatalf("CreateBeacon (tuned): %v", err)
	}

	if err := s.seedSmartBeaconFromLegacyBeacons(ctx); err != nil {
		t.Fatalf("seedSmartBeaconFromLegacyBeacons: %v", err)
	}

	got, err := s.GetSmartBeaconConfig(ctx)
	if err != nil {
		t.Fatalf("GetSmartBeaconConfig: %v", err)
	}
	if got == nil {
		t.Fatalf("expected seeded singleton, got nil")
	}
	if got.Enabled {
		t.Fatalf("seeded row must start Enabled=false (user opts in), got true")
	}
	if got.FastRateSec != 45 {
		t.Fatalf("expected seeded FastRateSec=45, got %d", got.FastRateSec)
	}
	if got.FastSpeedKt != legacySbFastSpeed || got.SlowSpeedKt != legacySbSlowSpeed ||
		got.SlowRateSec != legacySbSlowRate || got.MinTurnDeg != legacySbTurnAngle ||
		got.TurnSlope != legacySbTurnSlope || got.MinTurnSec != legacySbMinTurnTime {
		t.Fatalf("unexpected carried-over values: %+v", got)
	}
}

// TestSeedSmartBeaconFromLegacyBeaconsIdempotent asserts the seed
// helper leaves an existing singleton row alone — the helper must be
// safe to call on every startup without clobbering user edits.
func TestSeedSmartBeaconFromLegacyBeaconsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Populate a user-edited singleton first.
	user := &SmartBeaconConfig{
		Enabled:     true,
		FastSpeedKt: 99,
		FastRateSec: 30,
		SlowSpeedKt: 2,
		SlowRateSec: 900,
		MinTurnDeg:  10,
		TurnSlope:   100,
		MinTurnSec:  5,
	}
	if err := s.UpsertSmartBeaconConfig(ctx, user); err != nil {
		t.Fatalf("UpsertSmartBeaconConfig: %v", err)
	}

	// Add a beacon with tuned Sb* values that would otherwise trigger a
	// seed. The helper must still be a no-op because a row exists.
	if err := s.CreateBeacon(ctx, &Beacon{
		Type: "tracker", Channel: 1, Callsign: "N0CAL-9",
		Path: "WIDE1-1", SymbolTable: "/", Symbol: ">",
		EverySeconds:  1800,
		SbFastSpeed:   50, // non-default — would seed if no row existed
		SbSlowSpeed:   legacySbSlowSpeed,
		SbFastRate:    legacySbFastRate,
		SbSlowRate:    legacySbSlowRate,
		SbTurnAngle:   legacySbTurnAngle,
		SbTurnSlope:   legacySbTurnSlope,
		SbMinTurnTime: legacySbMinTurnTime,
	}); err != nil {
		t.Fatalf("CreateBeacon: %v", err)
	}

	if err := s.seedSmartBeaconFromLegacyBeacons(ctx); err != nil {
		t.Fatalf("seedSmartBeaconFromLegacyBeacons: %v", err)
	}

	got, err := s.GetSmartBeaconConfig(ctx)
	if err != nil {
		t.Fatalf("GetSmartBeaconConfig: %v", err)
	}
	if got == nil {
		t.Fatalf("expected user row preserved, got nil")
	}
	if got.FastSpeedKt != 99 || got.FastRateSec != 30 || !got.Enabled {
		t.Fatalf("seed clobbered user row: %+v", got)
	}
}

// TestSmartBeaconConfigSeedRunsOnMigrate asserts the seed helper is
// invoked automatically from Migrate() — the contract Phase 3 relies on
// (adapters read GetSmartBeaconConfig without needing to trigger the
// seed themselves). Runs by creating a beacon with tuned values,
// re-calling Migrate, and observing the singleton appears.
func TestSmartBeaconConfigSeedRunsOnMigrate(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Tuned beacon created against the already-migrated store.
	if err := s.CreateBeacon(ctx, &Beacon{
		Type: "tracker", Channel: 1, Callsign: "N0CAL-9",
		Path: "WIDE1-1", SymbolTable: "/", Symbol: ">",
		EverySeconds:  1800,
		SbFastSpeed:   legacySbFastSpeed,
		SbSlowSpeed:   legacySbSlowSpeed,
		SbFastRate:    legacySbFastRate,
		SbSlowRate:    legacySbSlowRate,
		SbTurnAngle:   legacySbTurnAngle,
		SbTurnSlope:   999, // user-tuned
		SbMinTurnTime: legacySbMinTurnTime,
	}); err != nil {
		t.Fatalf("CreateBeacon: %v", err)
	}

	// The initial Migrate (called from OpenMemory) ran before the beacon
	// existed. Simulate the "fresh start with legacy data on disk" path
	// by re-invoking Migrate and asserting the seed populates the row.
	if err := s.Migrate(); err != nil {
		t.Fatalf("re-Migrate: %v", err)
	}

	got, err := s.GetSmartBeaconConfig(ctx)
	if err != nil {
		t.Fatalf("GetSmartBeaconConfig: %v", err)
	}
	if got == nil {
		t.Fatalf("expected Migrate to seed singleton from legacy beacon")
	}
	if got.TurnSlope != 999 {
		t.Fatalf("expected seeded TurnSlope=999, got %d", got.TurnSlope)
	}
}
