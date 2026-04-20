// Package ingress defines an in-process typed identifier for the origin
// of an RX frame flowing through the modem-RX fanout.
//
// Source provenance is threaded alongside *pb.ReceivedFrame so downstream
// subscribers (KISS broadcast, digipeater, APRS submit) can suppress
// feedback loops when a frame originated from a KISS-TNC interface rather
// than the local modem. The identity is deliberately kept in-process — it
// is not encoded on the wire protocol.
package ingress

// Kind enumerates the supported RX sources. New sources (SDR, AGW,
// Bluetooth TNC, etc.) get their own constant without churning the
// fanout signature.
type Kind uint8

const (
	// KindModem is a frame demodulated by the local modem bridge.
	KindModem Kind = iota + 1
	// KindKissTnc is a frame ingested from a KISS interface configured
	// in TNC mode (i.e. an off-air RX source, not a TX peer).
	KindKissTnc
)

// Source identifies where an RX frame entered graywolf. ID is the
// KissInterface DB row ID for KindKissTnc; unused (zero) for KindModem.
type Source struct {
	Kind Kind
	ID   uint32
}

// Modem returns a Source tagging a frame as coming from the local modem.
func Modem() Source { return Source{Kind: KindModem} }

// KissTnc returns a Source tagging a frame as coming from the KISS-TNC
// interface with the given DB row ID.
func KissTnc(ifaceID uint32) Source { return Source{Kind: KindKissTnc, ID: ifaceID} }

// IsKissTnc reports whether this Source is the KISS-TNC interface with
// the given ID. Used by the broadcast subscriber to suppress echo back
// to the originating interface.
func (s Source) IsKissTnc(id uint32) bool {
	return s.Kind == KindKissTnc && s.ID == id
}
