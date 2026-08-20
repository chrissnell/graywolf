package weather

import (
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// AlertEntry is the live alert state for one NWS county zone code.
type AlertEntry struct {
	NWSCode     string
	AlertStatus string // "clear" | "watch" | "warning"
	AlertType   string // e.g. "FLASH_FLOOD"; "" when status is "clear"
	LastSeen    time.Time
	LastTX      time.Time
	OrigPacket  *aprs.DecodedAPRSPacket // the most recent raw packet for re-transmission
}

// PositionEntry is a live position event from an NWS WFO object.
type PositionEntry struct {
	WFO         string
	AlertType   string
	AlertStatus string
	Lat         float64
	Lon         float64
	LastSeen    time.Time
	LastTX      time.Time
	OrigPacket  *aprs.DecodedAPRSPacket
}

// StateStore holds the in-memory NWS alert state. It is safe for concurrent use.
type StateStore struct {
	mu        sync.RWMutex
	alerts    map[string]*AlertEntry    // keyed by NWS code
	positions map[string]*PositionEntry // keyed by WFO+alertType
}

func NewStateStore() *StateStore {
	return &StateStore{
		alerts:    make(map[string]*AlertEntry),
		positions: make(map[string]*PositionEntry),
	}
}

// UpsertAlert updates the alert state for a county zone code. Returns true
// when the alert status or type changed, which signals an immediate TX bypass.
func (s *StateStore) UpsertAlert(nwsCode, status, alertType string, pkt *aprs.DecodedAPRSPacket) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.alerts[nwsCode]
	if !ok {
		// New entry — treat as changed so the first packet can be forwarded.
		s.alerts[nwsCode] = &AlertEntry{
			NWSCode:     nwsCode,
			AlertStatus: status,
			AlertType:   alertType,
			LastSeen:    time.Now(),
			OrigPacket:  pkt,
		}
		return true
	}
	changed = existing.AlertStatus != status || existing.AlertType != alertType
	existing.AlertStatus = status
	existing.AlertType = alertType
	existing.LastSeen = time.Now()
	existing.OrigPacket = pkt
	return changed
}

// UpsertPosition updates the position state for a WFO+alertType pair.
// Returns true when the position or alert type changed.
func (s *StateStore) UpsertPosition(wfo, alertType, status string, lat, lon float64, pkt *aprs.DecodedAPRSPacket) (changed bool) {
	key := wfo + ":" + alertType
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.positions[key]
	if !ok {
		s.positions[key] = &PositionEntry{
			WFO:         wfo,
			AlertType:   alertType,
			AlertStatus: status,
			Lat:         lat,
			Lon:         lon,
			LastSeen:    time.Now(),
			OrigPacket:  pkt,
		}
		return true
	}
	changed = existing.AlertType != alertType || existing.AlertStatus != status
	existing.AlertType = alertType
	existing.AlertStatus = status
	existing.Lat = lat
	existing.Lon = lon
	existing.LastSeen = time.Now()
	existing.OrigPacket = pkt
	return changed
}

// GetAlert returns the current alert state for a county zone code, or nil.
func (s *StateStore) GetAlert(nwsCode string) *AlertEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e := s.alerts[nwsCode]
	if e == nil {
		return nil
	}
	cp := *e
	return &cp
}

// SetLastTX records the most recent RF transmission time for a county alert.
func (s *StateStore) SetLastTX(nwsCode string, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.alerts[nwsCode]; ok {
		e.LastTX = t
	}
}

// SetPositionLastTX records the most recent RF transmission time for a position event.
func (s *StateStore) SetPositionLastTX(wfo, alertType string, t time.Time) {
	key := wfo + ":" + alertType
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.positions[key]; ok {
		e.LastTX = t
	}
}

// AlertSnapshot returns a copy of all current county alert entries.
func (s *StateStore) AlertSnapshot() []AlertEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AlertEntry, 0, len(s.alerts))
	for _, e := range s.alerts {
		out = append(out, *e)
	}
	return out
}

// PositionSnapshot returns a copy of all current position entries.
func (s *StateStore) PositionSnapshot() []PositionEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PositionEntry, 0, len(s.positions))
	for _, e := range s.positions {
		out = append(out, *e)
	}
	return out
}

// ClearStaleAlerts transitions active alerts to "clear" when they have not
// been refreshed since cutoff. Entries are kept in the map so the table
// continues to show them with their last-heard timestamp.
func (s *StateStore) ClearStaleAlerts(cutoff time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cleared []string
	for code, e := range s.alerts {
		if e.LastSeen.Before(cutoff) && e.AlertStatus != AlertStatusClear {
			e.AlertStatus = AlertStatusClear
			e.AlertType = ""
			cleared = append(cleared, code)
		}
	}
	return cleared
}

// ExpirePositions removes position entries not refreshed since cutoff.
func (s *StateStore) ExpirePositions(cutoff time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, e := range s.positions {
		if e.LastSeen.Before(cutoff) {
			delete(s.positions, key)
		}
	}
}
