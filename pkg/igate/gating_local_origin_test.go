package igate

import (
	"sync"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// fakeLocalOrigin lets tests drive the LocalOriginRing contract.
type fakeLocalOrigin struct {
	mu      sync.Mutex
	entries map[string]struct{}
	calls   int
}

func newFakeLocalOrigin() *fakeLocalOrigin {
	return &fakeLocalOrigin{entries: make(map[string]struct{})}
}

func (f *fakeLocalOrigin) Add(source, msgID string) {
	f.mu.Lock()
	f.entries[source+"|"+msgID] = struct{}{}
	f.mu.Unlock()
}

func (f *fakeLocalOrigin) Contains(source, msgID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	_, ok := f.entries[source+"|"+msgID]
	return ok
}

func (f *fakeLocalOrigin) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// buildMessagePkt constructs a DecodedAPRSPacket simulating an RF-heard
// message with source + msgID. Uses the shared buildRawFrame helper so
// DedupKey() can decode a real info field from the raw bytes.
func buildMessagePkt(t *testing.T, source, addressee, msgID string) *aprs.DecodedAPRSPacket {
	t.Helper()
	pad := addressee
	for len(pad) < 9 {
		pad += " "
	}
	info := ":" + pad + ":hello{" + msgID
	raw := buildRawFrame(t, source, "APGRWO", []string{"WIDE1-1"}, info)
	return &aprs.DecodedAPRSPacket{
		Source: source,
		Dest:   "APGRWO",
		Path:   []string{"WIDE1-1"},
		Type:   aprs.PacketMessage,
		Message: &aprs.Message{
			Addressee: addressee,
			Text:      "hello",
			MessageID: msgID,
		},
		Raw: raw,
	}
}

func TestGating_LocalOrigin_SuppressesLocalMessage(t *testing.T) {
	ring := newFakeLocalOrigin()
	ring.Add("KE7XYZ", "042")

	ig, err := New(Config{
		Server:                     "127.0.0.1:1",
		StationCallsign:            "KE7XYZ",
		LocalOrigin:                ring,
		SuppressLocalMessageReGate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Mark connected so the drop-offline path doesn't fire.
	ig.mu.Lock()
	ig.connected = true
	ig.mu.Unlock()

	pkt := buildMessagePkt(t, "KE7XYZ", "W1ABC", "042")
	ig.gateRFToIS(pkt)

	if ig.Status().Gated != 0 {
		t.Errorf("locally-originated message was gated (Gated=%d)", ig.Status().Gated)
	}
	if ring.callCount() != 1 {
		t.Errorf("ring call count = %d, want 1", ring.callCount())
	}
}

func TestGating_LocalOrigin_MissDoesNotSuppress(t *testing.T) {
	ring := newFakeLocalOrigin() // empty
	ig, err := New(Config{
		Server:                     "127.0.0.1:1",
		StationCallsign:            "KE7XYZ",
		LocalOrigin:                ring,
		SuppressLocalMessageReGate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ig.mu.Lock()
	ig.connected = true
	ig.mu.Unlock()

	pkt := buildMessagePkt(t, "W5ABC", "KE7XYZ", "099")
	// The simulation path will log and increment Gated so we don't
	// need a real APRS-IS client. Enable simulation mode explicitly.
	ig.simulation.Store(true)
	ig.gateRFToIS(pkt)

	if ig.Status().Gated != 1 {
		t.Errorf("non-local message suppressed (Gated=%d)", ig.Status().Gated)
	}
}

func TestGating_LocalOrigin_OptOut(t *testing.T) {
	ring := newFakeLocalOrigin()
	ring.Add("KE7XYZ", "042")
	ig, err := New(Config{
		Server:                     "127.0.0.1:1",
		StationCallsign:            "KE7XYZ",
		LocalOrigin:                ring,
		SuppressLocalMessageReGate: false, // opted out
	})
	if err != nil {
		t.Fatal(err)
	}
	ig.mu.Lock()
	ig.connected = true
	ig.mu.Unlock()
	ig.simulation.Store(true)

	pkt := buildMessagePkt(t, "KE7XYZ", "W1ABC", "042")
	ig.gateRFToIS(pkt)
	if ig.Status().Gated != 1 {
		t.Errorf("opt-out should re-gate (Gated=%d, want 1)", ig.Status().Gated)
	}
	if ring.callCount() != 0 {
		t.Errorf("ring consulted even though SuppressLocalMessageReGate=false; calls=%d", ring.callCount())
	}
}

func TestGating_LocalOrigin_NoRingNoCheck(t *testing.T) {
	ig, err := New(Config{
		Server:                     "127.0.0.1:1",
		StationCallsign:            "KE7XYZ",
		LocalOrigin:                nil,
		SuppressLocalMessageReGate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ig.mu.Lock()
	ig.connected = true
	ig.mu.Unlock()
	ig.simulation.Store(true)
	pkt := buildMessagePkt(t, "KE7XYZ", "W1ABC", "042")
	// Should not panic despite nil ring + flag set.
	ig.gateRFToIS(pkt)
	if ig.Status().Gated != 1 {
		t.Errorf("nil-ring path should gate normally (Gated=%d)", ig.Status().Gated)
	}
}

func TestGating_LocalOrigin_NonMessagePacketIgnoresRing(t *testing.T) {
	ring := newFakeLocalOrigin()
	ring.Add("KE7XYZ", "042")
	ig, err := New(Config{
		Server:                     "127.0.0.1:1",
		StationCallsign:            "KE7XYZ",
		LocalOrigin:                ring,
		SuppressLocalMessageReGate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Position packet — no Message, no MessageID.
	pkt := &aprs.DecodedAPRSPacket{
		Source: "KE7XYZ",
		Dest:   "APRS",
		Type:   aprs.PacketPosition,
		Raw:    []byte{0x02},
	}
	if ig.shouldSuppressLocalMessage(pkt) {
		t.Error("position packet should never be suppressed by LocalOrigin")
	}
}
