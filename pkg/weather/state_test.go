package weather

import (
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

func TestUpsertAlert_Changed(t *testing.T) {
	s := NewStateStore()
	pkt := &aprs.DecodedAPRSPacket{Source: "PBZFFW"}

	// First insert: always treated as changed.
	changed := s.UpsertAlert("OHC019", AlertStatusWarning, "FLASH_FLOOD", pkt)
	if !changed {
		t.Error("expected changed=true on first insert")
	}

	// Same status and type: not changed.
	changed = s.UpsertAlert("OHC019", AlertStatusWarning, "FLASH_FLOOD", pkt)
	if changed {
		t.Error("expected changed=false when status and type are same")
	}

	// Status change: changed.
	changed = s.UpsertAlert("OHC019", AlertStatusWatch, "FLASH_FLOOD", pkt)
	if !changed {
		t.Error("expected changed=true on status change")
	}

	// Type change: changed.
	changed = s.UpsertAlert("OHC019", AlertStatusWatch, "SVR_STORM", pkt)
	if !changed {
		t.Error("expected changed=true on type change")
	}
}

func TestClearStaleAlerts(t *testing.T) {
	s := NewStateStore()
	pkt := &aprs.DecodedAPRSPacket{}

	s.UpsertAlert("OHC019", AlertStatusWarning, "FLASH_FLOOD", pkt)
	s.UpsertAlert("OHC029", AlertStatusWatch, "SVR_STORM", pkt)

	// Advance OHC019's LastSeen into the past so it falls before the cutoff.
	s.mu.Lock()
	s.alerts["OHC019"].LastSeen = time.Now().Add(-15 * time.Minute)
	s.mu.Unlock()

	cleared := s.ClearStaleAlerts(time.Now().Add(-10 * time.Minute))
	if len(cleared) != 1 || cleared[0] != "OHC019" {
		t.Errorf("expected OHC019 to be cleared, got %v", cleared)
	}
	// Entry must still exist, but status must be "clear".
	e := s.GetAlert("OHC019")
	if e == nil {
		t.Fatal("expected OHC019 entry to remain after clear")
	}
	if e.AlertStatus != AlertStatusClear {
		t.Errorf("expected AlertStatusClear, got %q", e.AlertStatus)
	}
	if e.AlertType != "" {
		t.Errorf("expected empty AlertType after clear, got %q", e.AlertType)
	}
	// OHC029 is still recent — must be untouched.
	if e2 := s.GetAlert("OHC029"); e2 == nil || e2.AlertStatus != AlertStatusWatch {
		t.Error("expected OHC029 to remain as watch")
	}
	// Already-clear entries must not appear in the cleared list on a second call.
	cleared2 := s.ClearStaleAlerts(time.Now().Add(-10 * time.Minute))
	if len(cleared2) != 0 {
		t.Errorf("expected no new clears on second call, got %v", cleared2)
	}
}

func TestSetLastTX(t *testing.T) {
	s := NewStateStore()
	s.UpsertAlert("OHC019", AlertStatusWarning, "FLASH_FLOOD", nil)
	tx := time.Now()
	s.SetLastTX("OHC019", tx)
	e := s.GetAlert("OHC019")
	if e == nil {
		t.Fatal("expected entry to exist")
	}
	if !e.LastTX.Equal(tx) {
		t.Errorf("LastTX = %v, want %v", e.LastTX, tx)
	}
}

func TestAlertSnapshot(t *testing.T) {
	s := NewStateStore()
	s.UpsertAlert("OHC019", AlertStatusWarning, "FLASH_FLOOD", nil)
	s.UpsertAlert("OHC029", AlertStatusWatch, "SVR_STORM", nil)

	snap := s.AlertSnapshot()
	if len(snap) != 2 {
		t.Errorf("expected 2 entries in snapshot, got %d", len(snap))
	}
}
