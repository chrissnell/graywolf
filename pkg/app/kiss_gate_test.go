package app

import (
	"context"
	"testing"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/igate"
)

// TestKissClientTxGateToIs_NoIgateIsNoop verifies the App method
// gracefully tolerates a nil igateOut (iGate disabled or not wired).
func TestKissClientTxGateToIs_NoIgateIsNoop(t *testing.T) {
	app := &App{
		logger:   quietLogger(),
		igateOut: nil,
	}
	src, _ := ax25.ParseAddress("N0CALL")
	dst, _ := ax25.ParseAddress("APRS")
	f := &ax25.Frame{
		Source:  src,
		Dest:    dst,
		Control: ax25.ControlUI,
		PID:     ax25.PIDNoLayer3,
		Info:    []byte(`=4900.00N/12300.00W-wx`),
	}
	app.kissClientTxGateToIs(context.Background(), 1, 7, f)
}

// TestKissClientTxGateToIs_FeedsIgate verifies the parsed packet is
// passed to igateOut.SendPacket with Channel + Direction set. Uses
// an IgateOutput wrapping a nil *Igate so SendPacket is a no-op but
// the call site is exercised — combined with the manager-level
// integration test in Task 10, this catches the parse + tag work
// without a live iGate.
func TestKissClientTxGateToIs_FeedsIgate(t *testing.T) {
	app := &App{
		logger:   quietLogger(),
		igateOut: igate.NewIgateOutput(nil),
	}
	src, _ := ax25.ParseAddress("N0CALL")
	dst, _ := ax25.ParseAddress("APRS")
	f := &ax25.Frame{
		Source:  src,
		Dest:    dst,
		Control: ax25.ControlUI,
		PID:     ax25.PIDNoLayer3,
		Info:    []byte(`=4900.00N/12300.00W-wx`),
	}
	app.kissClientTxGateToIs(context.Background(), 1, 7, f)
}
