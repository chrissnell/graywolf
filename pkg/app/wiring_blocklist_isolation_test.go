package app

import (
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/digipeater/blocklist"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// TestBlockedSourceStillReachesNonDigiConsumers enforces the spec's
// hard invariant: the digipeater block list is digipeater-only. A
// frame whose source is on the block list must still reach the APRS
// submit pipeline (which feeds the iGate's RF->IS adapter, the station
// cache, and the packet log). The digipeater silently drops the
// frame; nothing else is affected.
func TestBlockedSourceStillReachesNonDigiConsumers(t *testing.T) {
	h := newKissTncHarness(t)
	defer h.stop()

	h.app.digi.SetBlocklist([]blocklist.Entry{{Pattern: "BADCAL-*", Reason: "isolation test"}})

	src := mustAddrApp(t, "BADCAL-9")
	dest := mustAddrApp(t, "APRS")
	path := []ax25.Address{mustAddrApp(t, "WIDE2-2")}
	f, err := ax25.NewUIFrame(src, dest, path, []byte("=4900.00N/12300.00W>blocked"))
	if err != nil {
		t.Fatalf("NewUIFrame: %v", err)
	}
	axBytes, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	select {
	case h.app.rxFanout <- rxFanoutItem{
		rf:  &pb.ReceivedFrame{Channel: 1, Data: axBytes},
		src: ingress.Modem(),
	}:
	case <-time.After(time.Second):
		t.Fatal("rxFanout send blocked")
	}
	h.waitDispatched(1, 2*time.Second)

	if h.digiEmits.Len() != 0 {
		t.Fatalf("digipeater emitted %d frames for a blocked source, want 0", h.digiEmits.Len())
	}
	if h.app.digi.Stats().Blocked != 1 {
		t.Fatalf("digi Stats().Blocked=%d, want 1", h.app.digi.Stats().Blocked)
	}

	select {
	case <-h.aprsOut:
	case <-time.After(2 * time.Second):
		t.Fatal("APRS submit pipeline did not receive blocked frame; isolation invariant broken")
	}
}

func mustAddrApp(t *testing.T, s string) ax25.Address {
	t.Helper()
	a, err := ax25.ParseAddress(s)
	if err != nil {
		t.Fatalf("ParseAddress(%q): %v", s, err)
	}
	return a
}
