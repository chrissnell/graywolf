package ax25

import (
	"errors"
	"fmt"
)

// AX.25 control-field values.
const (
	// ControlUI is the unnumbered-information control byte (poll/final=0).
	ControlUI = 0x03
	// ControlUIWithPF is UI with the poll/final bit set.
	ControlUIWithPF = 0x13

	// PIDNoLayer3 is the APRS-standard Protocol Identifier.
	PIDNoLayer3 = 0xF0
)

// MaxRepeaters is the AX.25 maximum number of digipeater addresses in the
// path.
const MaxRepeaters = 8

// A Frame is a decoded AX.25 UI frame (or the header of an unsupported
// frame type). The zero value is not a valid frame.
type Frame struct {
	Dest    Address
	Source  Address
	Path    []Address // up to 8 digipeater addresses
	Control byte
	PID     byte
	Info    []byte

	// CommandResp encodes the C-bit layout: direwolf-style command = dest
	// C-bit set, source C-bit clear (AX.25 v2.0 "command" frame). The
	// default New* constructors produce a command frame.
	CommandResp bool
}

// NewUIFrame constructs a v2.0-command UI frame with the given path and
// payload. PID defaults to 0xF0 (no-layer-3, APRS).
func NewUIFrame(source, dest Address, path []Address, info []byte) (*Frame, error) {
	if len(path) > MaxRepeaters {
		return nil, fmt.Errorf("ax25: path length %d > %d", len(path), MaxRepeaters)
	}
	return &Frame{
		Dest:        dest,
		Source:      source,
		Path:        append([]Address(nil), path...),
		Control:     ControlUI,
		PID:         PIDNoLayer3,
		Info:        append([]byte(nil), info...),
		CommandResp: true,
	}, nil
}

// IsUI reports whether the frame is an Unnumbered Information frame.
// Connected-mode control bytes (I, RR, RNR, REJ, SABM/E, DISC, UA, DM,
// FRMR, XID, TEST) return false.
func (f *Frame) IsUI() bool {
	return f.Control == ControlUI || f.Control == ControlUIWithPF
}

// Encode serialises f into a byte slice suitable for passing as the Data
// field of a TransmitFrame IPC message (no FCS — the modem appends it).
func (f *Frame) Encode() ([]byte, error) {
	if !f.IsUI() {
		return nil, errors.New("ax25: Encode only supports UI frames")
	}
	if len(f.Path) > MaxRepeaters {
		return nil, fmt.Errorf("ax25: path length %d > %d", len(f.Path), MaxRepeaters)
	}
	addrBytes := addrLen * (2 + len(f.Path))
	out := make([]byte, 0, addrBytes+2+len(f.Info))
	out = append(out, make([]byte, addrBytes)...)

	lastIsPath := len(f.Path) > 0
	// Dest: C-bit set on command frames (v2.0 AX.25 convention).
	if err := f.Dest.encode(out[0:addrLen], false, false, f.CommandResp); err != nil {
		return nil, err
	}
	// Source: C-bit cleared on command frames. Last address iff no path.
	if err := f.Source.encode(out[addrLen:2*addrLen], !lastIsPath, false, !f.CommandResp); err != nil {
		return nil, err
	}
	// Path: repeater addresses carry the H bit.
	for i, a := range f.Path {
		last := i == len(f.Path)-1
		off := (2 + i) * addrLen
		if err := a.encode(out[off:off+addrLen], last, true, false); err != nil {
			return nil, err
		}
	}
	out = append(out, f.Control, f.PID)
	out = append(out, f.Info...)
	return out, nil
}

// Decode parses an AX.25 frame from raw bytes. Only UI frames have their
// info field populated; other control-field values parse the header and
// return a Frame with IsUI()==false and empty Info.
func Decode(raw []byte) (*Frame, error) {
	// Minimum frame: dest(7) + source(7) + control(1) = 15 bytes.
	if len(raw) < 2*addrLen+1 {
		return nil, fmt.Errorf("ax25: frame too short: %d bytes", len(raw))
	}
	f := &Frame{}

	// Dest.
	dest, last, err := decodeAddress(raw[0:addrLen])
	if err != nil {
		return nil, err
	}
	// For dest/source the top SSID bit encodes the C bit, not H. Fix up.
	destCBit := raw[6]&0x80 != 0
	dest.Repeated = false
	f.Dest = dest
	if last {
		return nil, errors.New("ax25: unexpected end-of-address after dest")
	}

	// Source.
	src, last, err := decodeAddress(raw[addrLen : 2*addrLen])
	if err != nil {
		return nil, err
	}
	srcCBit := raw[2*addrLen-1]&0x80 != 0
	src.Repeated = false
	f.Source = src
	f.CommandResp = destCBit && !srcCBit // v2.0 command

	// Path (zero or more repeater addresses).
	off := 2 * addrLen
	for !last {
		if len(f.Path) >= MaxRepeaters {
			return nil, errors.New("ax25: too many digipeater addresses")
		}
		if off+addrLen > len(raw) {
			return nil, errors.New("ax25: truncated path")
		}
		a, l, err := decodeAddress(raw[off : off+addrLen])
		if err != nil {
			return nil, err
		}
		// Path bytes carry the H bit in the top SSID position; decodeAddress
		// already populated Repeated from that bit.
		f.Path = append(f.Path, a)
		last = l
		off += addrLen
	}

	if off >= len(raw) {
		return nil, errors.New("ax25: missing control field")
	}
	f.Control = raw[off]
	off++

	if f.IsUI() {
		if off >= len(raw) {
			return nil, errors.New("ax25: missing PID")
		}
		f.PID = raw[off]
		off++
		f.Info = append([]byte(nil), raw[off:]...)
	}
	return f, nil
}

// String renders a direwolf-style monitor line: "SRC>DEST[,DIGI*,...]:info".
func (f *Frame) String() string {
	s := f.Source.String() + ">" + f.Dest.String()
	for _, p := range f.Path {
		s += "," + p.String()
	}
	if f.IsUI() && len(f.Info) > 0 {
		s += ":" + string(f.Info)
	}
	return s
}

// DedupKey returns a byte slice suitable as a map key for deduplication.
// Uses (dest + source + info) per the plan's txgovernor contract.
func (f *Frame) DedupKey() string {
	var b []byte
	b = append(b, f.Dest.Call...)
	b = append(b, f.Dest.SSID)
	b = append(b, f.Source.Call...)
	b = append(b, f.Source.SSID)
	b = append(b, f.Info...)
	return string(b)
}
