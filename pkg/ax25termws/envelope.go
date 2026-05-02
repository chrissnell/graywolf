// Package ax25termws bridges a per-WebSocket connection to a
// pkg/ax25conn session: inbound JSON envelopes from the browser drive
// the session's event loop; outbound observer events become envelopes
// pushed back to the browser.
package ax25termws

import "time"

// MsgKind enumerates wire-level message types. The wire is JSON; the
// kind value drives a switch in both directions.
type MsgKind string

const (
	// Client -> Server
	KindConnect       MsgKind = "connect"
	KindData          MsgKind = "data"
	KindDisconnect    MsgKind = "disconnect"
	KindAbort         MsgKind = "abort"
	KindTranscriptSet     MsgKind = "transcript_set"
	KindRawTailSubscribe  MsgKind = "raw_tail_subscribe"
	KindRawTailUnsub      MsgKind = "raw_tail_unsubscribe"

	// Server -> Client
	KindState     MsgKind = "state"
	KindDataRX    MsgKind = "data_rx"
	KindLinkStats MsgKind = "link_stats"
	KindError     MsgKind = "error"
	KindRawTail   MsgKind = "raw_tail"
)

// Envelope is the on-wire JSON shape. Data is base64-encoded by
// encoding/json when the field type is []byte; the JS side decodes
// via atob.
type Envelope struct {
	Kind       MsgKind               `json:"kind"`
	Connect    *ConnectArgs          `json:"connect,omitempty"`
	Data       []byte                `json:"data,omitempty"`
	State      *StatePayload         `json:"state,omitempty"`
	Stats      *StatsPayload         `json:"stats,omitempty"`
	Error      *ErrorPayload         `json:"error,omitempty"`
	Transcript *TranscriptSetPayload `json:"transcript,omitempty"`
	RawTailSub *RawTailSubscribeArgs `json:"raw_tail_sub,omitempty"`
	RawTail    *RawTailEntry         `json:"raw_tail,omitempty"`
}

// TranscriptSetPayload toggles transcript recording on/off for the
// current session. Sent client -> server only.
type TranscriptSetPayload struct {
	Enabled bool `json:"enabled"`
}

// RawTailSubscribeArgs scopes a raw-packet tail to one channel and
// optional Filter fields. Empty fields match anything.
type RawTailSubscribeArgs struct {
	ChannelID uint32 `json:"channel_id"`
	Source    string `json:"source,omitempty"`
	Type      string `json:"type,omitempty"`
	Direction string `json:"direction,omitempty"`
	// SubstringMatch narrows by matching against the Entry's Decoded.Source
	// (callsign) -- common operator-friendly filter.
	SubstringMatch string `json:"substring,omitempty"`
}

// RawTailEntry is the wire-form of a packetlog Entry, slimmed down to
// what RawPacketView renders. Server-only payload.
type RawTailEntry struct {
	TS        time.Time `json:"ts"`
	Source    string    `json:"source"`
	Type      string    `json:"type,omitempty"`
	Direction string    `json:"direction,omitempty"`
	ChannelID uint32    `json:"channel_id,omitempty"`
	From      string    `json:"from,omitempty"`
	Raw       string    `json:"raw,omitempty"` // TNC2-formatted packet
}

// ConnectArgs is the payload of a KindConnect envelope.
type ConnectArgs struct {
	ChannelID uint32   `json:"channel_id"`
	LocalCall string   `json:"local_call"`
	LocalSSID uint8    `json:"local_ssid"`
	DestCall  string   `json:"dest_call"`
	DestSSID  uint8    `json:"dest_ssid"`
	Via       []string `json:"via,omitempty"`
	Mod128    bool     `json:"mod128,omitempty"`
	Paclen    int      `json:"paclen,omitempty"`
	// Maxframe is the LAPB window k. Defaults: 2 (mod-8), 32 (mod-128).
	Maxframe int    `json:"maxframe,omitempty"`
	T1MS     int    `json:"t1_ms,omitempty"`
	T2MS     int    `json:"t2_ms,omitempty"`
	T3MS     int    `json:"t3_ms,omitempty"`
	N2       int    `json:"n2,omitempty"`
	Backoff  string `json:"backoff,omitempty"` // "none"|"linear"|"exponential"; default linear
}

// StatePayload reports a state transition.
type StatePayload struct {
	Name   string `json:"name"`             // DISCONNECTED, AWAITING_CONNECTION, CONNECTED, ...
	Reason string `json:"reason,omitempty"` // human-readable transition cause
}

// StatsPayload is the LinkStats snapshot the bridge ships to the
// telemetry side panel.
type StatsPayload struct {
	State    string `json:"state"`
	VS       uint8  `json:"vs"`
	VR       uint8  `json:"vr"`
	VA       uint8  `json:"va"`
	RC       int    `json:"rc"`
	PeerBusy bool   `json:"peer_busy"`
	OwnBusy  bool   `json:"own_busy"`
	FramesTX uint64 `json:"frames_tx"`
	FramesRX uint64 `json:"frames_rx"`
	BytesTX  uint64 `json:"bytes_tx"`
	BytesRX  uint64 `json:"bytes_rx"`
	RTTMS    int    `json:"rtt_ms"`
}

// ErrorPayload is a typed error surfaced back to the operator.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
