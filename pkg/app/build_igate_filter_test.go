package app

import (
	"context"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// TestBuildIgateFilter_PullsFreshTacticalState pins the pull-model
// contract: every call to buildIgateFilter reads the current enabled
// tactical set from the store, so a dropped signalIgateReload does
// not cause the live filter to diverge from persisted state.
func TestBuildIgateFilter_PullsFreshTacticalState(t *testing.T) {
	ctx := context.Background()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer store.Close()

	if err := store.UpsertIGateConfig(ctx, &configstore.IGateConfig{
		Enabled: false, Server: "x", Port: 1, ServerFilter: "m/50",
	}); err != nil {
		t.Fatalf("UpsertIGateConfig: %v", err)
	}

	// No tacticals yet: expect the base filter to pass through.
	got, err := buildIgateFilter(ctx, store)
	if err != nil {
		t.Fatalf("buildIgateFilter (empty tacticals): %v", err)
	}
	if got != "m/50" {
		t.Fatalf("empty tacticals: got %q, want %q", got, "m/50")
	}

	// Add two enabled tacticals and one disabled. Only the enabled
	// two should appear, and they must be sorted lexically.
	must := func(cs string, enabled bool) {
		t.Helper()
		row := &configstore.TacticalCallsign{Callsign: cs, Enabled: enabled}
		if err := store.CreateTacticalCallsign(ctx, row); err != nil {
			t.Fatalf("CreateTacticalCallsign %q: %v", cs, err)
		}
	}
	must("NET1", true)
	must("BASECAMP", true)
	must("DISABLED1", false)

	got, err = buildIgateFilter(ctx, store)
	if err != nil {
		t.Fatalf("buildIgateFilter (with tacticals): %v", err)
	}
	// Sort guarantees BASECAMP before NET1 regardless of insert order.
	want := "m/50 g/BASECAMP/NET1"
	if got != want {
		t.Fatalf("with tacticals: got %q, want %q", got, want)
	}
}

// TestBuildIgateFilter_DeterministicOrdering protects the no-op-skip
// string-equality check in reloadIgate: two invocations with the same
// persisted state must return byte-identical output. Without the sort
// in buildIgateFilter, DB ordering changes (or GORM reorder under
// load) would defeat the no-op-skip and cause spurious reconnects.
func TestBuildIgateFilter_DeterministicOrdering(t *testing.T) {
	ctx := context.Background()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer store.Close()

	if err := store.UpsertIGateConfig(ctx, &configstore.IGateConfig{
		Enabled: false, Server: "x", Port: 1, ServerFilter: "",
	}); err != nil {
		t.Fatalf("UpsertIGateConfig: %v", err)
	}
	// Insert tacticals in an order that is NOT lexically sorted. The
	// output must still be lexical.
	for _, cs := range []string{"ZULU", "ALPHA", "MIKE"} {
		if err := store.CreateTacticalCallsign(ctx, &configstore.TacticalCallsign{
			Callsign: cs, Enabled: true,
		}); err != nil {
			t.Fatalf("CreateTacticalCallsign %q: %v", cs, err)
		}
	}

	a, err := buildIgateFilter(ctx, store)
	if err != nil {
		t.Fatalf("buildIgateFilter (first): %v", err)
	}
	b, err := buildIgateFilter(ctx, store)
	if err != nil {
		t.Fatalf("buildIgateFilter (second): %v", err)
	}
	if a != b {
		t.Fatalf("non-deterministic composition: %q vs %q", a, b)
	}
	want := "g/ALPHA/MIKE/ZULU"
	if a != want {
		t.Fatalf("got %q, want %q", a, want)
	}
}

// TestBuildIgateFilter_EmptyBaseEmptyTacticals confirms the empty-in
// → empty-out contract is honored all the way through to the caller.
// The sentinel substitution happens inside pkg/igate/client.go and
// must NOT be duplicated in buildIgateFilter.
func TestBuildIgateFilter_EmptyBaseEmptyTacticals(t *testing.T) {
	ctx := context.Background()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer store.Close()
	if err := store.UpsertIGateConfig(ctx, &configstore.IGateConfig{
		Enabled: false, Server: "x", Port: 1, ServerFilter: "",
	}); err != nil {
		t.Fatalf("UpsertIGateConfig: %v", err)
	}
	got, err := buildIgateFilter(ctx, store)
	if err != nil {
		t.Fatalf("buildIgateFilter: %v", err)
	}
	if got != "" {
		t.Fatalf("empty base + no tacticals: got %q, want empty string", got)
	}
}
