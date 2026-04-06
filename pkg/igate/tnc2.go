package igate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
)

// encodeTNC2 renders a decoded APRS packet as a TNC2-monitor line suitable
// for APRS-IS uplink, WITHOUT the trailing CRLF. The qAR construct and
// iGate callsign are appended by the caller (the APRS-IS convention is
// to replace the last path element or append ",qAR,IGATECALL" before the
// colon).
func encodeTNC2(pkt *aprs.DecodedAPRSPacket, igateCall string) (string, error) {
	if pkt == nil {
		return "", errors.New("igate: nil packet")
	}
	if pkt.Source == "" || pkt.Dest == "" {
		return "", errors.New("igate: packet missing source or dest")
	}
	info := infoBytes(pkt)
	if len(info) == 0 {
		return "", errors.New("igate: packet has no info field")
	}
	var b strings.Builder
	b.WriteString(pkt.Source)
	b.WriteByte('>')
	b.WriteString(pkt.Dest)
	for _, p := range pkt.Path {
		if p == "" {
			continue
		}
		// Strip any trailing '*' H-bit markers; APRS-IS wants the
		// unmarked path with a qAR construct appended.
		p = strings.TrimSuffix(p, "*")
		b.WriteByte(',')
		b.WriteString(p)
	}
	b.WriteString(",qAR,")
	b.WriteString(igateCall)
	b.WriteByte(':')
	b.Write(info)
	return b.String(), nil
}

// infoBytes returns the AX.25 info field for a decoded packet. Prefer
// the original Raw frame's info (lossless) and fall back to re-parsing
// Raw when present.
func infoBytes(pkt *aprs.DecodedAPRSPacket) []byte {
	if len(pkt.Raw) > 0 {
		if f, err := ax25.Decode(pkt.Raw); err == nil && len(f.Info) > 0 {
			return f.Info
		}
	}
	return nil
}

// parseTNC2 parses a TNC2-monitor line from APRS-IS into an ax25.Frame.
// APRS-IS lines look like "SRC>DEST,PATH,qAC,SERVER:info". The qA*
// constructs and anything past them are stripped from the path before
// building an RF-transmittable frame. Returns an error on any malformed
// input.
func parseTNC2(line string) (*ax25.Frame, error) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return nil, errors.New("igate: empty line")
	}
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return nil, errors.New("igate: tnc2 line missing ':' info separator")
	}
	header := line[:colon]
	info := line[colon+1:]
	if info == "" {
		return nil, errors.New("igate: tnc2 line has empty info field")
	}
	gt := strings.IndexByte(header, '>')
	if gt <= 0 {
		return nil, errors.New("igate: tnc2 line missing '>' source/dest separator")
	}
	srcStr := header[:gt]
	rest := header[gt+1:]
	parts := strings.Split(rest, ",")
	if len(parts) == 0 || parts[0] == "" {
		return nil, errors.New("igate: tnc2 line missing dest")
	}
	destStr := parts[0]
	var pathStrs []string
	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		// APRS-IS q-construct ("qAC", "qAR", "qAO", etc.) terminates
		// the routable path; everything from here on is server trace.
		if len(p) >= 2 && p[0] == 'q' && p[1] == 'A' {
			break
		}
		pathStrs = append(pathStrs, p)
	}
	src, err := ax25.ParseAddress(srcStr)
	if err != nil {
		return nil, fmt.Errorf("igate: parse source: %w", err)
	}
	dest, err := ax25.ParseAddress(destStr)
	if err != nil {
		return nil, fmt.Errorf("igate: parse dest: %w", err)
	}
	path := make([]ax25.Address, 0, len(pathStrs))
	for _, p := range pathStrs {
		a, err := ax25.ParseAddress(p)
		if err != nil {
			return nil, fmt.Errorf("igate: parse path %q: %w", p, err)
		}
		// For IS->RF we clear the H bit: the local digipeater (if any)
		// needs to treat the path as unrepeated.
		a.Repeated = false
		path = append(path, a)
	}
	return ax25.NewUIFrame(src, dest, path, []byte(info))
}
