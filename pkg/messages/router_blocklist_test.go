package messages

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/configstore"
)

// buildRouterWithBlocklist mirrors buildRouter but seeds a BlocklistSet
// and monitors "NET" as a tactical so both DM and tactical inbound paths
// can be exercised against the blocklist filter.
func buildRouterWithBlocklist(t *testing.T, ourCall string, blocked []string) (*Router, *Store, *fakeTxSink, func()) {
	t.Helper()
	cs, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	store := NewStore(cs.DB())
	sink := &fakeTxSink{}
	ring := NewLocalTxRing(16, time.Minute)
	tact := NewTacticalSet()
	tact.Store(map[string]struct{}{"NET": {}})
	block := NewBlocklistSet()
	if len(blocked) > 0 {
		m := make(map[string]struct{}, len(blocked))
		for _, k := range blocked {
			m[k] = struct{}{}
		}
		block.Store(m)
	}
	hub := NewEventHub(16)
	r, err := NewRouter(RouterConfig{
		Store:       store,
		TxSink:      sink,
		IGateSender: &fakeIGateSender{},
		OurCall:     func() string { return ourCall },
		LocalTxRing: ring,
		TacticalSet: tact,
		BlockedSet:  block,
		EventHub:    hub,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:       &fakeClock{now: time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	r.Start(context.Background())
	cleanup := func() {
		r.Stop()
		_ = cs.Close()
	}
	return r, store, sink, cleanup
}

func TestRouterBlockedSenderDroppedNoPersistNoAutoACK(t *testing.T) {
	r, store, sink, cleanup := buildRouterWithBlocklist(t, "N0CALL", []string{"W1ABC"})
	defer cleanup()

	// DM addressed to us, but from a blocked sender.
	pkt := makeMessagePacket(t, "W1ABC", "N0CALL", "cert claim", "001", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	time.Sleep(50 * time.Millisecond)

	ms, _, _ := store.List(context.Background(), Filter{})
	if len(ms) != 0 {
		t.Fatalf("blocked sender must not persist, got %d rows", len(ms))
	}
	if got := len(sink.list()); got != 0 {
		t.Fatalf("blocked sender must not be auto-ACKed, got %d", got)
	}
}

func TestRouterBlockedBareCallBlocksSSID(t *testing.T) {
	r, store, _, cleanup := buildRouterWithBlocklist(t, "N0CALL", []string{"W1ABC"})
	defer cleanup()

	// Blocklist holds the bare base call; a message from any SSID of it
	// must be dropped.
	pkt := makeMessagePacket(t, "W1ABC-9", "N0CALL", "cert claim", "002", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	time.Sleep(50 * time.Millisecond)

	ms, _, _ := store.List(context.Background(), Filter{})
	if len(ms) != 0 {
		t.Fatalf("bare-call block must cover all SSIDs, got %d rows", len(ms))
	}
}

func TestRouterUnblockedSenderStillDelivered(t *testing.T) {
	r, store, _, cleanup := buildRouterWithBlocklist(t, "N0CALL", []string{"W1ABC"})
	defer cleanup()

	// A different sender is not blocked and should persist normally.
	pkt := makeMessagePacket(t, "K1XYZ", "N0CALL", "hello", "003", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	waitFor(t, time.Second, func() bool {
		ms, _, _ := store.List(context.Background(), Filter{})
		return len(ms) == 1
	}, "unblocked message persisted")

	ms, _, _ := store.List(context.Background(), Filter{})
	if ms[0].FromCall != "K1XYZ" {
		t.Fatalf("FromCall = %q, want K1XYZ", ms[0].FromCall)
	}
}

// TestRouterDisabledBlocklistEntryAdmitsTraffic wires the real
// enabled-only reload contract (ListEnabledBlockedCallsigns → BlocklistSet)
// against a router: a disabled row must NOT block, an enabled one must.
// This guards the core enable/disable toggle end to end at the store level.
func TestRouterDisabledBlocklistEntryAdmitsTraffic(t *testing.T) {
	t.Helper()
	cs, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer func() { _ = cs.Close() }()
	ctx := context.Background()
	// One disabled block (W1ABC) and one enabled block (K9ZZZ).
	if err := cs.CreateBlockedCallsign(ctx, &configstore.BlockedCallsign{Callsign: "W1ABC", Enabled: false}); err != nil {
		t.Fatalf("create disabled: %v", err)
	}
	if err := cs.CreateBlockedCallsign(ctx, &configstore.BlockedCallsign{Callsign: "K9ZZZ", Enabled: true}); err != nil {
		t.Fatalf("create enabled: %v", err)
	}

	store := NewStore(cs.DB())
	block := NewBlocklistSet()
	// Mirror Service.ReloadBlockedCallsigns exactly.
	rows, err := cs.ListEnabledBlockedCallsigns(ctx)
	if err != nil {
		t.Fatalf("ListEnabledBlockedCallsigns: %v", err)
	}
	set := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		set[row.Callsign] = struct{}{}
	}
	block.Store(set)

	r, err := NewRouter(RouterConfig{
		Store:       store,
		TxSink:      &fakeTxSink{},
		IGateSender: &fakeIGateSender{},
		OurCall:     func() string { return "N0CALL" },
		LocalTxRing: NewLocalTxRing(16, time.Minute),
		TacticalSet: NewTacticalSet(),
		BlockedSet:  block,
		EventHub:    NewEventHub(16),
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:       &fakeClock{now: time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	r.Start(ctx)
	defer r.Stop()

	// Disabled entry: traffic must be delivered.
	_ = r.SendPacket(ctx, makeMessagePacket(t, "W1ABC", "N0CALL", "still here", "005", aprs.DirectionRF))
	waitFor(t, time.Second, func() bool {
		ms, _, _ := store.List(ctx, Filter{})
		return len(ms) == 1
	}, "disabled-block sender delivered")

	// Enabled entry: traffic must be dropped (row count stays at 1).
	_ = r.SendPacket(ctx, makeMessagePacket(t, "K9ZZZ", "N0CALL", "spam", "006", aprs.DirectionRF))
	time.Sleep(50 * time.Millisecond)
	ms, _, _ := store.List(ctx, Filter{})
	if len(ms) != 1 {
		t.Fatalf("expected only the disabled-block sender to persist, got %d rows", len(ms))
	}
	if ms[0].FromCall != "W1ABC" {
		t.Fatalf("FromCall = %q, want W1ABC", ms[0].FromCall)
	}
}

func TestRouterBlockedTacticalSenderDropped(t *testing.T) {
	r, store, _, cleanup := buildRouterWithBlocklist(t, "N0CALL", []string{"W1ABC"})
	defer cleanup()

	// Blocked sender posting to a monitored tactical is also dropped —
	// the block applies to the source regardless of thread kind.
	pkt := makeMessagePacket(t, "W1ABC", "NET", "spam", "004", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	time.Sleep(50 * time.Millisecond)

	ms, _, _ := store.List(context.Background(), Filter{})
	if len(ms) != 0 {
		t.Fatalf("blocked tactical sender must not persist, got %d rows", len(ms))
	}
}
