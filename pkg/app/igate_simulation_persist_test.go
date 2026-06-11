package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// Regression coverage for graywolf issue #225 (GRA-34): toggling
// simulation mode via POST /api/igate/simulation only flipped the
// runtime atomic (SetSimulationMode) and never wrote the stored config.
// The Simulation page reads simulation_mode from GET /api/igate/config
// on load, so after a refresh the toggle sprang back to its persisted
// (false) value even though simulation was actually running. The fix
// routes the toggle through persistIGateSimulation so the stored config
// — the source of truth the UI reads — stays in sync.
func TestPersistIGateSimulation_RoundTrip(t *testing.T) {
	ctx := context.Background()

	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer store.Close()

	// Seed a realistic enabled iGate config with simulation off and
	// other non-zero fields we expect to survive the read-modify-write.
	seed := &configstore.IGateConfig{
		Enabled:        true,
		Server:         "rotate.aprs2.net",
		Port:           14580,
		ServerFilter:   "m/50",
		SimulationMode: false,
		MaxMsgHops:     2,
		TxChannel:      1,
	}
	if err := store.UpsertIGateConfig(ctx, seed); err != nil {
		t.Fatalf("seed UpsertIGateConfig: %v", err)
	}

	a := &App{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		store:  store,
	}

	// Enable simulation: the stored config must now report it on, while
	// every other field is preserved unchanged.
	if err := a.persistIGateSimulation(ctx, true); err != nil {
		t.Fatalf("persistIGateSimulation(true): %v", err)
	}
	got, err := store.GetIGateConfig(ctx)
	if err != nil {
		t.Fatalf("GetIGateConfig after enable: %v", err)
	}
	if !got.SimulationMode {
		t.Fatalf("simulation_mode = false after persist(true); want true")
	}
	if got.Server != seed.Server || got.Port != seed.Port ||
		got.ServerFilter != seed.ServerFilter || got.MaxMsgHops != seed.MaxMsgHops ||
		got.TxChannel != seed.TxChannel || !got.Enabled {
		t.Fatalf("read-modify-write clobbered sibling fields: got %+v want server/port/filter/hops/tx/enabled from %+v", got, seed)
	}

	// Disable again: the stored config must flip back to off.
	if err := a.persistIGateSimulation(ctx, false); err != nil {
		t.Fatalf("persistIGateSimulation(false): %v", err)
	}
	got, err = store.GetIGateConfig(ctx)
	if err != nil {
		t.Fatalf("GetIGateConfig after disable: %v", err)
	}
	if got.SimulationMode {
		t.Fatalf("simulation_mode = true after persist(false); want false")
	}
}
