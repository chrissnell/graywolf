package dto

import "time"

// AX25SessionProfile is the on-wire shape of GET / POST / PUT
// /api/ax25/profiles. Pinned + LastUsed are read-only on POST/PUT;
// promote with POST /api/ax25/profiles/{id}/pin and update LastUsed
// via the WebSocket bridge's CONNECTED hook.
type AX25SessionProfile struct {
	ID        uint32     `json:"id"`
	Name      string     `json:"name"`
	LocalCall string     `json:"local_call"`
	LocalSSID uint8      `json:"local_ssid"`
	DestCall  string     `json:"dest_call"`
	DestSSID  uint8      `json:"dest_ssid"`
	ViaPath   string     `json:"via_path"`
	Mod128    bool       `json:"mod128"`
	Paclen    uint32     `json:"paclen"`
	Maxframe  uint32     `json:"maxframe"`
	T1MS      uint32     `json:"t1_ms"`
	T2MS      uint32     `json:"t2_ms"`
	T3MS      uint32     `json:"t3_ms"`
	N2        uint32     `json:"n2"`
	ChannelID *uint32    `json:"channel_id,omitempty"`
	Pinned    bool       `json:"pinned"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
}

// AX25SessionProfilePin is the on-wire body for POST
// /api/ax25/profiles/{id}/pin.
type AX25SessionProfilePin struct {
	Pinned bool `json:"pinned"`
}
