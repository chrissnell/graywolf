package webapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/igate"
)

// TestStatusDTO_WireShape_WithoutIgate confirms that the flattened
// StatusIgateDTO still omits the "igate" key entirely when the iGate is
// absent, matching the previous *igate.Status behavior.
func TestStatusDTO_WireShape_WithoutIgate(t *testing.T) {
	dto := StatusDTO{
		UptimeSeconds: 42,
		Channels:      []StatusChannel{},
	}
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["igate"]; ok {
		t.Fatalf("expected no igate key when nil; got %s", b)
	}
}

// TestStatusDTO_WireShape_WithIgate confirms that the JSON bytes emitted
// from StatusIgateDTO are byte-identical to what the previous embedded
// *igate.Status produced.
func TestStatusDTO_WireShape_WithIgate(t *testing.T) {
	lastConn := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	ig := igate.Status{
		Connected:      true,
		Server:         "rotate.aprs2.net:14580",
		Callsign:       "W1ABC-1",
		SimulationMode: false,
		LastConnected:  lastConn,
		Gated:          100,
		Downlinked:     5,
		Filtered:       20,
		DroppedOffline: 1,
	}

	// "Old" shape: embedded pointer would have marshaled ig directly.
	oldShape := struct {
		UptimeSeconds int64           `json:"uptime_seconds"`
		Channels      []StatusChannel `json:"channels"`
		Igate         *igate.Status   `json:"igate,omitempty"`
	}{
		UptimeSeconds: 42,
		Channels:      []StatusChannel{},
		Igate:         &ig,
	}
	oldBytes, err := json.Marshal(oldShape)
	if err != nil {
		t.Fatalf("marshal old: %v", err)
	}

	// New shape: local StatusIgateDTO projection.
	newDTO := StatusDTO{
		UptimeSeconds: 42,
		Channels:      []StatusChannel{},
		Igate:         newStatusIgateDTO(ig),
	}
	newBytes, err := json.Marshal(newDTO)
	if err != nil {
		t.Fatalf("marshal new: %v", err)
	}

	if string(oldBytes) != string(newBytes) {
		t.Fatalf("wire shape drift:\n old: %s\n new: %s", oldBytes, newBytes)
	}
}

// TestStatusDTO_WireShape_WithIgate_EmptyLastConnected confirms that a
// zero LastConnected is serialized the same way the previous embedded
// *igate.Status serialized it (encoding/json does not treat a zero
// time.Time as empty for omitempty, so the old shape emitted
// "0001-01-01T00:00:00Z"; the flattened type must match byte-for-byte).
func TestStatusDTO_WireShape_WithIgate_EmptyLastConnected(t *testing.T) {
	ig := igate.Status{
		Connected: false,
		Server:    "rotate.aprs2.net:14580",
		Callsign:  "W1ABC-1",
	}

	oldShape := struct {
		UptimeSeconds int64           `json:"uptime_seconds"`
		Channels      []StatusChannel `json:"channels"`
		Igate         *igate.Status   `json:"igate,omitempty"`
	}{
		UptimeSeconds: 1,
		Channels:      []StatusChannel{},
		Igate:         &ig,
	}
	oldBytes, err := json.Marshal(oldShape)
	if err != nil {
		t.Fatalf("marshal old: %v", err)
	}

	newDTO := StatusDTO{
		UptimeSeconds: 1,
		Channels:      []StatusChannel{},
		Igate:         newStatusIgateDTO(ig),
	}
	newBytes, err := json.Marshal(newDTO)
	if err != nil {
		t.Fatalf("marshal new: %v", err)
	}

	if string(oldBytes) != string(newBytes) {
		t.Fatalf("wire shape drift with zero LastConnected:\n old: %s\n new: %s", oldBytes, newBytes)
	}
}
