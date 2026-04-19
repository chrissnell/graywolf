package messages

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTacticalSetEmptyByDefault(t *testing.T) {
	s := NewTacticalSet()
	if s.Contains("NET") {
		t.Fatal("expected empty set to not contain NET")
	}
	if len(s.Load()) != 0 {
		t.Fatal("expected empty non-nil map on Load")
	}
}

func TestTacticalSetStoreContains(t *testing.T) {
	s := NewTacticalSet()
	s.Store(map[string]struct{}{
		"NET":     {},
		"EOC":     {},
		"ARES-WX": {},
	})
	for _, want := range []string{"NET", "EOC", "ARES-WX"} {
		if !s.Contains(want) {
			t.Fatalf("expected %s in set", want)
		}
	}
	if s.Contains("SKYWARN") {
		t.Fatal("unexpected membership for SKYWARN")
	}
}

func TestTacticalSetCaseInsensitive(t *testing.T) {
	s := NewTacticalSet()
	s.Store(map[string]struct{}{"net": {}})
	if !s.Contains("NET") {
		t.Fatal("expected case-insensitive Contains(NET)")
	}
	if !s.Contains("Net") {
		t.Fatal("expected case-insensitive Contains(Net)")
	}
	// Load returns normalized keys.
	m := s.Load()
	if _, ok := m["NET"]; !ok {
		t.Fatalf("expected Load to return uppercase key, got %v", m)
	}
}

func TestTacticalSetStoreReplacesSnapshot(t *testing.T) {
	s := NewTacticalSet()
	s.Store(map[string]struct{}{"NET": {}})
	old := s.Load()
	s.Store(map[string]struct{}{"EOC": {}})
	// Old snapshot still reads correctly.
	if _, ok := old["NET"]; !ok {
		t.Fatal("old snapshot lost NET")
	}
	// New snapshot reflects the replacement.
	if s.Contains("NET") {
		t.Fatal("replaced snapshot still contains NET")
	}
	if !s.Contains("EOC") {
		t.Fatal("replaced snapshot missing EOC")
	}
}

func TestTacticalSetNilStoreIsEmpty(t *testing.T) {
	s := NewTacticalSet()
	s.Store(map[string]struct{}{"NET": {}})
	s.Store(nil)
	if s.Contains("NET") {
		t.Fatal("nil Store should clear the set")
	}
	if s.Load() == nil {
		t.Fatal("Load must never return nil")
	}
}

func TestTacticalSetContainsOnEmptyString(t *testing.T) {
	s := NewTacticalSet()
	s.Store(map[string]struct{}{"NET": {}})
	if s.Contains("") {
		t.Fatal("empty string must not match any entry")
	}
	if s.Contains("   ") {
		t.Fatal("whitespace-only must not match any entry")
	}
}

// Concurrent-swap safety: readers see either the old or new snapshot,
// never a torn read. atomic.Pointer guarantees this; the test verifies
// no panics or data-race reports under -race.
func TestTacticalSetConcurrentReadsDuringStore(t *testing.T) {
	s := NewTacticalSet()
	s.Store(map[string]struct{}{"NET": {}})

	var done atomic.Bool
	var readOps atomic.Uint64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !done.Load() {
				_ = s.Contains("NET")
				readOps.Add(1)
			}
		}()
	}
	// Writer: swap snapshots repeatedly for a short duration.
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.Store(map[string]struct{}{"EOC": {}})
		s.Store(map[string]struct{}{"NET": {}, "EOC": {}})
	}
	done.Store(true)
	wg.Wait()
	if readOps.Load() == 0 {
		t.Fatal("expected reader goroutines to complete at least one read")
	}
}
