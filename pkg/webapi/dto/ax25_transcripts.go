package dto

import "time"

// AX25TranscriptSession is the on-wire shape for the transcripts list.
// Body bytes are NOT included here -- fetch via /api/ax25/transcripts/{id}.
type AX25TranscriptSession struct {
	ID         uint32     `json:"id"`
	ChannelID  uint32     `json:"channel_id"`
	PeerCall   string     `json:"peer_call"`
	PeerSSID   uint8      `json:"peer_ssid"`
	ViaPath    string     `json:"via_path"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	EndReason  string     `json:"end_reason"`
	ByteCount  uint64     `json:"byte_count"`
	FrameCount uint64     `json:"frame_count"`
}

// AX25TranscriptEntry is one persisted line in a transcript.
type AX25TranscriptEntry struct {
	ID        uint64    `json:"id"`
	TS        time.Time `json:"ts"`
	Direction string    `json:"direction"` // rx|tx
	Kind      string    `json:"kind"`      // data|event
	Payload   []byte    `json:"payload"`
}

// AX25TranscriptDetail bundles a session row with its full entry list,
// served by GET /api/ax25/transcripts/{id}.
type AX25TranscriptDetail struct {
	Session AX25TranscriptSession `json:"session"`
	Entries []AX25TranscriptEntry `json:"entries"`
}
