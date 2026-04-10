package packetlog

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestRingBufferEviction(t *testing.T) {
	l := New(Config{Capacity: 4, MaxAge: time.Hour})
	for i := 0; i < 10; i++ {
		l.Record(Entry{
			Timestamp: time.Now().Add(time.Duration(i) * time.Millisecond),
			Source:    "test",
			Direction: DirRX,
			Display:   fmt.Sprintf("%d", i),
		})
	}
	if got := l.Len(); got != 4 {
		t.Fatalf("Len()=%d want 4", got)
	}
	entries := l.Query(Filter{})
	if len(entries) != 4 {
		t.Fatalf("query returned %d want 4", len(entries))
	}
	// Oldest retained should be "6".
	if entries[0].Display != "6" || entries[3].Display != "9" {
		t.Fatalf("unexpected retention: %s..%s", entries[0].Display, entries[3].Display)
	}
}

func TestFilters(t *testing.T) {
	l := New(Config{Capacity: 16, MaxAge: time.Hour})
	now := time.Now()
	l.Record(Entry{Timestamp: now, Source: "kiss", Direction: DirRX, Type: "position", Channel: 1})
	l.Record(Entry{Timestamp: now.Add(time.Second), Source: "digi", Direction: DirTX, Type: "message", Channel: 2})
	l.Record(Entry{Timestamp: now.Add(2 * time.Second), Source: "igate-is", Direction: DirIS, Type: "position", Channel: 1})

	cases := []struct {
		name string
		f    Filter
		want int
	}{
		{"all", Filter{}, 3},
		{"since", Filter{Since: now.Add(500 * time.Millisecond)}, 2},
		{"source", Filter{Source: "digi"}, 1},
		{"type position", Filter{Type: "position"}, 2},
		{"dir TX", Filter{Direction: DirTX}, 1},
		{"channel 1", Filter{Channel: 1}, 2},
		{"limit 1", Filter{Limit: 1}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := l.Query(c.f)
			if len(got) != c.want {
				t.Fatalf("got %d want %d", len(got), c.want)
			}
		})
	}
}

func TestMaxAgeGC(t *testing.T) {
	l := New(Config{Capacity: 16, MaxAge: 50 * time.Millisecond})
	old := time.Now().Add(-time.Second)
	l.Record(Entry{Timestamp: old, Source: "old"})
	// A subsequent fresh Record triggers GC.
	l.Record(Entry{Timestamp: time.Now(), Source: "new"})
	if got := l.Len(); got != 1 {
		t.Fatalf("Len=%d want 1 after GC", got)
	}
	if l.Query(Filter{})[0].Source != "new" {
		t.Fatalf("wrong entry retained")
	}
}

func TestConcurrentWritersAndReaders(t *testing.T) {
	l := New(Config{Capacity: 256, MaxAge: time.Hour})
	var writersWG, readersWG sync.WaitGroup
	const writers = 8
	const readers = 4
	const iters = 1000
	for w := 0; w < writers; w++ {
		writersWG.Add(1)
		go func(id int) {
			defer writersWG.Done()
			for i := 0; i < iters; i++ {
				l.Record(Entry{Source: fmt.Sprintf("w%d", id), Direction: DirRX})
			}
		}(w)
	}
	stop := make(chan struct{})
	for r := 0; r < readers; r++ {
		readersWG.Add(1)
		go func() {
			defer readersWG.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = l.Query(Filter{Limit: 10})
				}
			}
		}()
	}
	writersWG.Wait()
	close(stop)
	readersWG.Wait()
}

func TestStressMemoryStability(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	l := New(Config{Capacity: 1000, MaxAge: time.Hour})
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)
	for i := 0; i < 10000; i++ {
		l.Record(Entry{
			Source:    "stress",
			Direction: DirRX,
			Raw:       make([]byte, 256),
			Display:   fmt.Sprintf("pkt-%d", i),
		})
	}
	if l.Len() != 1000 {
		t.Fatalf("len=%d want 1000", l.Len())
	}
	runtime.GC()
	runtime.ReadMemStats(&m2)
	// Sanity: heap should not have grown unbounded. With cap=1000 and
	// 256-byte payloads, steady state is <2 MB; leave headroom.
	grew := int64(m2.HeapAlloc) - int64(m1.HeapAlloc)
	if grew > 10*1024*1024 {
		t.Fatalf("heap grew by %d bytes, expected bounded", grew)
	}
}
