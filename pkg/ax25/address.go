// Package ax25 implements AX.25 v2.0 UI frame encode/decode.
//
// Only Unnumbered Information (UI) frames are supported — the workhorse
// for APRS, digipeater, and beaconing. Connected-mode frames (I, RR, RNR,
// REJ, SABM, DISC, UA, DM, FRMR) are recognised by control-field value
// but not otherwise implemented; see [Frame.IsUI].
package ax25

import (
	"errors"
	"fmt"
	"strings"
)

// An Address is a callsign + SSID pair, with AX.25 digipeater "has been
// repeated" (H) flag for path entries.
type Address struct {
	Call     string // 1..6 uppercase alphanumerics
	SSID     uint8  // 0..15
	Repeated bool   // H bit — only meaningful for digipeater path entries
}

// addrLen is the on-wire length of one encoded address.
const addrLen = 7

// ParseAddress parses "CALL[-SSID][*]" into an Address. The trailing '*'
// sets Repeated.
func ParseAddress(s string) (Address, error) {
	var a Address
	if s == "" {
		return a, errors.New("ax25: empty address")
	}
	if strings.HasSuffix(s, "*") {
		a.Repeated = true
		s = s[:len(s)-1]
	}
	call := s
	if i := strings.IndexByte(s, '-'); i >= 0 {
		call = s[:i]
		var ssid int
		if _, err := fmt.Sscanf(s[i+1:], "%d", &ssid); err != nil {
			return a, fmt.Errorf("ax25: bad ssid %q: %w", s[i+1:], err)
		}
		if ssid < 0 || ssid > 15 {
			return a, fmt.Errorf("ax25: ssid out of range: %d", ssid)
		}
		a.SSID = uint8(ssid)
	}
	if len(call) == 0 || len(call) > 6 {
		return a, fmt.Errorf("ax25: callsign length %d not in 1..6", len(call))
	}
	for i := 0; i < len(call); i++ {
		c := call[i]
		if !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') {
			return a, fmt.Errorf("ax25: invalid character %q in callsign", c)
		}
	}
	a.Call = strings.ToUpper(call)
	return a, nil
}

// String renders "CALL[-SSID][*]" (trailing '*' if Repeated is set).
func (a Address) String() string {
	var sb strings.Builder
	sb.WriteString(a.Call)
	if a.SSID != 0 {
		fmt.Fprintf(&sb, "-%d", a.SSID)
	}
	if a.Repeated {
		sb.WriteByte('*')
	}
	return sb.String()
}

// encode writes a into buf[0:7]. last indicates this is the final address
// (sets the end-of-address bit in the SSID byte). isRepeater selects the
// repeater SSID layout (H bit instead of C bit).
//
// AX.25 address byte layout:
//   bytes 0..5: callsign, left-justified, space-padded, shifted left by 1
//   byte 6:     CRRSSID1 / HRRSSID1 where
//                  bit 7 = C (dest/source) or H (repeater)
//                  bits 6,5 = RR (reserved, set to 1)
//                  bits 4..1 = SSID
//                  bit 0 = end-of-address marker (0 unless last)
func (a Address) encode(buf []byte, last bool, isRepeater bool, cBit bool) error {
	if len(buf) < addrLen {
		return errors.New("ax25: short address buffer")
	}
	if len(a.Call) == 0 || len(a.Call) > 6 {
		return fmt.Errorf("ax25: invalid callsign length %d", len(a.Call))
	}
	for i := 0; i < 6; i++ {
		var c byte = ' '
		if i < len(a.Call) {
			c = a.Call[i]
			if c >= 'a' && c <= 'z' {
				c -= 'a' - 'A'
			}
		}
		buf[i] = c << 1
	}
	ssid := byte(0x60) | ((a.SSID & 0x0F) << 1) // RR bits set to 1
	if isRepeater {
		if a.Repeated {
			ssid |= 0x80
		}
	} else if cBit {
		ssid |= 0x80
	}
	if last {
		ssid |= 0x01
	}
	buf[6] = ssid
	return nil
}

// decodeAddress parses 7 bytes into an Address and reports whether the
// end-of-address bit is set.
func decodeAddress(buf []byte) (Address, bool, error) {
	if len(buf) < addrLen {
		return Address{}, false, errors.New("ax25: short address")
	}
	var a Address
	call := make([]byte, 0, 6)
	for i := 0; i < 6; i++ {
		c := buf[i] >> 1
		if c == ' ' {
			continue
		}
		call = append(call, c)
	}
	a.Call = string(call)
	a.SSID = (buf[6] >> 1) & 0x0F
	a.Repeated = buf[6]&0x80 != 0 // interpretation depends on position; caller reinterprets for dest/src
	last := buf[6]&0x01 != 0
	return a, last, nil
}
