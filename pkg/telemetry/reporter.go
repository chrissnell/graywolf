package telemetry

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type sample struct {
	at     time.Time
	counts Counts
}

// Reporter serializes its small state machine; Snapshot never exposes internal maps.
// Tick accepts an explicit monotonic clock for deterministic tests; Run uses time.Now.
type Reporter struct {
	mu            sync.Mutex
	opts          Options
	loaded        bool
	durable       State
	status        Status
	signature     string
	samples       []sample
	nextData      time.Time
	lastTick      time.Time
	metaRoute     string
	metaKinds     []string
	nextMeta      time.Time
	metaRetry     time.Time
	routeReported string
}

func New(opts Options) *Reporter {
	return &Reporter{opts: opts, status: Status{State: "disabled", Scope: "station", WarmupSeconds: 600}}
}
func (r *Reporter) Snapshot() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.status
	out.Recent = append([]Report{}, r.status.Recent...)
	return out
}
func (r *Reporter) Run(ctx context.Context) {
	t := time.NewTicker(SampleEvery)
	defer t.Stop()
	r.Tick(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			r.Tick(ctx, now)
		}
	}
}
func (r *Reporter) reset(reason string) {
	if r.opts.Cancel != nil {
		r.opts.Cancel()
	}
	r.samples = nil
	r.metaKinds = nil
	r.metaRoute = ""
	r.routeReported = ""
	r.nextData = time.Time{}
	r.status.Ready = false
	r.status.WarmupSeconds = 600
	r.status.Preview = ""
	r.status.State = reason
	r.status.Counts = Counts{}
	r.status.Encoded = Counts{}
	r.status.Clipped = false
	r.status.NextReport = time.Time{}
}
func (r *Reporter) Tick(ctx context.Context, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.tick(ctx, now, false)
}

// ReportNow requires a complete baseline and applies a 30-second manual-send guard.
func (r *Reporter) ReportNow(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if !r.status.LastSubmission.IsZero() && now.Sub(r.status.LastSubmission) < DefinitionSpacing {
		return fmt.Errorf("wait 30 seconds between manual reports")
	}
	return r.tick(ctx, now, true)
}
func (r *Reporter) tick(ctx context.Context, now time.Time, force bool) error {
	cfg, saved, err := r.opts.Load(ctx)
	if err != nil {
		r.reset("waiting_for_configuration")
		r.status.LastError = err.Error()
		return err
	}
	cfg = Normalize(cfg)
	if err = Validate(cfg); err != nil {
		r.reset("invalid_configuration")
		r.status.LastError = err.Error()
		return err
	}
	if !r.loaded {
		r.durable = saved
		r.durable.NextSequence %= 1000
		if r.durable.Metadata == nil {
			r.durable.Metadata = map[string]time.Time{}
		}
		r.loaded = true
	}
	if !cfg.Enabled {
		r.reset("disabled")
		r.signature = ""
		r.status.LastError = ""
		return fmt.Errorf("telemetry is disabled")
	}
	obs, err := r.opts.Observe(ctx, cfg)
	if err != nil {
		r.reset("waiting_for_counters")
		r.status.LastError = err.Error()
		return err
	}
	identity := cfg.Callsign
	if identity == "" {
		identity = obs.StationCallsign
	}
	r.status.EffectiveCallsign = identity
	r.status.InputReady = obs.InputReady
	r.status.DigiSource = obs.DigiSource
	if err = ValidateIdentity(identity); err != nil {
		r.reset("waiting_for_identity")
		r.status.LastError = err.Error()
		return err
	}
	if obs.DigiSource != "forwarded" && obs.DigiSource != "legacy" {
		r.reset("waiting_for_counters")
		return fmt.Errorf("digipeater counter source unavailable")
	}
	sig := fmt.Sprintf("%+v/%s/%s", cfg, identity, obs.DigiSource)
	if sig != r.signature {
		r.reset("warming_up")
		r.signature = sig
	}
	if !r.lastTick.IsZero() && (now.Sub(r.lastTick) > 90*time.Second || now.Before(r.lastTick)) {
		r.reset("warming_up")
	}
	r.lastTick = now
	if len(r.samples) > 0 {
		last := r.samples[len(r.samples)-1]
		for i, n := range obs.Counts {
			if n < last.counts[i] {
				r.reset("warming_up")
				break
			}
		}
	}
	if len(r.samples) == 0 && !obs.InputReady {
		r.status.State = "waiting_for_rf_input"
		return fmt.Errorf("waiting for an enabled RF input")
	}
	// Retain at most 128 observations (normally 62); manual requests cannot grow it.
	if len(r.samples) == 0 || now.Sub(r.samples[len(r.samples)-1].at) >= SampleEvery {
		r.samples = append(r.samples, sample{now, obs.Counts})
	}
	for len(r.samples) > 1 && now.Sub(r.samples[1].at) >= Window {
		r.samples = r.samples[1:]
	}
	if len(r.samples) > 128 {
		r.reset("warming_up")
		return fmt.Errorf("sample buffer limit")
	}
	age := now.Sub(r.samples[0].at)
	if age < Window {
		r.status.State = "warming_up"
		r.status.WarmupSeconds = int((Window - age + time.Second - 1) / time.Second)
		return fmt.Errorf("collecting ten-minute baseline")
	}
	if age > Window+75*time.Second {
		r.reset("warming_up")
		return fmt.Errorf("baseline too old")
	}
	r.status.Ready = true
	r.status.WarmupSeconds = 0
	r.status.LastError = ""
	for i, n := range obs.Counts {
		r.status.Counts[i] = n - r.samples[0].counts[i]
	}
	payload, encoded, clipped := Encode(r.durable.NextSequence, r.status.Counts, cfg.Extended)
	r.status.Preview = payload
	r.status.Encoded = encoded
	r.status.Clipped = clipped
	route := ""
	if cfg.Delivery != "rf" && obs.ISReady {
		route = "aprsis"
	} else if cfg.Delivery != "aprsis" && obs.RFReady {
		route = "rf"
	}
	r.status.Route = route
	if route == "" {
		r.metaKinds = nil
		r.routeReported = ""
		r.status.State = "waiting_for_delivery"
		return fmt.Errorf("no permitted delivery route ready")
	}
	if route != r.metaRoute {
		r.metaKinds = nil
		r.metaRoute = route
	}
	metaIdentity := fmt.Sprintf("%s/%t/%d/%s", identity, cfg.Extended, cfg.Channel, cfg.RFPath)
	if r.durable.MetadataIdentity != metaIdentity {
		r.durable.MetadataIdentity = metaIdentity
		r.durable.Metadata = map[string]time.Time{}
	}
	// Data always precedes definitions, including the first report on a new route.
	if force || r.nextData.IsZero() || !now.Before(r.nextData) {
		r.nextData = now.Add(Window)
		r.status.NextReport = r.nextData
		r.durable.NextSequence = (r.durable.NextSequence + 1) % 1000
		if err = r.opts.Save(ctx, r.durable); err != nil {
			r.status.LastError = err.Error()
			r.status.State = "persistence_error"
			return err
		}
		if err = r.opts.Send(ctx, cfg, identity, route, "data", payload); err != nil {
			r.metaKinds = nil
			r.status.LastError = err.Error()
			r.status.State = "submission_failed"
			return err
		}
		r.status.Recent = append([]Report{{SubmittedAt: now, Identity: identity, Route: route, Counts: r.status.Counts, Encoded: encoded, Clipped: clipped, Payload: payload}}, r.status.Recent...)
		if len(r.status.Recent) > 10 {
			r.status.Recent = r.status.Recent[:10]
		}
		r.status.LastSubmission = now
		r.status.LastPayload = payload
		r.status.State = "submitted"
		r.routeReported = route
		period := time.Duration(cfg.MetadataISSeconds) * time.Second
		if route == "rf" {
			period = time.Duration(cfg.MetadataRFSeconds) * time.Second
		}
		last := r.durable.Metadata[route]
		if len(r.metaKinds) == 0 && (last.IsZero() || now.Before(last) || now.Sub(last) >= period) && !now.Before(r.metaRetry) {
			r.metaKinds = []string{"parm", "unit", "eqns"}
			r.nextMeta = now.Add(DefinitionSpacing)
		}
		return nil
	}
	if r.routeReported == route && len(r.metaKinds) > 0 && !now.Before(r.nextMeta) {
		kind := r.metaKinds[0]
		info := Definitions(identity)[kind]
		if err = r.opts.Send(ctx, cfg, identity, route, kind, info); err != nil {
			r.metaKinds = nil
			r.metaRetry = now.Add(Window)
			r.status.LastError = err.Error()
			r.status.State = "definitions_failed"
			return err
		}
		r.metaKinds = r.metaKinds[1:]
		r.nextMeta = now.Add(DefinitionSpacing)
		if len(r.metaKinds) == 0 {
			r.durable.Metadata[route] = now
			if err = r.opts.Save(ctx, r.durable); err != nil {
				r.status.LastError = err.Error()
				return err
			}
		}
		r.status.State = "definitions_submitted"
		return nil
	}
	r.status.State = "sampling"
	r.status.NextReport = r.nextData
	return nil
}

func Encode(seq uint32, counts Counts, extended bool) (string, Counts, bool) {
	values := counts
	clipped := false
	if !extended {
		for i, n := range values {
			if n > 255 {
				values[i] = 255
				clipped = true
			}
		}
	}
	return fmt.Sprintf("T#%03d,%03d,%03d,%03d,%03d,%03d,00000000", seq%1000, values[0], values[1], values[2], values[3], values[4]), values, clipped
}
func Definitions(identity string) map[string]string {
	prefix := fmt.Sprintf(":%-9s:", identity)
	return map[string]string{"parm": prefix + "PARM.RF Rx,Digi,RF Tx,RF > IS,IS > RF", "unit": prefix + "UNIT.pkt/10m,pkt/10m,pkt/10m,pkt/10m,pkt/10m", "eqns": prefix + "EQNS.0,1,0,0,1,0,0,1,0,0,1,0,0,1,0"}
}

// Invalidate immediately discards the baseline and queued radio work after a settings change.
func (r *Reporter) Invalidate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reset("warming_up")
	r.signature = ""
}
