package app

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/gps"
	"github.com/chrissnell/graywolf/pkg/metrics"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// fakeTxSink is a txgovernor.TxSink that discards frames. The
// smart-beacon reload test never fires a beacon, so every Submit path
// is cold — we only need a sink that satisfies the scheduler's
// construction contract.
type fakeTxSink struct{}

func (fakeTxSink) Submit(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
	return nil
}

// newTestApp stands up an App whose wiring is deep enough to exercise
// the beacon reload pipeline but skips the modem bridge, HTTP server,
// and other unrelated components. The configstore is in-memory, the
// beacon scheduler is real, and the tx sink is a no-op.
//
// The returned cancel func tears everything down; callers should defer
// it to make sure the reload goroutine exits before the test ends.
func newTestApp(t *testing.T) (*App, context.Context, context.CancelFunc) {
	t.Helper()

	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	gpsCache := gps.NewMemCache()
	stationPos := gps.NewStationPos(gpsCache)

	sched, err := beacon.New(beacon.Options{
		Sink:     fakeTxSink{},
		Cache:    gpsCache,
		Logger:   logger,
		Observer: &beaconObserver{m: metrics.New()},
	})
	if err != nil {
		t.Fatalf("beacon.New: %v", err)
	}

	a := &App{
		cfg:               DefaultConfig(),
		logger:            logger,
		store:             store,
		gpsCache:          gpsCache,
		stationPos:        stationPos,
		beaconSched:       sched,
		beaconReload:      make(chan struct{}, 1),
		smartBeaconReload: make(chan struct{}, 1),
		beaconReloadDone:  make(chan struct{}, 1),
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Bring only the beacon component online. The scheduler goroutine
	// and the reload goroutine both attach to ctx, so cancel() tears
	// them down when the test ends.
	comp := a.beaconComponent()
	if err := comp.start(ctx); err != nil {
		cancel()
		t.Fatalf("beaconComponent start: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		// Best-effort: wait for the reload + scheduler goroutines to
		// exit so races across tests don't cross-contaminate.
		shutdownCtx, shCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shCancel()
		_ = comp.stop(shutdownCtx)
	})

	return a, ctx, cancel
}

// TestBeaconConfigFromStore_SmartBeaconPrecedence locks down the
// precedence rule on beaconConfigFromStore: cfg.SmartBeacon is non-nil
// iff BOTH b.SmartBeacon AND the global singleton's Enabled are true.
// Either being false means fallback to the fixed Every interval.
func TestBeaconConfigFromStore_SmartBeaconPrecedence(t *testing.T) {
	b := configstore.Beacon{
		Type:         "tracker",
		Channel:      1,
		Callsign:     "N0CALL",
		Destination:  "APGRWF",
		Path:         "WIDE1-1",
		SymbolTable:  "/",
		Symbol:       ">",
		EverySeconds: 1800,
		Enabled:      true,
		SmartBeacon:  true,
	}
	smart := &configstore.SmartBeaconConfig{
		Enabled:     true,
		FastSpeedKt: 100,
		FastRateSec: 10,
		SlowSpeedKt: 5,
		SlowRateSec: 600,
		MinTurnDeg:  30,
		TurnSlope:   255,
		MinTurnSec:  15,
	}

	cases := []struct {
		name           string
		beaconSmart    bool
		globalSmart    *configstore.SmartBeaconConfig
		wantSmartSet   bool
		wantFastSpeed  float64
		wantFastRate   time.Duration
		wantSlowSpeed  float64
		wantSlowRate   time.Duration
		wantTurnAngle  float64
		wantTurnSlope  float64
		wantTurnTime   time.Duration
	}{
		{
			name:         "both off",
			beaconSmart:  false,
			globalSmart:  nil,
			wantSmartSet: false,
		},
		{
			name:         "beacon flag only, no singleton row",
			beaconSmart:  true,
			globalSmart:  nil,
			wantSmartSet: false,
		},
		{
			name:         "beacon flag only, singleton disabled",
			beaconSmart:  true,
			globalSmart:  &configstore.SmartBeaconConfig{Enabled: false, FastSpeedKt: 100},
			wantSmartSet: false,
		},
		{
			name:         "global enabled but beacon flag off",
			beaconSmart:  false,
			globalSmart:  smart,
			wantSmartSet: false,
		},
		{
			name:           "both enabled",
			beaconSmart:    true,
			globalSmart:    smart,
			wantSmartSet:   true,
			wantFastSpeed:  100,
			wantFastRate:   10 * time.Second,
			wantSlowSpeed:  5,
			wantSlowRate:   600 * time.Second,
			wantTurnAngle:  30,
			wantTurnSlope:  255,
			wantTurnTime:   15 * time.Second,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b2 := b
			b2.SmartBeacon = tc.beaconSmart
			cfg, err := beaconConfigFromStore(b2, tc.globalSmart)
			if err != nil {
				t.Fatalf("beaconConfigFromStore: %v", err)
			}
			if tc.wantSmartSet {
				if cfg.SmartBeacon == nil {
					t.Fatalf("cfg.SmartBeacon = nil, want non-nil")
				}
				got := cfg.SmartBeacon
				if !got.Enabled {
					t.Errorf("Enabled = false, want true")
				}
				if got.FastSpeed != tc.wantFastSpeed {
					t.Errorf("FastSpeed = %v, want %v", got.FastSpeed, tc.wantFastSpeed)
				}
				if got.FastRate != tc.wantFastRate {
					t.Errorf("FastRate = %v, want %v", got.FastRate, tc.wantFastRate)
				}
				if got.SlowSpeed != tc.wantSlowSpeed {
					t.Errorf("SlowSpeed = %v, want %v", got.SlowSpeed, tc.wantSlowSpeed)
				}
				if got.SlowRate != tc.wantSlowRate {
					t.Errorf("SlowRate = %v, want %v", got.SlowRate, tc.wantSlowRate)
				}
				if got.TurnAngle != tc.wantTurnAngle {
					t.Errorf("TurnAngle = %v, want %v", got.TurnAngle, tc.wantTurnAngle)
				}
				if got.TurnSlope != tc.wantTurnSlope {
					t.Errorf("TurnSlope = %v, want %v", got.TurnSlope, tc.wantTurnSlope)
				}
				if got.TurnTime != tc.wantTurnTime {
					t.Errorf("TurnTime = %v, want %v", got.TurnTime, tc.wantTurnTime)
				}
			} else {
				if cfg.SmartBeacon != nil {
					t.Fatalf("cfg.SmartBeacon = %+v, want nil", cfg.SmartBeacon)
				}
			}
		})
	}
}

// TestSmartBeaconReloadPipeline is the oracle for live-reload
// correctness: it seeds a SmartBeacon-enabled tracker beacon, proves
// cfg.SmartBeacon is nil until a global row exists (precedence rule),
// upserts a row with Enabled=true + distinctive values, fires
// smartBeaconReload, waits for the reload to land, and asserts the
// values propagated through beaconConfigFromStore. Then it flips the
// global toggle off, fires reload again, and asserts cfg.SmartBeacon
// is nil once more.
//
// Observation of "reload landed" uses a.beaconReloadDone — a test-only
// hook that the reload goroutine pings after each successful pass.
// loadBeaconConfigs is called directly to snapshot what the scheduler
// would see; no public scheduler accessor is required.
func TestSmartBeaconReloadPipeline(t *testing.T) {
	a, ctx, _ := newTestApp(t)

	// Seed one beacon with SmartBeacon=true; no global row yet.
	beaconRow := &configstore.Beacon{
		Type:         "tracker",
		Channel:      1,
		Callsign:     "N0CALL",
		Destination:  "APGRWF",
		Path:         "WIDE1-1",
		SymbolTable:  "/",
		Symbol:       ">",
		EverySeconds: 1800,
		Enabled:      true,
		SmartBeacon:  true,
	}
	if err := a.store.CreateBeacon(ctx, beaconRow); err != nil {
		t.Fatalf("CreateBeacon: %v", err)
	}

	// Precedence rule: both flags required. No global row → nil.
	configs := a.loadBeaconConfigs(ctx, "test")
	if len(configs) != 1 {
		t.Fatalf("configs: got %d, want 1", len(configs))
	}
	if configs[0].SmartBeacon != nil {
		t.Fatalf("initial cfg.SmartBeacon = %+v, want nil (no global row)",
			configs[0].SmartBeacon)
	}

	// Upsert the global singleton with Enabled=true and distinctive
	// values chosen to stand out from any realistic default.
	want := &configstore.SmartBeaconConfig{
		Enabled:     true,
		FastSpeedKt: 100,
		FastRateSec: 10,
		SlowSpeedKt: 5,
		SlowRateSec: 600,
		MinTurnDeg:  30,
		TurnSlope:   255,
		MinTurnSec:  15,
	}
	if err := a.store.UpsertSmartBeaconConfig(ctx, want); err != nil {
		t.Fatalf("UpsertSmartBeaconConfig: %v", err)
	}

	// Drain any spurious reload notifications from the init pass so
	// the post-signal wait is deterministic.
	select {
	case <-a.beaconReloadDone:
	default:
	}

	// Fire the signal and wait for the reload goroutine to run.
	a.smartBeaconReload <- struct{}{}
	select {
	case <-a.beaconReloadDone:
	case <-time.After(time.Second):
		t.Fatal("reload did not land within 1s after smartBeaconReload signal")
	}

	// After reload, loadBeaconConfigs reflects the upserted row.
	configs = a.loadBeaconConfigs(ctx, "test")
	if len(configs) != 1 {
		t.Fatalf("configs after enable: got %d, want 1", len(configs))
	}
	got := configs[0].SmartBeacon
	if got == nil {
		t.Fatalf("cfg.SmartBeacon = nil, want non-nil")
	}
	if !got.Enabled {
		t.Errorf("Enabled = false, want true")
	}
	if got.FastSpeed != float64(want.FastSpeedKt) {
		t.Errorf("FastSpeed = %v, want %v", got.FastSpeed, want.FastSpeedKt)
	}
	if got.FastRate != time.Duration(want.FastRateSec)*time.Second {
		t.Errorf("FastRate = %v, want %v", got.FastRate,
			time.Duration(want.FastRateSec)*time.Second)
	}
	if got.SlowSpeed != float64(want.SlowSpeedKt) {
		t.Errorf("SlowSpeed = %v, want %v", got.SlowSpeed, want.SlowSpeedKt)
	}
	if got.SlowRate != time.Duration(want.SlowRateSec)*time.Second {
		t.Errorf("SlowRate = %v, want %v", got.SlowRate,
			time.Duration(want.SlowRateSec)*time.Second)
	}
	if got.TurnAngle != float64(want.MinTurnDeg) {
		t.Errorf("TurnAngle = %v, want %v", got.TurnAngle, want.MinTurnDeg)
	}
	if got.TurnSlope != float64(want.TurnSlope) {
		t.Errorf("TurnSlope = %v, want %v", got.TurnSlope, want.TurnSlope)
	}
	if got.TurnTime != time.Duration(want.MinTurnSec)*time.Second {
		t.Errorf("TurnTime = %v, want %v", got.TurnTime,
			time.Duration(want.MinTurnSec)*time.Second)
	}

	// Flip the global toggle off. The per-beacon SmartBeacon flag is
	// still true, but the precedence rule means cfg.SmartBeacon
	// collapses back to nil.
	want.Enabled = false
	if err := a.store.UpsertSmartBeaconConfig(ctx, want); err != nil {
		t.Fatalf("UpsertSmartBeaconConfig (disable): %v", err)
	}

	select {
	case <-a.beaconReloadDone:
	default:
	}
	a.smartBeaconReload <- struct{}{}
	select {
	case <-a.beaconReloadDone:
	case <-time.After(time.Second):
		t.Fatal("reload did not land within 1s after disable signal")
	}

	configs = a.loadBeaconConfigs(ctx, "test")
	if len(configs) != 1 {
		t.Fatalf("configs after disable: got %d, want 1", len(configs))
	}
	if configs[0].SmartBeacon != nil {
		t.Fatalf("cfg.SmartBeacon after disable = %+v, want nil",
			configs[0].SmartBeacon)
	}
}
