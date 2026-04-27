package igate

import (
	"bytes"
	"errors"
	"strings"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

// aprsGatewayToCall is graywolf's assigned APRS "tocall" — the AX.25
// destination address used for packets the iGate originates itself
// (including IS→RF third-party wrappers). The 6-char uppercase
// alphanumeric form is valid per the APRS tocall registry convention.
const aprsGatewayToCall = "APGWLF"

// wrapThirdParty takes an APRS-IS frame inner and returns a new AX.25
// frame that carries inner as an APRS "third-party" payload.
//
// Per the APRS101 spec (§ 20) and aprsc's IGATE-HINTS, any packet the
// iGate sends out onto RF from APRS-IS MUST be wrapped in the
// third-party format so other iGates and client apps can see the
// packet originated on the internet side. The outer AX.25 frame is
// sourced from the iGate's own call (so other iGates don't conclude
// the original sender is local RF) with an empty digipeater path
// (IS→RF targets stations in direct RF range of the iGate). The inner
// payload follows the format:
//
//	}origSrc>origDest[,origPath…],TCPIP,IGATECALL*:origInfo
//
// The inner "TCPIP,IGATECALL*" marker is what other iGates look at to
// decide "I already have this from the net — do not re-gate".
func wrapThirdParty(inner *ax25.Frame, igateCall string) (*ax25.Frame, error) {
	if inner == nil {
		return nil, errors.New("igate: wrapThirdParty: nil frame")
	}
	igateCall = strings.TrimSpace(igateCall)
	if igateCall == "" {
		return nil, errors.New("igate: wrapThirdParty: empty igate callsign")
	}
	outerSrc, err := ax25.ParseAddress(igateCall)
	if err != nil {
		return nil, err
	}
	outerDest, err := ax25.ParseAddress(aprsGatewayToCall)
	if err != nil {
		return nil, err
	}

	// Build the inner header string "origSrc>origDest[,origPath…]".
	var hdr bytes.Buffer
	hdr.WriteByte('}')
	hdr.WriteString(inner.Source.String())
	hdr.WriteByte('>')
	hdr.WriteString(inner.Dest.String())
	for _, a := range inner.Path {
		// Strip the H bit for the serialized form; the third-party
		// inner path is informational and the '*' marker would
		// confuse downstream parsers.
		a.Repeated = false
		hdr.WriteByte(',')
		hdr.WriteString(a.String())
	}
	hdr.WriteString(",TCPIP,")
	hdr.WriteString(igateCall)
	hdr.WriteString("*:")

	// Concatenate header + original info bytes. Use a byte slice copy
	// so binary (e.g. NUL) bytes in the info field survive unchanged.
	info := make([]byte, 0, hdr.Len()+len(inner.Info))
	info = append(info, hdr.Bytes()...)
	info = append(info, inner.Info...)

	// IS→RF third-party packets target stations in direct RF range of
	// the iGate; no digipeater path is included per IGATE-HINTS.
	return ax25.NewUIFrame(outerSrc, outerDest, nil, info)
}
