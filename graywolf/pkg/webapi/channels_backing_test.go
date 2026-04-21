package webapi

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// TestComputeChannelBacking_TxCapabilityMatrix walks the 2^3 combinations
// of (InputDeviceID nil/set, OutputDeviceID zero/non-zero, KissEntries
// empty/non-empty) and asserts both Tx.Capable and Tx.Reason. The KISS
// short-circuit takes precedence so rows with ≥1 TNC-mode interface are
// always Capable=true regardless of the modem-side fields (plan D1).
//
// The test also asserts the Reason contract invariant (Reason == "" iff
// Capable) on every row; this is the godoc'd invariant on
// dto.TxCapability.
func TestComputeChannelBacking_TxCapabilityMatrix(t *testing.T) {
	u := configstore.U32Ptr

	// Helper: run computeChannelBacking over a channel + optional KISS
	// interface and return the Tx sub-object. When wantKiss is true, we
	// attach one TNC-mode KISS interface to the channel.
	run := func(in *uint32, out uint32, wantKiss bool) dto.TxCapability {
		ch := configstore.Channel{ID: 42, Name: "ch", InputDeviceID: in, OutputDeviceID: out}
		var ifaces []configstore.KissInterface
		var statuses map[uint32]kiss.InterfaceStatus
		if wantKiss {
			ifaces = []configstore.KissInterface{
				{ID: 7, Name: "tnc", Channel: 42, Mode: configstore.KissModeTnc},
			}
			statuses = map[uint32]kiss.InterfaceStatus{
				7: {State: kiss.StateListening},
			}
		}
		return computeChannelBacking(ch, ifaces, statuses, false).Tx
	}

	cases := []struct {
		name       string
		inputID    *uint32
		outputID   uint32
		hasKiss    bool
		wantCap    bool
		wantReason string
	}{
		// KISS short-circuit branch: all four rows with hasKiss=true
		// are Capable regardless of the modem-side fields. The KISS
		// branch MUST win before the input-device check, otherwise a
		// KISS-only channel (InputDeviceID==nil) would be reported
		// non-TX-capable with "no input device configured" even though
		// it has a usable KISS path. This is the critical ordering
		// invariant from plan D1.
		{name: "kiss+input-nil+output-zero", inputID: nil, outputID: 0, hasKiss: true, wantCap: true, wantReason: ""},
		{name: "kiss+input-nil+output-set", inputID: nil, outputID: 2, hasKiss: true, wantCap: true, wantReason: ""},
		{name: "kiss+input-set+output-zero", inputID: u(1), outputID: 0, hasKiss: true, wantCap: true, wantReason: ""},
		{name: "kiss+input-set+output-set", inputID: u(1), outputID: 2, hasKiss: true, wantCap: true, wantReason: ""},

		// No KISS: the modem-only decision order applies.
		{name: "no-kiss+input-nil+output-zero (unbound)", inputID: nil, outputID: 0, hasKiss: false, wantCap: false, wantReason: dto.TxReasonNoInputDevice},
		{name: "no-kiss+input-nil+output-set (invalid but exercises branch)", inputID: nil, outputID: 2, hasKiss: false, wantCap: false, wantReason: dto.TxReasonNoInputDevice},
		{name: "no-kiss+input-set+output-zero (RX-only modem)", inputID: u(1), outputID: 0, hasKiss: false, wantCap: false, wantReason: dto.TxReasonNoOutputDevice},
		{name: "no-kiss+input-set+output-set (full modem)", inputID: u(1), outputID: 2, hasKiss: false, wantCap: true, wantReason: ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := run(c.inputID, c.outputID, c.hasKiss)
			if got.Capable != c.wantCap {
				t.Errorf("Capable=%v, want %v (reason=%q)", got.Capable, c.wantCap, got.Reason)
			}
			if got.Reason != c.wantReason {
				t.Errorf("Reason=%q, want %q", got.Reason, c.wantReason)
			}
			// Contract invariant: Reason == "" iff Capable == true. Assert
			// on every row so the guarantee can't drift.
			if got.Capable && got.Reason != "" {
				t.Errorf("contract violation: Capable=true but Reason=%q", got.Reason)
			}
			if !got.Capable && got.Reason == "" {
				t.Error("contract violation: Capable=false but Reason is empty")
			}
		})
	}
}

// TestComputeChannelBacking_TxIgnoresKissModemMode asserts that KISS
// interfaces in modem-mode (not TNC-mode) do NOT count as a TX backend
// — they are phase-3 TX paths for the modem, not for the governor.
// Without this carve-out a modem-mode KISS interface on an unbound
// channel would falsely report the channel as TX-capable.
func TestComputeChannelBacking_TxIgnoresKissModemMode(t *testing.T) {
	ch := configstore.Channel{ID: 9, Name: "mixed", InputDeviceID: nil, OutputDeviceID: 0}
	ifaces := []configstore.KissInterface{
		{ID: 1, Name: "modem-mode", Channel: 9, Mode: configstore.KissModeModem},
	}
	b := computeChannelBacking(ch, ifaces, nil, false)
	if b.Tx.Capable {
		t.Errorf("expected modem-mode KISS iface to NOT enable TX, got Capable=true")
	}
	if b.Tx.Reason != dto.TxReasonNoInputDevice {
		t.Errorf("expected reason=%q, got %q", dto.TxReasonNoInputDevice, b.Tx.Reason)
	}
}
