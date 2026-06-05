package igate

import (
	"context"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// TestGateClientTxSentinelReasons covers the two early-return sentinels
// the kiss-modem gate hook relies on to distinguish "iGate fully off"
// from a real gating decision. The real gating outcomes (third-party,
// offline, gated, …) are exercised by the gating_test.go cases via
// gateRFToIS directly.
func TestGateClientTxSentinelReasons(t *testing.T) {
	// nil receiver → "no-output". The hook calls GateClientTx through
	// the *IgateOutput pointer stored on App; we model the nil-output
	// case as an early-stage wiring fault.
	var nilOut *IgateOutput
	if got := nilOut.GateClientTx(context.Background(), nil); got != GateReason("no-output") {
		t.Errorf("nil IgateOutput: got %q, want %q", got, "no-output")
	}

	// Allocated output but no inner Igate (the iGate is disabled at
	// runtime). The hook must surface this distinctly from a real
	// gating drop so the operator log line can say "your iGate is
	// off" rather than "your packet hit some filter".
	out := NewIgateOutput(nil)
	if got := out.GateClientTx(context.Background(), nil); got != GateReason("igate-disabled") {
		t.Errorf("nil inner Igate: got %q, want %q", got, "igate-disabled")
	}
}

// TestGateRFToISReturnsReasons asserts each branch of gateRFToIS
// returns the documented GateReason. Mirrors the existing
// gating_test.go cases (third-party / offline) at the new return-value
// API so a future change to the reason names breaks here loudly rather
// than silently regressing the kiss-client log line.
func TestGateRFToISReturnsReasons(t *testing.T) {
	t.Run("nil packet", func(t *testing.T) {
		ig := mustIgate(t)
		if got := ig.gateRFToIS(nil); got != GateReasonNilPacket {
			t.Errorf("nil pkt: got %q, want %q", got, GateReasonNilPacket)
		}
	})

	t.Run("third party", func(t *testing.T) {
		ig := mustIgate(t)
		ig.mu.Lock()
		ig.connected = true
		ig.mu.Unlock()
		raw := buildRawFrame(t, "W5ABC-7", "APRS", nil, "}KE7XYZ>APRS,TCPIP*:test")
		pkt := &aprs.DecodedAPRSPacket{
			Source:     "W5ABC-7",
			Dest:       "APRS",
			Raw:        raw,
			ThirdParty: &aprs.DecodedAPRSPacket{Source: "KE7XYZ"},
		}
		if got := ig.gateRFToIS(pkt); got != GateReasonThirdParty {
			t.Errorf("third-party: got %q, want %q", got, GateReasonThirdParty)
		}
	})

	t.Run("path blocks", func(t *testing.T) {
		ig := mustIgate(t)
		ig.mu.Lock()
		ig.connected = true
		ig.mu.Unlock()
		pkt := &aprs.DecodedAPRSPacket{
			Source: "W5ABC-7",
			Dest:   "APRS",
			Path:   []string{"TCPIP*"},
			Raw:    buildRawFrame(t, "W5ABC-7", "APRS", nil, "!3725.00N/12158.00W>hi"),
		}
		if got := ig.gateRFToIS(pkt); got != GateReasonPathBlocks {
			t.Errorf("path-blocks: got %q, want %q", got, GateReasonPathBlocks)
		}
	})

	t.Run("offline", func(t *testing.T) {
		ig := mustIgate(t)
		// Default disconnected.
		pkt := &aprs.DecodedAPRSPacket{
			Source: "W5ABC-7",
			Dest:   "APRS",
			Raw:    buildRawFrame(t, "W5ABC-7", "APRS", nil, "!3725.00N/12158.00W>hi"),
			Type:   aprs.PacketPosition,
		}
		if got := ig.gateRFToIS(pkt); got != GateReasonOffline {
			t.Errorf("offline: got %q, want %q", got, GateReasonOffline)
		}
	})
}

func mustIgate(t *testing.T) *Igate {
	t.Helper()
	ig, err := New(Config{Server: "127.0.0.1:1", StationCallsign: "KE7XYZ"})
	if err != nil {
		t.Fatal(err)
	}
	return ig
}
