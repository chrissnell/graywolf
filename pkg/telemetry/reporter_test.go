package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type testSend struct {
	identity, route, kind, info string
	at                          time.Time
}
type harness struct {
	r                            *Reporter
	cfg                          Config
	obs                          Observation
	state                        State
	sends                        []testSend
	now                          time.Time
	saveErr, sendErr, observeErr error
	cancelCount                  int
}

func newHarness() *harness {
	h := &harness{cfg: Defaults(), obs: Observation{DigiSource: "legacy", StationCallsign: "TEST1-10", InputReady: true, ISReady: true, RFReady: true}, now: time.Unix(1000000, 0)}
	h.cfg.Enabled = true
	h.cfg.Channel = 1
	h.r = New(Options{
		Load: func(context.Context) (Config, State, error) { return h.cfg, h.state, nil },
		Save: func(_ context.Context, s State) error {
			if h.saveErr != nil {
				return h.saveErr
			}
			b, _ := json.Marshal(s)
			_ = json.Unmarshal(b, &h.state)
			return nil
		},
		Observe: func(context.Context, Config) (Observation, error) { return h.obs, h.observeErr },
		Cancel:  func() { h.cancelCount++ },
		Send: func(_ context.Context, _ Config, identity, route, kind, info string) error {
			h.sends = append(h.sends, testSend{identity, route, kind, info, h.now})
			return h.sendErr
		},
	})
	return h
}
func (h *harness) tick(seconds int) {
	h.now = time.Unix(1000000+int64(seconds), 0)
	h.r.Tick(context.Background(), h.now)
}
func (h *harness) advance(from, to int) {
	for i := from; i <= to; i += 10 {
		h.tick(i)
	}
}
func TestFullBaselineAndDataBeforeDefinitions(t *testing.T) {
	h := newHarness()
	h.advance(0, 590)
	if len(h.sends) != 0 {
		t.Fatal("sent before full baseline")
	}
	h.obs.Counts = Counts{10, 20, 30, 40, 50}
	h.tick(600)
	if len(h.sends) != 1 || h.sends[0].kind != "data" || h.sends[0].info != "T#000,010,020,030,040,050,00000000" {
		t.Fatalf("first send: %+v", h.sends)
	}
	h.tick(620)
	if len(h.sends) != 1 {
		t.Fatal("definitions too early")
	}
	h.tick(630)
	h.tick(660)
	h.tick(690)
	for i, kind := range []string{"data", "parm", "unit", "eqns"} {
		if h.sends[i].kind != kind {
			t.Fatalf("order: %+v", h.sends)
		}
	}
	if !strings.Contains(h.sends[1].info, ":TEST1-10 :PARM.RF Rx,Digi,RF Tx,RF > IS,IS > RF") {
		t.Fatal(h.sends[1].info)
	}
	if h.state.NextSequence != 1 || h.state.Metadata["aprsis"].IsZero() {
		t.Fatal("state not persisted")
	}
	h.advance(700, 1190)
	if len(h.sends) != 4 {
		t.Fatal("extra report")
	}
	h.tick(1200)
	if len(h.sends) != 5 || !strings.HasPrefix(h.sends[4].info, "T#001,") {
		t.Fatal(h.sends)
	}
}
func TestWaitsForInputNotFirstPacketOrInternet(t *testing.T) {
	h := newHarness()
	h.obs.InputReady = false
	h.obs.ISReady = false
	h.advance(0, 700)
	if h.r.Snapshot().State != "waiting_for_rf_input" || len(h.sends) != 0 {
		t.Fatal(h.r.Snapshot())
	}
	h.obs.InputReady = true
	h.advance(710, 1300)
	if len(h.sends) != 0 {
		t.Fatal("early report")
	}
	h.tick(1310)
	if len(h.sends) != 1 || h.sends[0].route != "rf" || !strings.Contains(h.sends[0].info, ",000,000,000,000,000,") {
		t.Fatal(h.sends)
	}
}
func TestInputLossDoesNotResetValidCounters(t *testing.T) {
	h := newHarness()
	h.advance(0, 300)
	h.obs.InputReady = false
	h.advance(310, 600)
	if len(h.sends) != 1 {
		t.Fatalf("lost baseline: %+v", h.r.Snapshot())
	}
}
func TestSourceChangesResetBaselineEvenIfHigher(t *testing.T) {
	h := newHarness()
	h.advance(0, 300)
	h.obs.DigiSource = "forwarded"
	h.obs.Counts[1] = 100
	h.advance(310, 900)
	if len(h.sends) != 0 {
		t.Fatal("mixed sources")
	}
	h.tick(910)
	if len(h.sends) != 1 {
		t.Fatal("did not recover")
	}
}
func TestCounterResetAndSamplingGap(t *testing.T) {
	for _, reason := range []string{"counter", "gap", "unavailable"} {
		t.Run(reason, func(t *testing.T) {
			h := newHarness()
			h.obs.Counts = Counts{10, 10, 10, 10, 10}
			h.advance(0, 300)
			switch reason {
			case "counter":
				h.obs.Counts[0] = 0
				h.tick(310)
			case "gap":
				h.tick(500)
			case "unavailable":
				h.observeErr = errors.New("not readable")
				h.tick(310)
				h.observeErr = nil
				h.tick(320)
			}
			if h.r.Snapshot().Ready || len(h.sends) != 0 {
				t.Fatal(h.r.Snapshot())
			}
		})
	}
}
func TestReportEncodingAndClipping(t *testing.T) {
	counts := Counts{0, 255, 256, 376, 10000}
	plain, encoded, clipped := Encode(1001, counts, false)
	if plain != "T#001,000,255,255,255,255,00000000" || !clipped || encoded[4] != 255 {
		t.Fatal(plain)
	}
	extended, encoded, clipped := Encode(1, counts, true)
	if extended != "T#001,000,255,256,376,10000,00000000" || clipped || encoded != counts {
		t.Fatal(extended)
	}
}
func TestIdentityInheritanceOverrideAndReset(t *testing.T) {
	h := newHarness()
	h.advance(0, 600)
	if h.sends[0].identity != "TEST1-10" {
		t.Fatal(h.sends)
	}
	h.obs.StationCallsign = "TEST1-11"
	h.tick(610)
	if h.r.Snapshot().Ready {
		t.Fatal("identity did not reset")
	}
	h.cfg.Callsign = "TEST1-12"
	h.tick(620)
	h.advance(630, 1220)
	if h.sends[len(h.sends)-1].identity != "TEST1-12" {
		t.Fatal(h.sends)
	}
	h.obs.StationCallsign = "TEST1-13"
	h.tick(1230)
	if !h.r.Snapshot().Ready {
		t.Fatal("override should stay fixed")
	}
}
func TestFailedOrUncertainSendDoesNotFallBackOrSendDefinitions(t *testing.T) {
	h := newHarness()
	h.sendErr = errors.New("uncertain transport failure")
	h.advance(0, 600)
	h.obs.ISReady = false
	h.advance(610, 1190)
	if len(h.sends) != 1 || h.sends[0].route != "aprsis" {
		t.Fatalf("unexpected retry: %+v", h.sends)
	}
	h.sendErr = nil
	h.tick(1200)
	if len(h.sends) != 2 || h.sends[1].kind != "data" || h.sends[1].route != "rf" {
		t.Fatal(h.sends)
	}
	h.tick(1230)
	if h.sends[2].kind != "parm" {
		t.Fatal(h.sends)
	}
}
func TestPersistenceFailurePreventsSend(t *testing.T) {
	h := newHarness()
	h.saveErr = errors.New("disk error")
	h.advance(0, 600)
	if len(h.sends) != 0 || h.r.Snapshot().State != "persistence_error" {
		t.Fatal(h.r.Snapshot())
	}
}
func TestDisableAndManualGuard(t *testing.T) {
	h := newHarness()
	h.cfg.Enabled = false
	h.advance(0, 600)
	if len(h.sends) != 0 {
		t.Fatal("disabled send")
	}
	if err := h.r.ReportNow(context.Background()); err == nil {
		t.Fatal("manual send while disabled")
	}
	h.cfg.Enabled = true
	h.tick(610)
	if h.r.Snapshot().Ready {
		t.Fatal("no warmup")
	}
	h.r.Invalidate()
	if h.cancelCount == 0 {
		t.Fatal("queued work not canceled")
	}
}
func TestMetadataResumeStateDoesNotSkipWarmup(t *testing.T) {
	h := newHarness()
	h.advance(0, 690)
	state := h.state
	h2 := newHarness()
	h2.state = state
	h2.advance(0, 590)
	if len(h2.sends) != 0 {
		t.Fatal("reboot bypass")
	}
	h2.tick(600)
	h2.advance(610, 700)
	// Future persisted times are treated as due; no clock rollback can prevent definitions forever.
	if h2.sends[0].kind != "data" {
		t.Fatal(h2.sends)
	}
}
func TestNoCatchupBurstAndBoundedSamples(t *testing.T) {
	h := newHarness()
	h.advance(0, 590)
	h.tick(5000)
	if len(h.sends) != 0 || h.r.Snapshot().Ready {
		t.Fatal("caught up after gap")
	}
	h.advance(5010, 10000)
	if len(h.r.samples) > 64 {
		t.Fatal(len(h.r.samples))
	}
}
func TestValidateConfiguration(t *testing.T) {
	valid := Defaults()
	if err := Validate(valid); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Config){func(c *Config) { c.Delivery = "both" }, func(c *Config) { c.Callsign = "N0CALL" }, func(c *Config) { c.RFPath = "BAD PATH" }, func(c *Config) { c.MetadataISSeconds = 10 }, func(c *Config) { c.Enabled = true }} {
		c := valid
		change(&c)
		if Validate(c) == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
}

func TestRecentSubmissionHistoryIsBoundedAndIsolated(t *testing.T) {
	h := newHarness()
	h.advance(0, 12000)
	status := h.r.Snapshot()
	if len(status.Recent) != 10 {
		t.Fatal(len(status.Recent))
	}
	if status.Recent[0].Identity != "TEST1-10" || status.Recent[0].Route != "aprsis" || status.Recent[0].Payload == "" {
		t.Fatal(status.Recent[0])
	}
	status.Recent[0].Identity = "mutated"
	if h.r.Snapshot().Recent[0].Identity == "mutated" {
		t.Fatal("snapshot aliases live history")
	}
	h.sendErr = errors.New("uncertain")
	h.advance(12010, 12600)
	if h.r.Snapshot().Recent[0].SubmittedAt.After(status.Recent[0].SubmittedAt) {
		t.Fatal("failed send recorded as submitted")
	}
}
