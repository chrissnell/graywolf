package messages

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLocalTxRingAddContains(t *testing.T) {
	r := NewLocalTxRing(4, time.Minute)
	r.Add("W1ABC-9", "001")
	if !r.Contains("W1ABC-9", "001") {
		t.Fatal("expected Contains to return true after Add")
	}
	if r.Contains("W1ABC-9", "002") {
		t.Fatal("expected Contains false for unseen msgid")
	}
	if r.Contains("N0CALL", "001") {
		t.Fatal("expected Contains false for unseen source")
	}
}

func TestLocalTxRingCaseInsensitiveSource(t *testing.T) {
	r := NewLocalTxRing(4, time.Minute)
	r.Add("w1abc", "007")
	if !r.Contains("W1ABC", "007") {
		t.Fatal("expected case-insensitive source match")
	}
	if !r.Contains("W1abc", "007") {
		t.Fatal("expected case-insensitive source match (mixed)")
	}
}

func TestLocalTxRingEmptyMsgIDIgnored(t *testing.T) {
	r := NewLocalTxRing(4, time.Minute)
	r.Add("W1ABC", "")
	if r.Len() != 0 {
		t.Fatalf("expected empty msgid to be rejected, len=%d", r.Len())
	}
	if r.Contains("W1ABC", "") {
		t.Fatal("Contains with empty msgid must return false")
	}
}

func TestLocalTxRingTTLEviction(t *testing.T) {
	r := NewLocalTxRing(4, time.Minute)
	base := time.Unix(1_000_000, 0)
	r.now = func() time.Time { return base }
	r.Add("W1ABC-9", "001")
	// Advance past TTL.
	r.now = func() time.Time { return base.Add(2 * time.Minute) }
	if r.Contains("W1ABC-9", "001") {
		t.Fatal("expected expired entry to be evicted")
	}
	if r.Len() != 0 {
		t.Fatalf("expected len=0 after TTL eviction, got %d", r.Len())
	}
}

func TestLocalTxRingCapacityEviction(t *testing.T) {
	r := NewLocalTxRing(3, time.Hour)
	for i := 0; i < 5; i++ {
		r.Add("W1ABC", fmt.Sprintf("%03d", i))
	}
	if r.Len() != 3 {
		t.Fatalf("expected len=3, got %d", r.Len())
	}
	// Oldest two should be gone.
	if r.Contains("W1ABC", "000") {
		t.Fatal("expected 000 to be evicted (oldest)")
	}
	if r.Contains("W1ABC", "001") {
		t.Fatal("expected 001 to be evicted")
	}
	for _, id := range []string{"002", "003", "004"} {
		if !r.Contains("W1ABC", id) {
			t.Fatalf("expected %s to remain", id)
		}
	}
}

func TestLocalTxRingRefreshMovesToTail(t *testing.T) {
	r := NewLocalTxRing(3, time.Hour)
	r.Add("W1ABC", "001")
	r.Add("W1ABC", "002")
	r.Add("W1ABC", "003")
	// Refresh 001 — should now be newest.
	r.Add("W1ABC", "001")
	// Adding a fourth entry should now evict 002 (new oldest), not 001.
	r.Add("W1ABC", "004")
	if r.Contains("W1ABC", "002") {
		t.Fatal("expected 002 to be evicted after 001 refresh")
	}
	if !r.Contains("W1ABC", "001") {
		t.Fatal("expected refreshed 001 to survive")
	}
}

func TestLocalTxRingConcurrentAccess(t *testing.T) {
	r := NewLocalTxRing(64, time.Hour)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				msgID := fmt.Sprintf("%03d", (id*200+i)%1000)
				r.Add("W1ABC", msgID)
				_ = r.Contains("W1ABC", msgID)
			}
		}(w)
	}
	wg.Wait()
	if r.Len() > 64 {
		t.Fatalf("ring exceeded cap: %d", r.Len())
	}
}

func TestLocalTxRingContainsEvictsExpiredBeforeReturn(t *testing.T) {
	r := NewLocalTxRing(4, time.Minute)
	base := time.Unix(1_000_000, 0)
	r.now = func() time.Time { return base }
	r.Add("W1ABC", "042")

	// First Contains: fresh — true.
	if !r.Contains("W1ABC", "042") {
		t.Fatal("expected fresh Contains to return true")
	}

	// Advance clock past TTL.
	r.now = func() time.Time { return base.Add(90 * time.Second) }
	if r.Contains("W1ABC", "042") {
		t.Fatal("expected expired Contains to return false")
	}
	if r.Len() != 0 {
		t.Fatalf("expected Contains to evict expired entry, len=%d", r.Len())
	}
}
