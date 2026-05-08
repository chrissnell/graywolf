package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/metrics"
)

// Regression coverage for graywolf issue #84: enable/disable toggling
// the iGate at runtime had no effect until the daemon was restarted.
// The fix wires the reload signal channel and reload-drainer goroutine
// unconditionally, and rewrites reloadIgate to handle the
// disabled<->enabled transitions instead of only the enabled-enabled
// reconfigure path.

// toggleHarness boots an App with the iGate disabled at config time,
// matching the production wireIGate path: a.ig stays nil but the reload
// channel + adapters are still allocated. The harness drives reload
// signals through the production App.reloadIgate.
type toggleHarness struct {
	ctx        context.Context
	cancel     context.CancelFunc
	mock       *tfMockAprsIsServer
	app        *App
	reloadDone sync.WaitGroup
}

func (h *toggleHarness) close() {
	h.cancel()
	h.reloadDone.Wait()
	if ig := h.app.ig.Load(); ig != nil {
		ig.Stop()
	}
	h.mock.close()
	if h.app.store != nil {
		_ = h.app.store.Close()
	}
}

func newToggleHarness(t *testing.T) *toggleHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	store, err := configstore.OpenMemory()
	if err != nil {
		cancel()
		t.Fatalf("OpenMemory: %v", err)
	}

	if err := store.UpsertStationConfig(ctx, configstore.StationConfig{Callsign: "KE7XYZ"}); err != nil {
		cancel()
		t.Fatalf("UpsertStationConfig: %v", err)
	}

	mock := newTFMockAprsIsServer(t, "# logresp KE7XYZ verified, server TFMOCK")
	mock.start()

	host, portStr, _ := net.SplitHostPort(mock.addr())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)

	// Seed a disabled iGate config pointed at the mock so a subsequent
	// enable toggle has somewhere real to connect.
	if err := store.UpsertIGateConfig(ctx, &configstore.IGateConfig{
		Enabled:         false,
		Server:          host,
		Port:            uint32(port),
		TxChannel:       1,
		RfChannel:       1,
		MaxMsgHops:      2,
		ServerFilter:    "",
		SoftwareName:    "graywolf-test",
		SoftwareVersion: "toggle",
	}); err != nil {
		cancel()
		t.Fatalf("UpsertIGateConfig: %v", err)
	}

	a := &App{
		cfg:     DefaultConfig(),
		logger:  logger,
		store:   store,
		metrics: metrics.New(),
	}

	// Production wireIGate behavior for a disabled-at-boot config:
	// allocate the reload channel, the RF->IS fanout adapter, and the
	// live IGateLineSender so subsequent reload signals have somewhere
	// to land. We replicate that here without spinning up the rest of
	// wireServices.
	if err := a.wireIGate(ctx); err != nil {
		cancel()
		t.Fatalf("wireIGate: %v", err)
	}
	if got := a.ig.Load(); got != nil {
		cancel()
		t.Fatalf("disabled-at-boot harness should leave a.ig nil, got %v", got)
	}
	if a.igateReload == nil {
		cancel()
		t.Fatalf("wireIGate must allocate igateReload even when disabled")
	}
	if a.igateOut == nil {
		cancel()
		t.Fatalf("wireIGate must allocate igateOut even when disabled")
	}
	if a.igateLineSender == nil {
		cancel()
		t.Fatalf("wireIGate must allocate igateLineSender even when disabled")
	}

	h := &toggleHarness{
		ctx:    ctx,
		cancel: cancel,
		mock:   mock,
		app:    a,
	}

	// Reload-drainer goroutine — same shape as igateComponent.start.
	h.reloadDone.Add(1)
	go func() {
		defer h.reloadDone.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-a.igateReload:
				if !ok {
					return
				}
				a.reloadIgate(ctx)
			}
		}
	}()

	return h
}

func (h *toggleHarness) signalReload(t *testing.T) {
	t.Helper()
	select {
	case h.app.igateReload <- struct{}{}:
	case <-time.After(time.Second):
		t.Fatalf("timeout pushing reload signal")
	}
}

func (h *toggleHarness) waitForIgate(t *testing.T, want bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		got := h.app.ig.Load() != nil
		if got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for a.ig non-nil=%v after %v", want, timeout)
}

// TestIgateToggle_DisabledThenEnabled is the direct regression for
// issue #84's primary scenario: graywolf boots with the iGate off, the
// operator flips Enable on through the UI, and the iGate must connect
// without a daemon restart.
func TestIgateToggle_DisabledThenEnabled(t *testing.T) {
	h := newToggleHarness(t)
	defer h.close()

	// Flip Enabled=true in the configstore (mirrors what the webapi
	// PUT /api/igate/config handler does before signalIgateReload).
	cfg, err := h.app.store.GetIGateConfig(h.ctx)
	if err != nil {
		t.Fatalf("GetIGateConfig: %v", err)
	}
	cfg.Enabled = true
	if err := h.app.store.UpsertIGateConfig(h.ctx, cfg); err != nil {
		t.Fatalf("UpsertIGateConfig: %v", err)
	}
	h.signalReload(t)

	// Mock APRS-IS server must see a fresh login arrive — proves the
	// iGate was constructed AND Started by reloadIgate, not just
	// stored.
	h.mock.waitForLogin(t, 5*time.Second)
	h.waitForIgate(t, true, 2*time.Second)
}

// TestIgateToggle_EnabledThenDisabled covers the inverse: the operator
// turns the iGate off through the UI and the existing connection must
// be torn down without a restart.
func TestIgateToggle_EnabledThenDisabled(t *testing.T) {
	h := newToggleHarness(t)
	defer h.close()

	// Bring it up first.
	cfg, err := h.app.store.GetIGateConfig(h.ctx)
	if err != nil {
		t.Fatalf("GetIGateConfig: %v", err)
	}
	cfg.Enabled = true
	if err := h.app.store.UpsertIGateConfig(h.ctx, cfg); err != nil {
		t.Fatalf("UpsertIGateConfig (enable): %v", err)
	}
	h.signalReload(t)
	h.mock.waitForLogin(t, 5*time.Second)
	h.waitForIgate(t, true, 2*time.Second)

	// Now flip Enabled=false and signal again.
	cfg.Enabled = false
	if err := h.app.store.UpsertIGateConfig(h.ctx, cfg); err != nil {
		t.Fatalf("UpsertIGateConfig (disable): %v", err)
	}
	h.signalReload(t)

	h.waitForIgate(t, false, 2*time.Second)
}

// TestIgateToggle_DisableEnableCycle confirms the fix supports
// repeated transitions in a single session (each enable rebuilds a
// fresh *igate.Igate; each disable tears it down).
func TestIgateToggle_DisableEnableCycle(t *testing.T) {
	h := newToggleHarness(t)
	defer h.close()

	cfg, err := h.app.store.GetIGateConfig(h.ctx)
	if err != nil {
		t.Fatalf("GetIGateConfig: %v", err)
	}

	flip := func(enabled bool, want bool) {
		t.Helper()
		cfg.Enabled = enabled
		if err := h.app.store.UpsertIGateConfig(h.ctx, cfg); err != nil {
			t.Fatalf("UpsertIGateConfig: %v", err)
		}
		h.signalReload(t)
		if enabled {
			h.mock.waitForLogin(t, 5*time.Second)
		}
		h.waitForIgate(t, want, 2*time.Second)
	}

	flip(true, true)
	flip(false, false)
	flip(true, true)
	flip(false, false)
}
