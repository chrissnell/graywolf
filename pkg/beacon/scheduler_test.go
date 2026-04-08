package beacon

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/gps"
)

// mockSink captures submitted frames for assertions.
type mockSink struct {
	mu     sync.Mutex
	frames []*ax25.Frame
	done   chan struct{}
	want   int
}

func newMockSink(want int) *mockSink {
	return &mockSink{done: make(chan struct{}), want: want}
}

func (m *mockSink) Submit(_ context.Context, _ uint32, f *ax25.Frame, _ SubmitSource) error {
	m.mu.Lock()
	m.frames = append(m.frames, f)
	reached := len(m.frames) >= m.want
	m.mu.Unlock()
	if reached {
		select {
		case <-m.done:
		default:
			close(m.done)
		}
	}
	return nil
}

func (m *mockSink) Frames() []*ax25.Frame {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*ax25.Frame(nil), m.frames...)
}

// countingObserver records metric callbacks.
type countingObserver struct {
	sent atomic.Int64
	rate atomic.Int64
}

func (c *countingObserver) OnBeaconSent(_ Type)                      { c.sent.Add(1) }
func (c *countingObserver) OnSmartBeaconRate(_ uint32, _ time.Duration) { c.rate.Add(1) }

func mustAddr(t *testing.T, s string) ax25.Address {
	t.Helper()
	a, err := ax25.ParseAddress(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return a
}

// TestScheduler_PositionBeacon_InitialDelayThenPeriodic verifies that
// a position beacon sends at Delay then every Every seconds.
func TestScheduler_PositionBeacon(t *testing.T) {
	sink := newMockSink(2)
	obs := &countingObserver{}
	logger := slog.New(slog.NewTextHandler(logSink{}, nil))
	s, err := New(Options{Sink: sink, Logger: logger, Observer: obs})
	if err != nil {
		t.Fatal(err)
	}
	s.SetBeacons([]Config{{
		ID:          1,
		Type:        TypePosition,
		Channel:     0,
		Source:      mustAddr(t, "N0CALL-9"),
		Dest:        mustAddr(t, "APGW00"),
		Path:        []ax25.Address{mustAddr(t, "WIDE1-1")},
		Delay:       20 * time.Millisecond,
		Every:       50 * time.Millisecond,
		Slot:        -1,
		Lat:         37.7749,
		Lon:         -122.4194,
		SymbolTable: '/',
		SymbolCode:  '-',
		Comment:     "hello",
		Enabled:     true,
	}})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go s.Run(ctx)

	select {
	case <-sink.done:
	case <-ctx.Done():
		t.Fatalf("timeout waiting for beacons; got %d", len(sink.Frames()))
	}
	cancel()

	frames := sink.Frames()
	if len(frames) < 2 {
		t.Fatalf("got %d frames, want >=2", len(frames))
	}
	info := string(frames[0].Info)
	if !strings.HasPrefix(info, "!") {
		t.Errorf("expected position prefix, got %q", info)
	}
	if !strings.Contains(info, "hello") {
		t.Errorf("comment missing from %q", info)
	}
	if obs.sent.Load() < 2 {
		t.Errorf("observer sent count = %d", obs.sent.Load())
	}
}

// TestScheduler_TrackerFromGPS verifies that a tracker beacon sources
// lat/lon/speed/heading from the GPS cache.
func TestScheduler_TrackerFromGPS(t *testing.T) {
	sink := newMockSink(1)
	cache := gps.NewMemCache()
	cache.Update(gps.Fix{
		Latitude: 47.6062, Longitude: -122.3321,
		Speed: 42, Heading: 90, HasCourse: true,
		HasAlt: true, Altitude: 100,
	})
	logger := slog.New(slog.NewTextHandler(logSink{}, nil))
	s, _ := New(Options{Sink: sink, Cache: cache, Logger: logger})
	s.SetBeacons([]Config{{
		ID:      2,
		Type:    TypeTracker,
		Channel: 0,
		Source:  mustAddr(t, "N0CALL-7"),
		Dest:    mustAddr(t, "APGW00"),
		Delay:   10 * time.Millisecond,
		Every:   1 * time.Second,
		Slot:    -1,
		Enabled: true,
	}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go s.Run(ctx)
	select {
	case <-sink.done:
	case <-ctx.Done():
		t.Fatalf("no beacon sent")
	}
	cancel()
	info := string(sink.Frames()[0].Info)
	// Expect position info with course/speed and altitude.
	if !strings.Contains(info, "090/042") {
		t.Errorf("missing cse/spd extension in %q", info)
	}
	if !strings.Contains(info, "/A=") {
		t.Errorf("missing altitude ext in %q", info)
	}
}

// TestScheduler_ObjectBeacon covers OBEACON formatting.
func TestScheduler_ObjectBeacon(t *testing.T) {
	sink := newMockSink(1)
	logger := slog.New(slog.NewTextHandler(logSink{}, nil))
	s, _ := New(Options{Sink: sink, Logger: logger})
	s.SetBeacons([]Config{{
		ID:         3,
		Type:       TypeObject,
		ObjectName: "TESTOBJ",
		Source:     mustAddr(t, "N0CALL"),
		Dest:       mustAddr(t, "APGW00"),
		Delay:      5 * time.Millisecond,
		Every:      1 * time.Second,
		Slot:       -1,
		Lat:        30.0,
		Lon:        -97.0,
		Comment:    "net",
		Enabled:    true,
	}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go s.Run(ctx)
	select {
	case <-sink.done:
	case <-ctx.Done():
		t.Fatalf("no object beacon")
	}
	cancel()
	info := string(sink.Frames()[0].Info)
	if info[0] != ';' {
		t.Errorf("expected object prefix, got %q", info)
	}
	if !strings.Contains(info, "TESTOBJ") {
		t.Errorf("missing object name in %q", info)
	}
}

// TestScheduler_Reload verifies that calling Reload while Run is active
// cancels the running per-beacon goroutines and re-spawns them from the
// new beacon list. We start with a beacon that uses one comment, reload
// with a different comment, and check that subsequent frames carry the
// new comment.
func TestScheduler_Reload(t *testing.T) {
	sink := newMockSink(100) // arbitrarily large; we drive completion ourselves
	logger := slog.New(slog.NewTextHandler(logSink{}, nil))
	s, _ := New(Options{Sink: sink, Logger: logger})

	mkBeacon := func(comment string) Config {
		return Config{
			ID:      1,
			Type:    TypePosition,
			Channel: 0,
			Source:  mustAddr(t, "N0CALL-9"),
			Dest:    mustAddr(t, "APGW00"),
			Delay:   5 * time.Millisecond,
			Every:   20 * time.Millisecond,
			Slot:    -1,
			Lat:     37.0, Lon: -122.0,
			SymbolTable: '/', SymbolCode: '-',
			Comment: comment,
			Enabled: true,
		}
	}
	s.SetBeacons([]Config{mkBeacon("first")})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	runDone := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(runDone)
	}()

	// Wait for at least one frame from the first generation.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sink.Frames()) >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := len(sink.Frames()); got == 0 {
		t.Fatalf("no frames from initial generation")
	}

	// Snapshot the count and reload with a beacon carrying a new comment.
	beforeReload := len(sink.Frames())
	s.Reload([]Config{mkBeacon("second")})

	// Wait for at least one new frame after the reload.
	deadline = time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sink.Frames()) > beforeReload {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	frames := sink.Frames()
	if len(frames) <= beforeReload {
		t.Fatalf("no frames after reload; before=%d after=%d", beforeReload, len(frames))
	}

	// The most recent frame must carry the new comment, proving the
	// generation was rebuilt from the reloaded config.
	last := string(frames[len(frames)-1].Info)
	if !strings.Contains(last, "second") {
		t.Errorf("post-reload frame missing new comment: %q", last)
	}
	// And no first-generation frame can appear after the reload point.
	for i := beforeReload; i < len(frames); i++ {
		if strings.Contains(string(frames[i].Info), "first") {
			t.Errorf("frame %d after reload still carries old comment: %q", i, frames[i].Info)
		}
	}

	cancel()
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("Run did not exit after ctx cancel")
	}
}

func TestTimeToNextSlot(t *testing.T) {
	// 10:00:00 UTC, slot=30 → 30 seconds
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	if got := timeToNextSlot(now, 30); got != 30*time.Second {
		t.Errorf("slot=30 @ :00: got %v", got)
	}
	// 10:00:45, slot=30 → 3585 seconds (next hour)
	now2 := time.Date(2026, 1, 1, 10, 0, 45, 0, time.UTC)
	if got := timeToNextSlot(now2, 30); got != 3585*time.Second {
		t.Errorf("slot=30 @ :45: got %v", got)
	}
}

// logSink discards log output in tests.
type logSink struct{}

func (logSink) Write(p []byte) (int, error) { return len(p), nil }
