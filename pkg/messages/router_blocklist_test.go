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
