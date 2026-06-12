package configstore

import (
	"context"
	"testing"
)

// Regression coverage for graywolf issue #225 (GRA-34): toggling
// simulation mode via POST /api/igate/simulation must persist so the
// Simulation page (which reads simulation_mode from GET
// /api/igate/config on load) reflects it after a refresh. The toggle
// routes through SetIGateSimulationMode, which updates only the
// simulation_mode column so a concurrent PUT /api/igate/config can't be
// clobbered by a stale whole-row write-back.

// TestSetIGateSimulationMode_TouchesOnlySimulationColumn seeds a full
// config row and asserts the toggle flips simulation_mode in both
// directions while leaving every sibling field untouched — the no-clobber
// guarantee that motivated the single-column update.
func TestSetIGateSimulationMode_TouchesOnlySimulationColumn(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	seed := &IGateConfig{
		Enabled:        true,
		Server:         "rotate.aprs2.net",
		Port:           14580,
		ServerFilter:   "m/50",
		SimulationMode: false,
		GateRfToIs:     true,
		MaxMsgHops:     2,
		TxChannel:      1,
		RfChannel:      3,
	}
	if err := s.UpsertIGateConfig(ctx, seed); err != nil {
		t.Fatalf("seed UpsertIGateConfig: %v", err)
	}

	assertSiblingsUnchanged := func(got *IGateConfig) {
		t.Helper()
		if got.Server != seed.Server || got.Port != seed.Port ||
			got.ServerFilter != seed.ServerFilter || got.GateRfToIs != seed.GateRfToIs ||
			got.MaxMsgHops != seed.MaxMsgHops || got.TxChannel != seed.TxChannel ||
			got.RfChannel != seed.RfChannel || !got.Enabled {
			t.Fatalf("simulation toggle clobbered sibling fields: got %+v want siblings from %+v", got, seed)
		}
	}

	if err := s.SetIGateSimulationMode(ctx, true); err != nil {
		t.Fatalf("SetIGateSimulationMode(true): %v", err)
	}
	got, err := s.GetIGateConfig(ctx)
	if err != nil {
		t.Fatalf("GetIGateConfig after enable: %v", err)
	}
	if !got.SimulationMode {
		t.Fatalf("simulation_mode = false after enable; want true")
	}
	assertSiblingsUnchanged(got)

	if err := s.SetIGateSimulationMode(ctx, false); err != nil {
		t.Fatalf("SetIGateSimulationMode(false): %v", err)
	}
	got, err = s.GetIGateConfig(ctx)
	if err != nil {
		t.Fatalf("GetIGateConfig after disable: %v", err)
	}
	if got.SimulationMode {
		t.Fatalf("simulation_mode = true after disable; want false")
	}
	assertSiblingsUnchanged(got)
}

// TestSetIGateSimulationMode_NoRowCreatesSingleton covers the
// fresh-install branch: with no config row present, the toggle creates a
// singleton carrying just the simulation flag (other columns take their
// defaults, which the read path re-defaults). Reachable only outside the
// HTTP toggle path, but exercised so the branch can't silently rot.
func TestSetIGateSimulationMode_NoRowCreatesSingleton(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if existing, err := s.GetIGateConfig(ctx); err != nil {
		t.Fatalf("GetIGateConfig (precondition): %v", err)
	} else if existing != nil {
		t.Fatalf("precondition: expected no igate config row, got %+v", existing)
	}

	if err := s.SetIGateSimulationMode(ctx, true); err != nil {
		t.Fatalf("SetIGateSimulationMode on empty store: %v", err)
	}
	got, err := s.GetIGateConfig(ctx)
	if err != nil {
		t.Fatalf("GetIGateConfig after create: %v", err)
	}
	if got == nil {
		t.Fatalf("expected a singleton row to be created, got nil")
	}
	if !got.SimulationMode {
		t.Fatalf("simulation_mode = false on created row; want true")
	}

	// A second toggle must update the same row, not create another.
	if err := s.SetIGateSimulationMode(ctx, false); err != nil {
		t.Fatalf("SetIGateSimulationMode(false): %v", err)
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&IGateConfig{}).Count(&count).Error; err != nil {
		t.Fatalf("count igate config rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 igate config row, got %d", count)
	}
}
