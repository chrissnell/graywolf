package aprs

import (
	"bytes"
	"errors"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

// ErrEmpty is returned when the AX.25 info field contains no APRS data.
var ErrEmpty = errors.New("aprs: empty info field")

// Parse decodes an AX.25 UI frame into a DecodedAPRSPacket. The frame
// must already be UI (f.IsUI() == true); connected-mode frames return
// an error.
//
// Parse is total: it never panics on malformed input. Fields for which
// no structured data could be extracted are left nil, and the packet
// type falls back to PacketUnknown (with the residual text in Comment).
func Parse(f *ax25.Frame) (*DecodedAPRSPacket, error) {
	if f == nil {
		return nil, errors.New("aprs: nil frame")
	}
	if !f.IsUI() {
		return nil, errors.New("aprs: non-UI frame")
	}
	pkt := &DecodedAPRSPacket{
		Type:      PacketUnknown,
		Timestamp: time.Now().UTC(),
	}
	pkt.FromAX25(f)
	// Preserve the wire bytes for downstream forwarding (iGate, logging).
	if enc, err := f.Encode(); err == nil {
		pkt.Raw = enc
	}
	if len(f.Info) == 0 {
		return pkt, ErrEmpty
	}
	info := f.Info
	if err := parseInfo(pkt, info, f); err != nil {
		return pkt, err
	}
	return pkt, nil
}

// ParseInfo decodes an APRS info field (the bytes after the AX.25 PID)
// without an enclosing frame. Source/Dest/Path on the returned packet
// are empty. Useful for testdata loaders.
func ParseInfo(info []byte) (*DecodedAPRSPacket, error) {
	pkt := &DecodedAPRSPacket{
		Type:      PacketUnknown,
		Timestamp: time.Now().UTC(),
	}
	if len(info) == 0 {
		return pkt, ErrEmpty
	}
	if err := parseInfo(pkt, info, nil); err != nil {
		return pkt, err
	}
	return pkt, nil
}

// parseInfo is the first-character dispatch described in the APRS spec
// (chapter 5 "APRS Data in AX.25 Packets"). It is best-effort: if a
// sub-parser fails, parseInfo falls back to PacketUnknown and stores the
// text in pkt.Comment so the caller still sees something useful.
//
// TODO (Phase 4+): APRS101 Table 5-1 lists ~30 prefix bytes. Still
// unhandled: '$' (raw GPS NMEA), '#' (Peet Bros U-II), '*' (Peet Bros
// complete), '%' (Agrelo DFjr), '&' (reserved), ',' (invalid/test),
// '[' (Maidenhead grid locator beacon), '{' (user-defined).
func parseInfo(pkt *DecodedAPRSPacket, info []byte, frame *ax25.Frame) error {
	return parseInfoDepth(pkt, info, frame, 0)
}

func parseInfoDepth(pkt *DecodedAPRSPacket, info []byte, frame *ax25.Frame, depth int) error {
	// Mic-E is special: the geographic data lives in the AX.25
	// destination address, not the info field's first byte.
	c := info[0]
	switch c {
	case '!', '=':
		// "!!" starts a Peet Bros Ultimeter logging frame, not a
		// position report.
		if c == '!' && len(info) >= 2 && info[1] == '!' {
			return parseUltwBang(pkt, info)
		}
		return parsePositionNoTS(pkt, info, false)
	case '/', '@':
		return parsePositionWithTS(pkt, info, c == '@')
	case ':':
		return parseMessage(pkt, info)
	case 'T':
		return parseTelemetry(pkt, info)
	case '_':
		return parseWeatherPositionless(pkt, info)
	case ';':
		return parseObject(pkt, info)
	case ')':
		return parseItem(pkt, info)
	case '\'', '`':
		return parseMicE(pkt, info, frame)
	case '>':
		return parseStatus(pkt, info)
	case '<':
		return parseCapabilities(pkt, info)
	case '$':
		return parseNMEA(pkt, info)
	case '?':
		pkt.Type = PacketQuery
		pkt.Comment = string(info[1:])
		return nil
	case '{':
		// APRS101 ch 18: user-defined experimental. We don't decode the
		// payload; store raw text as the comment and return.
		pkt.Type = PacketUnknown
		pkt.Comment = string(info)
		return nil
	case '}':
		// APRS101 ch 20: third-party traffic wraps a complete APRS
		// packet. Recursively decode the inner info field (bounded
		// depth to prevent pathological nesting).
		pkt.Type = PacketThirdParty
		pkt.Comment = string(info[1:])
		if depth >= 2 {
			return nil
		}
		// The wrapped content is "<src>><dest>,<path>:<info>". Split
		// at the first ':' to find the nested info field.
		body := info[1:]
		colon := bytes.IndexByte(body, ':')
		if colon < 0 || colon+1 >= len(body) {
			return nil
		}
		header := string(body[:colon])
		innerInfo := body[colon+1:]
		inner := &DecodedAPRSPacket{
			Type:      PacketUnknown,
			Timestamp: pkt.Timestamp,
		}
		// Best-effort header split "SRC>DEST,path".
		if gt := strings.IndexByte(header, '>'); gt >= 0 {
			inner.Source = header[:gt]
			tail := header[gt+1:]
			if comma := strings.IndexByte(tail, ','); comma >= 0 {
				inner.Dest = tail[:comma]
				inner.Path = strings.Split(tail[comma+1:], ",")
			} else {
				inner.Dest = tail
			}
		}
		if len(innerInfo) > 0 {
			_ = parseInfoDepth(inner, innerInfo, nil, depth+1)
		}
		pkt.ThirdParty = inner
		return nil
	}
	// Last-resort scan: some stations embed free-form text before a '!'
	// uncompressed position. Perl FAP scans the first 40 bytes for a
	// '!' that is followed by a valid DDMM.mm[NS] lat and re-dispatches
	// from there.
	if pos := lastResortBangScan(info); pos >= 0 {
		return parsePositionNoTS(pkt, info[pos:], false)
	}
	pkt.Comment = string(info)
	return nil
}

// lastResortBangScan searches the first 40 bytes of info for a '!' that
// is followed by what looks like an uncompressed position (DDMM.mmN or
// DDMM.mmS at offset +1..+8). Returns the byte offset of the '!', or
// -1 if no candidate found.
func lastResortBangScan(info []byte) int {
	limit := 40
	if len(info) < limit {
		limit = len(info)
	}
	for i := 0; i < limit; i++ {
		if info[i] != '!' {
			continue
		}
		if i+uncompressedPosLen >= len(info) {
			continue
		}
		body := info[i+1:]
		// Require digits in the expected latitude positions and a
		// hemisphere letter.
		if len(body) < 8 {
			continue
		}
		if !isDigit(body[0]) || !isDigit(body[1]) || !isDigit(body[2]) || !isDigit(body[3]) {
			continue
		}
		if body[4] != '.' {
			continue
		}
		if body[7] != 'N' && body[7] != 'S' {
			continue
		}
		return i
	}
	return -1
}

// parseStatus handles the '>' status report (APRS101 ch 16). Optional
// 7-byte DDHHMMz / HHMMSSh timestamp may precede the free-form status.
func parseStatus(pkt *DecodedAPRSPacket, info []byte) error {
	pkt.Type = PacketStatus
	body := info[1:]
	if len(body) >= 7 {
		// Accept only the timestamped form if byte 6 is one of the
		// recognized suffixes; anything else (e.g. Maidenhead grid)
		// stays in Status verbatim.
		if s := body[6]; s == 'z' || s == '/' || s == 'h' {
			if ts, err := parseAPRSTimestamp(body[:7]); err == nil && ts != nil {
				pkt.Timestamp = *ts
				body = body[7:]
			}
		}
	}
	pkt.Status = string(body)
	return nil
}
