package ax25conn

import "fmt"

// FrameKind enumerates the AX.25 frame types decodable from the
// control field. See AX.25 v2.0 §3.4 / v2.2 §4.2.
type FrameKind uint8

const (
	FrameInvalid FrameKind = iota
	FrameI
	FrameRR
	FrameRNR
	FrameREJ
	FrameSREJ
	FrameSABM
	FrameSABME
	FrameDISC
	FrameDM
	FrameUA
	FrameFRMR
	FrameUI
	FrameXID
	FrameTEST
)

func (k FrameKind) String() string {
	switch k {
	case FrameI:
		return "I"
	case FrameRR:
		return "RR"
	case FrameRNR:
		return "RNR"
	case FrameREJ:
		return "REJ"
	case FrameSREJ:
		return "SREJ"
	case FrameSABM:
		return "SABM"
	case FrameSABME:
		return "SABME"
	case FrameDISC:
		return "DISC"
	case FrameDM:
		return "DM"
	case FrameUA:
		return "UA"
	case FrameFRMR:
		return "FRMR"
	case FrameUI:
		return "UI"
	case FrameXID:
		return "XID"
	case FrameTEST:
		return "TEST"
	}
	return "INVALID"
}

// Control is the parsed form of an AX.25 control field. NS/NR are
// in the range 0..7 for mod-8 and 0..127 for mod-128.
type Control struct {
	Kind FrameKind
	NS   uint8 // I-frames only
	NR   uint8 // I, RR, RNR, REJ, SREJ
	PF   bool  // poll/final
}

// Mod-8 control byte masks per AX.25 v2.0 §3.4.
const (
	mod8MaskNR = 0xE0
	mod8MaskPF = 0x10
	mod8MaskNS = 0x0E
)

// ParseControl decodes 1 byte (mod-8) or 2 bytes (mod-128) into a
// Control. Behavior follows AX.25 v2.0 §3.4 / v2.2 §4.2.
func ParseControl(b []byte, mod128 bool) (Control, error) {
	if mod128 {
		return parseControlMod128(b)
	}
	if len(b) < 1 {
		return Control{}, fmt.Errorf("ax25conn: short control")
	}
	c := Control{PF: b[0]&mod8MaskPF != 0}
	switch {
	case b[0]&0x01 == 0: // I-frame: low bit clear
		c.Kind = FrameI
		c.NS = (b[0] & mod8MaskNS) >> 1
		c.NR = (b[0] & mod8MaskNR) >> 5
	case b[0]&0x03 == 0x01: // S-frame: low two bits = 01
		c.NR = (b[0] & mod8MaskNR) >> 5
		switch b[0] & 0x0C { // bits 2-3 select S kind
		case 0x00:
			c.Kind = FrameRR
		case 0x04:
			c.Kind = FrameRNR
		case 0x08:
			c.Kind = FrameREJ
		case 0x0C:
			c.Kind = FrameSREJ
		}
	default: // U-frame: low two bits = 11
		switch b[0] &^ mod8MaskPF { // mask off P/F to get the kind
		case 0x2F:
			c.Kind = FrameSABM
		case 0x6F:
			c.Kind = FrameSABME
		case 0x43:
			c.Kind = FrameDISC
		case 0x0F:
			c.Kind = FrameDM
		case 0x63:
			c.Kind = FrameUA
		case 0x87:
			c.Kind = FrameFRMR
		case 0x03:
			c.Kind = FrameUI
		case 0xAF:
			c.Kind = FrameXID
		case 0xE3:
			c.Kind = FrameTEST
		default:
			return c, fmt.Errorf("ax25conn: unknown U-frame control 0x%02x", b[0])
		}
	}
	return c, nil
}

// EncodeControl renders a Control into 1 (mod-8) or 2 (mod-128) bytes.
// Returns an error on out-of-range NS/NR for the chosen modulus.
func EncodeControl(c Control, mod128 bool) ([]byte, error) {
	if mod128 {
		return encodeControlMod128(c)
	}
	var b byte
	if c.PF {
		b |= mod8MaskPF
	}
	switch c.Kind {
	case FrameI:
		if c.NS > 7 || c.NR > 7 {
			return nil, fmt.Errorf("ax25conn: mod-8 NS/NR overflow")
		}
		b |= (c.NR & 0x07) << 5
		b |= (c.NS & 0x07) << 1
		// low bit 0 already
	case FrameRR:
		if c.NR > 7 {
			return nil, fmt.Errorf("ax25conn: mod-8 NR overflow")
		}
		b |= ((c.NR & 0x07) << 5) | 0x01
	case FrameRNR:
		if c.NR > 7 {
			return nil, fmt.Errorf("ax25conn: mod-8 NR overflow")
		}
		b |= ((c.NR & 0x07) << 5) | 0x05
	case FrameREJ:
		if c.NR > 7 {
			return nil, fmt.Errorf("ax25conn: mod-8 NR overflow")
		}
		b |= ((c.NR & 0x07) << 5) | 0x09
	case FrameSREJ:
		if c.NR > 7 {
			return nil, fmt.Errorf("ax25conn: mod-8 NR overflow")
		}
		b |= ((c.NR & 0x07) << 5) | 0x0D
	case FrameSABM:
		b |= 0x2F
	case FrameSABME:
		b |= 0x6F
	case FrameDISC:
		b |= 0x43
	case FrameDM:
		b |= 0x0F
	case FrameUA:
		b |= 0x63
	case FrameFRMR:
		b |= 0x87
	case FrameUI:
		b |= 0x03
	case FrameXID:
		b |= 0xAF
	case FrameTEST:
		b |= 0xE3
	default:
		return nil, fmt.Errorf("ax25conn: cannot encode FrameKind %v", c.Kind)
	}
	return []byte{b}, nil
}

// parseControlMod128 / encodeControlMod128 stubs (Phase 3 fills in).
func parseControlMod128(b []byte) (Control, error) {
	return Control{}, fmt.Errorf("ax25conn: mod-128 not yet supported")
}
func encodeControlMod128(c Control) ([]byte, error) {
	return nil, fmt.Errorf("ax25conn: mod-128 not yet supported")
}
