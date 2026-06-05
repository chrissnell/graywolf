package app

import (
	"context"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
)

// uiFrame builds a minimal AX.25 UI frame carrying info as the APRS
// info field. Helper for the gate-decoder tests below.
func uiFrame(t *testing.T, info string) *ax25.Frame {
	t.Helper()
	src, _ := ax25.ParseAddress("N0CALL")
	dst, _ := ax25.ParseAddress("APRS")
	return &ax25.Frame{
		Source:  src,
		Dest:    dst,
		Control: ax25.ControlUI,
		PID:     ax25.PIDNoLayer3,
		Info:    []byte(info),
	}
}

// TestDecodeAprsForGate_TagsPacket exercises the parse + tag path:
// a UI frame carrying a valid APRS position info field must produce
// a packet whose Channel matches the originating KISS channel and
// whose Direction is DirectionRF (so the iGate's filter chain treats
// it as RF-originated, which it logically is).
func TestDecodeAprsForGate_TagsPacket(t *testing.T) {
	pkt := decodeAprsForGate(7, uiFrame(t, `=4900.00N/12300.00W-wx`))
	if pkt == nil {
		t.Fatalf("decodeAprsForGate returned nil, want packet")
	}
	if pkt.Channel != 7 {
		t.Errorf("pkt.Channel=%d, want 7", pkt.Channel)
	}
	if pkt.Direction != aprs.DirectionRF {
		t.Errorf("pkt.Direction=%q, want %q", pkt.Direction, aprs.DirectionRF)
	}
}

// TestDecodeAprsForGate_DropsNonAprs covers the silent-drop policy
// for frames that don't belong on APRS-IS even if a connected KISS
// client submits them: nil frames, non-UI frames, and UI frames
// whose info field doesn't parse as APRS (we use an empty info
// payload, which aprs.Parse rejects).
func TestDecodeAprsForGate_DropsNonAprs(t *testing.T) {
	if got := decodeAprsForGate(1, nil); got != nil {
		t.Errorf("nil frame: got=%v, want nil", got)
	}

	// Non-UI: connected-mode I-frame should not gate to APRS-IS.
	src, _ := ax25.ParseAddress("N0CALL")
	dst, _ := ax25.ParseAddress("APRS")
	iFrame := &ax25.Frame{
		Source:  src,
		Dest:    dst,
		Control: 0x00, // I-frame, not UI
		PID:     ax25.PIDNoLayer3,
		Info:    []byte("data"),
	}
	if got := decodeAprsForGate(1, iFrame); got != nil {
		t.Errorf("non-UI frame: got=%v, want nil", got)
	}

	// UI frame with an info field aprs.Parse rejects (empty info).
	if got := decodeAprsForGate(1, uiFrame(t, "")); got != nil {
		t.Errorf("unparseable UI frame: got=%v, want nil", got)
	}
}

// TestKissClientTxGateToIs_NoIgateIsNoop verifies the App method
// gracefully tolerates a nil igateOut (iGate disabled or not wired)
// and does not panic. The parse + tag work is covered by
// TestDecodeAprsForGate_TagsPacket above.
func TestKissClientTxGateToIs_NoIgateIsNoop(t *testing.T) {
	app := &App{
		logger:   quietLogger(),
		igateOut: nil,
	}
	app.kissClientTxGateToIs(context.Background(), 1, 7, uiFrame(t, `=4900.00N/12300.00W-wx`))
}
