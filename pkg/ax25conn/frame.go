package ax25conn

import (
	"fmt"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

// Frame is a connected-mode AX.25 frame. The address layout is
// identical to UI frames; only the control field and presence of
// PID/Info differ.
type Frame struct {
	Source, Dest ax25.Address
	Path         []ax25.Address
	Control      Control
	PID          byte // I and UI only
	Info         []byte
	IsCommand    bool // direwolf-style C-bit polarity (dest C set, source C clear)
	Mod128       bool
}

// hasInfo reports whether the frame's Control.Kind carries an info
// field (I, UI, FRMR, XID, TEST). Other kinds are control-only.
func (f *Frame) hasInfo() bool {
	switch f.Control.Kind {
	case FrameI, FrameUI, FrameFRMR, FrameXID, FrameTEST:
		return true
	}
	return false
}

// hasPID reports whether the frame carries a PID byte (I and UI only).
func (f *Frame) hasPID() bool {
	return f.Control.Kind == FrameI || f.Control.Kind == FrameUI
}

// ToAX25Frame projects f into the *ax25.Frame surface that
// pkg/txgovernor and TxHook consumers observe. Sets ConnectedControl
// to the encoded bytes; UI Control byte is left zero.
func (f *Frame) ToAX25Frame() (*ax25.Frame, error) {
	ctl, err := EncodeControl(f.Control, f.Mod128)
	if err != nil {
		return nil, err
	}
	out := &ax25.Frame{
		Dest:             f.Dest,
		Source:           f.Source,
		Path:             append([]ax25.Address(nil), f.Path...),
		CommandResp:      f.IsCommand,
		ConnectedControl: ctl,
		ConnectedHasInfo: f.hasInfo(),
	}
	if f.hasPID() {
		out.PID = f.PID
	}
	if f.hasInfo() {
		out.Info = append([]byte(nil), f.Info...)
	}
	return out, nil
}

// Encode emits the wire bytes (no FCS — modem appends).
func (f *Frame) Encode() ([]byte, error) {
	if len(f.Path) > ax25.MaxRepeaters {
		return nil, fmt.Errorf("ax25conn: path length %d > %d", len(f.Path), ax25.MaxRepeaters)
	}
	addrBytes, err := ax25.EncodeAddressBlock(f.Source, f.Dest, f.Path, f.IsCommand)
	if err != nil {
		return nil, err
	}
	ctl, err := EncodeControl(f.Control, f.Mod128)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(addrBytes)+len(ctl)+1+len(f.Info))
	out = append(out, addrBytes...)
	out = append(out, ctl...)
	if f.hasPID() {
		out = append(out, f.PID)
	}
	if f.hasInfo() {
		out = append(out, f.Info...)
	}
	return out, nil
}

// Decode parses connected-mode frame bytes.
func Decode(raw []byte, mod128 bool) (*Frame, error) {
	hdr, err := ax25.DecodeAddressBlock(raw)
	if err != nil {
		return nil, err
	}
	off := hdr.AddrLen
	ctlSize := 1
	if mod128 {
		ctlSize = 2
	}
	if len(raw) < off+ctlSize {
		return nil, fmt.Errorf("ax25conn: missing control field")
	}
	c, err := ParseControl(raw[off:off+ctlSize], mod128)
	if err != nil {
		return nil, err
	}
	off += ctlSize
	f := &Frame{
		Source:    hdr.Source,
		Dest:      hdr.Dest,
		Path:      hdr.Path,
		Control:   c,
		IsCommand: hdr.IsCommand,
		Mod128:    mod128,
	}
	if f.hasPID() {
		if off >= len(raw) {
			return nil, fmt.Errorf("ax25conn: missing PID")
		}
		f.PID = raw[off]
		off++
	}
	if f.hasInfo() {
		f.Info = append([]byte(nil), raw[off:]...)
	}
	return f, nil
}
