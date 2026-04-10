package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func quietApp(t *testing.T) *App {
	t.Helper()
	cfg := DefaultConfig()
	cfg.ShutdownTimeout = time.Second
	return New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// fakeComponent records start/stop calls into a shared recorder so
// tests can assert the observed ordering after the App's lifecycle
// runs. The optional startErr/stopErr fields let tests simulate
// partial startup and tear-down failures.
type fakeComponent struct {
	name     string
	rec      *recorder
	startErr error
	stopErr  error
}

type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(ev string) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	copy(out, r.events)
	return out
}

func (f *fakeComponent) toNamed() namedComponent {
	return namedComponent{
		name: f.name,
		start: func(ctx context.Context) error {
			f.rec.add("start:" + f.name)
			return f.startErr
		},
		stop: func(ctx context.Context) error {
			f.rec.add("stop:" + f.name)
			return f.stopErr
		},
	}
}

// TestLifecycleReverseShutdownOrder is the contract the work order
// specifies: Stop must tear down components in reverse of the order
// they were started, regardless of how many came up.
func TestLifecycleReverseShutdownOrder(t *testing.T) {
	rec := &recorder{}
	a := quietApp(t)
	names := []string{"alpha", "beta", "gamma", "delta"}
	for _, n := range names {
		f := &fakeComponent{name: n, rec: rec}
		a.startOrder = append(a.startOrder, f.toNamed())
	}

	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := a.Stop(shutdownCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	want := []string{
		"start:alpha", "start:beta", "start:gamma", "start:delta",
		"stop:delta", "stop:gamma", "stop:beta", "stop:alpha",
	}
	if got := rec.snapshot(); !equal(got, want) {
		t.Fatalf("events:\ngot:  %v\nwant: %v", got, want)
	}
}

// TestLifecyclePartialStartupCleansUpStartedOnly verifies that when a
// middle component fails, Stop only touches the components that
// actually came up — no spurious stop call on the failed component or
// the ones that never ran.
func TestLifecyclePartialStartupCleansUpStartedOnly(t *testing.T) {
	rec := &recorder{}
	a := quietApp(t)
	a.startOrder = []namedComponent{
		(&fakeComponent{name: "alpha", rec: rec}).toNamed(),
		(&fakeComponent{name: "beta", rec: rec}).toNamed(),
		(&fakeComponent{name: "gamma", rec: rec, startErr: errors.New("boom")}).toNamed(),
		(&fakeComponent{name: "delta", rec: rec}).toNamed(),
	}

	err := a.Start(context.Background())
	if err == nil {
		t.Fatal("Start: want error, got nil")
	}
	if !strings.Contains(err.Error(), "gamma") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("Start error should name failing component: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := a.Stop(shutdownCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// gamma's start was recorded (it was invoked, just returned an
	// error), but neither gamma nor delta should have had stop called.
	want := []string{"start:alpha", "start:beta", "start:gamma", "stop:beta", "stop:alpha"}
	if got := rec.snapshot(); !equal(got, want) {
		t.Fatalf("events:\ngot:  %v\nwant: %v", got, want)
	}
}

// TestLifecycleStopCollectsAllErrors ensures that a stop failure on
// one component does not abort the loop — later components still get
// their turn, and the returned error joins every stop failure.
func TestLifecycleStopCollectsAllErrors(t *testing.T) {
	rec := &recorder{}
	a := quietApp(t)
	a.startOrder = []namedComponent{
		(&fakeComponent{name: "alpha", rec: rec, stopErr: errors.New("alpha-bad")}).toNamed(),
		(&fakeComponent{name: "beta", rec: rec}).toNamed(),
		(&fakeComponent{name: "gamma", rec: rec, stopErr: errors.New("gamma-bad")}).toNamed(),
	}

	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	err := a.Stop(context.Background())
	if err == nil {
		t.Fatal("Stop: want joined error, got nil")
	}
	if !strings.Contains(err.Error(), "alpha-bad") || !strings.Contains(err.Error(), "gamma-bad") {
		t.Errorf("Stop error should join both failures: %v", err)
	}

	// Every component should have been attempted in reverse order
	// even though gamma's stop failed.
	want := []string{
		"start:alpha", "start:beta", "start:gamma",
		"stop:gamma", "stop:beta", "stop:alpha",
	}
	if got := rec.snapshot(); !equal(got, want) {
		t.Fatalf("events:\ngot:  %v\nwant: %v", got, want)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
