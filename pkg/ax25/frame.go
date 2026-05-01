package ax25

import (
	"errors"
	"fmt"
	"strings"
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

// A Frame is a decoded AX.25 UI or connected-mode frame. The zero value
// is not a valid frame.
//
// UI frames carry [Frame.Control] (single byte) plus PID/Info.
// Connected-mode frames (I, RR, RNR, REJ, SABM/E, DISC, UA, DM,
// FRMR, XID, TEST) populate [Frame.ConnectedControl] (1 byte mod-8,
// 2 bytes mod-128) and leave Control unused; PID and Info are only
// set when [Frame.ConnectedHasInfo] is true. pkg/ax25conn produces
// connected-mode Frames via [ax25conn.Frame.ToAX25Frame] so the
// txgovernor send path, hook surface, and dedup keying continue to
// observe a single Frame surface.
type Frame struct {
	Dest    Address
	Source  Address
	Path    []Address // up to 8 digipeater addresses
	Control byte      // UI control byte (used iff ConnectedControl == nil)
	PID     byte
	Info    []byte

	// CommandResp encodes the C-bit layout: direwolf-style command = dest
	// C-bit set, source C-bit clear (AX.25 v2.0 "command" frame). The
	// default New* constructors produce a command frame.
	CommandResp bool

	// ConnectedControl, when non-empty, is the wire-encoded control
	// field for a connected-mode frame (1 byte mod-8 / 2 bytes
	// mod-128). UI Control is ignored when this field is set.
	ConnectedControl []byte

	// ConnectedHasInfo indicates that PID and Info should be appended
	// after ConnectedControl. True for I-frames and the v2.2 informational
	// U-frames (FRMR, XID, TEST); false for RR/RNR/REJ/SREJ/SABM/E,
	// DISC, UA, DM.
	ConnectedHasInfo bool
}

// IsConnectedMode reports whether f represents a connected-mode (LAPB)
// frame produced by pkg/ax25conn rather than a UI frame.
func (f *Frame) IsConnectedMode() bool { return len(f.ConnectedControl) > 0 }

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
	addrBytes, err := EncodeAddressBlock(f.Source, f.Dest, f.Path, f.CommandResp)
	if err != nil {
		return nil, err
	}
	if f.IsConnectedMode() {
		out := make([]byte, 0, len(addrBytes)+len(f.ConnectedControl)+1+len(f.Info))
		out = append(out, addrBytes...)
		out = append(out, f.ConnectedControl...)
		if f.ConnectedHasInfo {
			out = append(out, f.PID)
			out = append(out, f.Info...)
		}
		return out, nil
	}
	if !f.IsUI() {
		return nil, errors.New("ax25: Encode only supports UI or connected-mode frames")
	}
	out := make([]byte, 0, len(addrBytes)+2+len(f.Info))
	out = append(out, addrBytes...)
	out = append(out, f.Control, f.PID)
	out = append(out, f.Info...)
	return out, nil
}

// AddressBlock holds the parsed address header of an AX.25 frame
// (destination + source + digipeater path).
type AddressBlock struct {
	Source, Dest Address
	Path         []Address
	IsCommand    bool
	AddrLen      int
}

// EncodeAddressBlock writes the AX.25 address header (dest, source, path)
// for both UI and connected-mode frames. isCommand selects v2.0
// command-frame C-bit polarity (dest C set, source C clear).
func EncodeAddressBlock(src, dst Address, path []Address, isCommand bool) ([]byte, error) {
	if len(path) > MaxRepeaters {
		return nil, fmt.Errorf("ax25: path length %d > %d", len(path), MaxRepeaters)
	}
	total := addrLen * (2 + len(path))
	out := make([]byte, total)
	lastIsPath := len(path) > 0
	if err := dst.encode(out[0:addrLen], false, false, isCommand); err != nil {
		return nil, err
	}
	if err := src.encode(out[addrLen:2*addrLen], !lastIsPath, false, !isCommand); err != nil {
		return nil, err
	}
	for i, a := range path {
		last := i == len(path)-1
		off := (2 + i) * addrLen
		if err := a.encode(out[off:off+addrLen], last, true, false); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Decode parses an AX.25 frame from raw bytes. Only UI frames have their
// info field populated; other control-field values parse the header and
// return a Frame with IsUI()==false and empty Info.
func Decode(raw []byte) (*Frame, error) {
	hdr, err := DecodeAddressBlock(raw)
	if err != nil {
		return nil, err
	}
	off := hdr.AddrLen
	if off >= len(raw) {
		return nil, errors.New("ax25: missing control field")
	}
	f := &Frame{
		Dest:        hdr.Dest,
		Source:      hdr.Source,
		Path:        hdr.Path,
		CommandResp: hdr.IsCommand,
		Control:     raw[off],
	}
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

// DecodeAddressBlock parses dest+source+path from raw, returning the
// number of bytes consumed in AddrLen.
func DecodeAddressBlock(raw []byte) (AddressBlock, error) {
	// Minimum address block: dest(7) + source(7) = 14 bytes.
	if len(raw) < 2*addrLen {
		return AddressBlock{}, fmt.Errorf("ax25: frame too short: %d bytes", len(raw))
	}
	var ab AddressBlock

	dest, last, err := decodeAddress(raw[0:addrLen])
	if err != nil {
		return ab, err
	}
	destCBit := raw[6]&0x80 != 0
	dest.Repeated = false
	ab.Dest = dest
	if last {
		return ab, errors.New("ax25: unexpected end-of-address after dest")
	}

	src, last, err := decodeAddress(raw[addrLen : 2*addrLen])
	if err != nil {
		return ab, err
	}
	srcCBit := raw[2*addrLen-1]&0x80 != 0
	src.Repeated = false
	ab.Source = src
	ab.IsCommand = destCBit && !srcCBit

	off := 2 * addrLen
	for !last {
		if len(ab.Path) >= MaxRepeaters {
			return ab, errors.New("ax25: too many digipeater addresses")
		}
		if off+addrLen > len(raw) {
			return ab, errors.New("ax25: truncated path")
		}
		a, l, err := decodeAddress(raw[off : off+addrLen])
		if err != nil {
			return ab, err
		}
		ab.Path = append(ab.Path, a)
		last = l
		off += addrLen
	}
	ab.AddrLen = off
	return ab, nil
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

// DedupKey returns a string suitable as a map key for deduplication
// at the AX.25 frame level. Uses (dest + source + info) so identical
// content from the same source to the same destination collapses
// regardless of how the frame was routed or which digipeaters it
// traversed. This is the key the centralized TX governor uses to
// prevent the same frame being queued twice in rapid succession.
//
// Call PathDedupKey instead when the path matters, e.g. the
// digipeater's own duplicate-suppression map where two copies of the
// same payload arriving over different geographic paths should be
// treated as distinct events.
func (f *Frame) DedupKey() string {
	var b []byte
	b = append(b, f.Dest.Call...)
	b = append(b, f.Dest.SSID)
	b = append(b, f.Source.Call...)
	b = append(b, f.Source.SSID)
	b = append(b, f.Info...)
	return string(b)
}

// PathDedupKey returns a dedup key that includes the digipeater path.
// Used by the digipeater: two copies of the same payload heard via
// different unconsumed path slots are not the same observation for
// the purposes of digi suppression, because digi-ing them both could
// extend a packet's geographic reach legitimately. Only the call and
// SSID of each path element contribute; the repeated (H) bit is
// deliberately omitted so an unconsumed-then-consumed pair still
// dedups (the payload is the same; only the H-bit changed as we
// digi'd it).
func (f *Frame) PathDedupKey() string {
	var sb strings.Builder
	sb.WriteString(f.Source.String())
	sb.WriteByte('>')
	sb.WriteString(f.Dest.String())
	for _, p := range f.Path {
		sb.WriteByte(',')
		sb.WriteString(p.Call)
		sb.WriteByte('-')
		sb.WriteByte(byte('0' + p.SSID))
	}
	sb.WriteByte(':')
	sb.Write(f.Info)
	return sb.String()
}
