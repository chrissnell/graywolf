package aprs

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// parseTelemetry handles two forms:
//
//  1. Uncompressed "T#SSS,A1,A2,A3,A4,A5,DDDDDDDD<comment>"
//  2. Compressed base-91 "T|SS|A|B|C|D|E|F|"  (pipe-delimited, 2-char
//     base-91 channels)
func parseTelemetry(pkt *DecodedAPRSPacket, info []byte) error {
	if len(info) < 2 {
		return errors.New("aprs: telemetry too short")
	}
	if info[1] == '#' {
		return parseTelemetryUncompressed(pkt, info[2:])
	}
	// Compressed form uses '|' markers around base-91 channel pairs.
	if bytes.Index(info, []byte("|")) >= 0 {
		return parseTelemetryCompressed(pkt, info)
	}
	pkt.Comment = string(info)
	return nil
}

func parseTelemetryUncompressed(pkt *DecodedAPRSPacket, body []byte) error {
	// body: SSS,A1,A2,A3,A4,A5[,DDDDDDDD<optional comment>]
	// Relaxed form (Phire's proposal) allows fewer than 5 analog
	// channels, empty-field "no data", and floats. A lone '-' or
	// trailing '.' (not followed by digits) is rejected as tlm_inv.
	parts := strings.SplitN(string(body), ",", 7)
	if len(parts) < 2 {
		return errors.New("aprs: telemetry field count")
	}
	t := &Telemetry{Seq: -1}
	if n, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
		t.Seq = n
	}
	for i := 0; i < 5 && i+1 < len(parts); i++ {
		raw := strings.TrimSpace(parts[i+1])
		if raw == "" {
			continue // "no data" (undef in Perl)
		}
		// Perl treats a lone '-' or a number with trailing '.' and no
		// digits as invalid, returning tlm_inv. Detect both.
		if raw == "-" || strings.HasSuffix(raw, ".") {
			return errors.New("aprs: tlm_inv invalid numeric field")
		}
		// Partial parse: Perl stops at the first non-numeric field
		// (could be a comment) leaving later channels undef. We
		// mirror that by bailing as soon as ParseFloat fails.
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			break
		}
		t.Analog[i] = v
		t.AnalogHas[i] = true
	}
	if len(parts) == 7 {
		digAndRest := parts[6]
		// digital bits: up to 8 leading binary characters, remainder is
		// the comment.
		n := 0
		for n < len(digAndRest) && n < 8 && (digAndRest[n] == '0' || digAndRest[n] == '1') {
			if digAndRest[n] == '1' {
				t.Digital |= 1 << (7 - n)
			}
			n++
		}
		if n > 0 {
			t.HasDigital = true
		}
		t.Comment = strings.TrimSpace(digAndRest[n:])
	}
	pkt.Telemetry = t
	pkt.Type = PacketTelemetry
	return nil
}

// parseTelemetryCompressed decodes the "|ss|aa|bb|cc|dd|ee|" base-91
// compressed telemetry form. Each pair of printable base-91 characters
// encodes a value in 0..8280 (91*91-1). The '|' markers delimit blocks
// so they must appear at offsets 1, 4, 7, 10, 13, 16, 19, 22.
func parseTelemetryCompressed(pkt *DecodedAPRSPacket, info []byte) error {
	// Find opening '|' and take the next 20 bytes if available.
	start := bytes.Index(info, []byte("|"))
	if start < 0 || start+2+6*3+1 > len(info) {
		return errors.New("aprs: compressed telemetry too short")
	}
	// Structure: | SS | AA | BB | CC | DD | EE | (no spaces on the wire)
	// Flatten by stripping every '|' in the window.
	end := start
	blocks := make([][]byte, 0, 7)
	for end < len(info) {
		if info[end] != '|' {
			break
		}
		if end+3 > len(info) {
			break
		}
		blocks = append(blocks, info[end+1:end+3])
		end += 3
		if end < len(info) && info[end] == '|' {
			// closing bar of the last block
			end++
			break
		}
	}
	if len(blocks) < 6 {
		return errors.New("aprs: compressed telemetry block count")
	}
	t := &Telemetry{Seq: int(base91DecodeN(blocks[0]))}
	for i := 0; i < 5 && i+1 < len(blocks); i++ {
		t.Analog[i] = float64(base91DecodeN(blocks[i+1]))
	}
	if len(blocks) >= 7 {
		t.Digital = uint8(base91DecodeN(blocks[6]) & 0xFF)
	}
	pkt.Telemetry = t
	pkt.Type = PacketTelemetry
	return nil
}

// EncodeTelemetry builds the uncompressed "T#SSS,a1,a2,a3,a4,a5,DDDDDDDD"
// info field for a telemetry packet.
func EncodeTelemetry(t Telemetry) ([]byte, error) {
	seq := t.Seq
	if seq < 0 {
		seq = 0
	}
	if seq > 999 {
		seq = seq % 1000
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "T#%03d", seq)
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&sb, ",%d", int(t.Analog[i]))
	}
	sb.WriteByte(',')
	for i := 0; i < 8; i++ {
		if t.Digital&(1<<(7-i)) != 0 {
			sb.WriteByte('1')
		} else {
			sb.WriteByte('0')
		}
	}
	if t.Comment != "" {
		sb.WriteByte(' ')
		sb.WriteString(t.Comment)
	}
	return []byte(sb.String()), nil
}
