// Package igate implements graywolf's APRS-IS iGate: bidirectional
// gatewaying between the RF side (decoded APRS packets coming out of
// pkg/aprs as PacketOutput submissions) and the APRS-IS internet
// backbone. It owns a single long-lived TCP session to an APRS-IS
// server, handles login/keepalive/reconnect, applies the RF->IS gating
// rules (third-party suppression, 30s duplicate window with a fixed
// beacon exemption), and applies the IS->RF filter engine plus
// txgovernor submission for traffic flowing in the reverse direction.
//
// The package exposes two adapters: IgateOutput implements
// aprs.PacketOutput for the RF->IS direction and IgateInput implements
// aprs.PacketInput for IS->RF. A simulation mode (runtime-toggleable)
// logs what would be sent to APRS-IS without actually writing to the
// socket, useful for shakedown tests on a production radio.
package igate

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/igate/filters"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
	"github.com/prometheus/client_golang/prometheus"
)

// Config is the iGate's runtime configuration. Fields marked "required"
// must be set before Start. The orchestrator will eventually source
// most of these from configstore's igate_config row (owned by agent 4C).
type Config struct {
	// Server is the APRS-IS host:port (required). Typical values are
	// "noam.aprs2.net:14580" or "rotate.aprs2.net:14580".
	Server string
	// Callsign is the iGate station identifier (required).
	Callsign string
	// Passcode is the APRS-IS login passcode ("-1" disables TX).
	Passcode string
	// ServerFilter is the APRS-IS filter string passed at login time
	// (e.g. "m/100" for a 100km radius around the station).
	ServerFilter string
	// SoftwareName and SoftwareVersion appear in the login banner.
	SoftwareName    string
	SoftwareVersion string
	// Rules seeds the IS->RF filter engine.
	Rules []filters.Rule
	// TxChannel is the radio channel IS->RF frames are submitted on.
	TxChannel uint32
	// Governor is the TX governor for IS->RF submissions. Required for
	// downlink; leave nil for IS->RF=disabled.
	Governor *txgovernor.Governor
	// SimulationMode starts with log-only APRS-IS sends when true.
	SimulationMode bool
	// Logger is optional; defaults to slog.Default().
	Logger *slog.Logger
	// Registry lets the iGate export its own Prometheus metrics into
	// graywolf's registry without needing pkg/metrics changes.
	Registry prometheus.Registerer
	// RfToIsHook is called after a packet has been successfully gated
	// from RF up to APRS-IS (or would have been, in simulation mode).
	// Optional. Used by the orchestrator to record a distinct
	// packetlog entry for the upload so it can be distinguished from
	// the raw RX entry.
	RfToIsHook func(pkt *aprs.DecodedAPRSPacket, line string)
	// now is an optional clock for tests.
	now func() time.Time
}

// Status is the current state exposed via the REST endpoint.
type Status struct {
	Connected      bool      `json:"connected"`
	Server         string    `json:"server"`
	Callsign       string    `json:"callsign"`
	SimulationMode bool      `json:"simulation_mode"`
	LastConnected  time.Time `json:"last_connected,omitempty"`
	Gated          uint64    `json:"rf_to_is_gated"`
	Downlinked     uint64    `json:"is_to_rf_gated"`
	Filtered       uint64    `json:"packets_filtered"`
	DroppedOffline uint64    `json:"rf_to_is_dropped"`
}

// Igate is the top-level coordinator: one session to APRS-IS, one
// filter engine, one RF->IS dedup cache, and runtime-toggleable
// simulation mode.
type Igate struct {
	cfg    Config
	logger *slog.Logger

	filter *filters.Engine
	dedup  *dedupCache

	mu            sync.Mutex
	connected     bool
	lastConnected time.Time
	simulation    atomic.Bool

	// inputCh fans IS->RF frames out to PacketInput consumers.
	inputCh chan *aprs.InboundPacket

	// Metrics.
	mGatedTotal     *prometheus.CounterVec // direction label: rf_to_is|is_to_rf
	mFilteredTotal  prometheus.Counter
	mConnectedGauge prometheus.Gauge
	mDroppedOffline prometheus.Counter

	// Stats snapshot for Status().
	statGated      uint64
	statDownlinked uint64
	statFiltered   uint64
	statDropped    uint64

	// session plumbing
	cancel context.CancelFunc
	done   chan struct{}
	client *client
}

// New constructs an Igate. Call Start to open the APRS-IS session.
func New(cfg Config) (*Igate, error) {
	if cfg.Callsign == "" {
		return nil, errors.New("igate: Callsign required")
	}
	if cfg.Server == "" {
		return nil, errors.New("igate: Server required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", "igate")
	if cfg.now == nil {
		cfg.now = time.Now
	}
	ig := &Igate{
		cfg:     cfg,
		logger:  logger,
		filter:  filters.New(cfg.Rules),
		dedup:   newDedupCache(),
		inputCh: make(chan *aprs.InboundPacket, 64),
		done:    make(chan struct{}),
	}
	ig.simulation.Store(cfg.SimulationMode)
	if err := ig.initMetrics(); err != nil {
		return nil, err
	}
	return ig, nil
}

func (ig *Igate) initMetrics() error {
	ig.mGatedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "igate_packets_gated_total",
		Help: "APRS packets gated by the iGate, by direction.",
	}, []string{"direction"})
	ig.mFilteredTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "igate_packets_filtered_total",
		Help: "APRS-IS packets dropped by the IS->RF filter engine.",
	})
	ig.mConnectedGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "igate_connected",
		Help: "1 when the iGate is connected to an APRS-IS server.",
	})
	ig.mDroppedOffline = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "igate_rf_to_is_dropped_total",
		Help: "RF->IS packets dropped because the APRS-IS session was down.",
	})
	if ig.cfg.Registry != nil {
		for _, c := range []prometheus.Collector{
			ig.mGatedTotal, ig.mFilteredTotal, ig.mConnectedGauge, ig.mDroppedOffline,
		} {
			if err := ig.cfg.Registry.Register(c); err != nil {
				// An AlreadyRegisteredError is fine (tests may call
				// New twice); anything else is a real problem.
				are := prometheus.AlreadyRegisteredError{}
				if !errors.As(err, &are) {
					return err
				}
			}
		}
	}
	return nil
}

// Start opens the APRS-IS session and launches the supervising
// goroutine. Safe to call once; subsequent calls return an error.
func (ig *Igate) Start(ctx context.Context) error {
	ig.mu.Lock()
	if ig.cancel != nil {
		ig.mu.Unlock()
		return errors.New("igate: already started")
	}
	sessCtx, cancel := context.WithCancel(ctx)
	ig.cancel = cancel
	ig.mu.Unlock()
	go ig.supervise(sessCtx)
	return nil
}

// Stop cancels the session and waits for the supervisor to exit.
func (ig *Igate) Stop() {
	ig.mu.Lock()
	cancel := ig.cancel
	ig.cancel = nil
	ig.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-ig.done
}

// supervise dials, runs one session, applies backoff, loops.
func (ig *Igate) supervise(ctx context.Context) {
	defer close(ig.done)
	bo := newBackoff(time.Now().UnixNano())
	ig.client = newClient(
		ig.cfg,
		ig.logger,
		ig.handleISLine,
		func() { bo.Reset(); ig.onConnected() },
		ig.onLost,
	)
	for {
		if ctx.Err() != nil {
			return
		}
		err := ig.client.run(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			ig.logger.Warn("aprs-is session ended", "err", err)
		}
		delay := bo.Next()
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (ig *Igate) onConnected() {
	ig.mu.Lock()
	ig.connected = true
	ig.lastConnected = ig.cfg.now()
	ig.mu.Unlock()
	ig.mConnectedGauge.Set(1)
	ig.logger.Info("aprs-is connected", "server", ig.cfg.Server, "callsign", ig.cfg.Callsign)
}

func (ig *Igate) onLost() {
	ig.mu.Lock()
	ig.connected = false
	ig.mu.Unlock()
	ig.mConnectedGauge.Set(0)
}

// handleISLine is called for every non-comment line received from
// APRS-IS. It parses, runs the filter engine, and submits to txgovernor.
func (ig *Igate) handleISLine(line string) {
	frame, err := parseTNC2(line)
	if err != nil {
		ig.logger.Debug("aprs-is tnc2 parse failed", "err", err, "line", line)
		return
	}
	// Decode just enough to evaluate rules (filter engine reads
	// Source/Message/Object on the decoded struct).
	pkt, err := aprs.Parse(frame)
	if err != nil || pkt == nil {
		// Parse failure is non-fatal; we still know source/dest from
		// the frame header, so construct a minimal decoded packet.
		pkt = &aprs.DecodedAPRSPacket{Source: frame.Source.String(), Dest: frame.Dest.String()}
	}
	if !ig.filter.Allow(pkt) {
		atomic.AddUint64(&ig.statFiltered, 1)
		ig.mFilteredTotal.Inc()
		return
	}
	if ig.cfg.Governor == nil {
		ig.logger.Debug("IS->RF drop: no governor configured")
		return
	}
	if err := ig.cfg.Governor.Submit(context.Background(), ig.cfg.TxChannel, frame, txgovernor.SubmitSource{
		Kind:     "igate",
		Detail:   "is2rf",
		Priority: txgovernor.PriorityIGateMsg,
	}); err != nil {
		ig.logger.Warn("IS->RF submit failed", "err", err)
		return
	}
	atomic.AddUint64(&ig.statDownlinked, 1)
	ig.mGatedTotal.WithLabelValues("is_to_rf").Inc()

	// Also publish into the PacketInput fan-out for any listeners.
	select {
	case ig.inputCh <- &aprs.InboundPacket{Raw: mustEncode(frame), Source: "aprs-is", Channel: int(ig.cfg.TxChannel)}:
	default:
	}
}

func mustEncode(f *ax25.Frame) []byte {
	raw, err := f.Encode()
	if err != nil {
		return nil
	}
	return raw
}

// gateRFToIS is called from IgateOutput.SendPacket to run the RF->IS
// gating pipeline.
func (ig *Igate) gateRFToIS(pkt *aprs.DecodedAPRSPacket) {
	if pkt == nil {
		return
	}
	// Rule: never gate third-party traffic (already came from the net).
	if pkt.ThirdParty != nil || pkt.Type == aprs.PacketThirdParty {
		return
	}
	// Rule: never gate packets whose path already contains a TCPIP/
	// TCPXX/NOGATE/RFONLY marker (the APRS-IS convention for
	// already-gated or do-not-gate traffic).
	if pathBlocksGating(pkt.Path) {
		return
	}
	// Dedup on (source + info bytes).
	info := infoBytes(pkt)
	if len(info) == 0 {
		return
	}
	key := pkt.Source + "\x00" + string(info)
	fixed := isFixedPositionBeacon(pkt)
	if !ig.dedup.shouldGate(key, fixed) {
		return
	}
	// Connection check. If disconnected, drop and count.
	ig.mu.Lock()
	connected := ig.connected
	ig.mu.Unlock()
	if !connected {
		atomic.AddUint64(&ig.statDropped, 1)
		ig.mDroppedOffline.Inc()
		return
	}
	line, err := encodeTNC2(pkt, ig.cfg.Callsign)
	if err != nil {
		ig.logger.Debug("igate: encode tnc2 failed", "err", err)
		return
	}
	if ig.simulation.Load() {
		ig.logger.Info("igate simulation send", "line", line)
		atomic.AddUint64(&ig.statGated, 1)
		ig.mGatedTotal.WithLabelValues("rf_to_is").Inc()
		if ig.cfg.RfToIsHook != nil {
			ig.cfg.RfToIsHook(pkt, line)
		}
		return
	}
	if err := ig.client.WriteLine(line); err != nil {
		ig.logger.Warn("igate: aprs-is write failed", "err", err)
		return
	}
	atomic.AddUint64(&ig.statGated, 1)
	ig.mGatedTotal.WithLabelValues("rf_to_is").Inc()
	if ig.cfg.RfToIsHook != nil {
		ig.cfg.RfToIsHook(pkt, line)
	}
}

func pathBlocksGating(path []string) bool {
	for _, p := range path {
		u := strings.ToUpper(strings.TrimSuffix(p, "*"))
		switch {
		case strings.HasPrefix(u, "TCPIP"), strings.HasPrefix(u, "TCPXX"):
			return true
		case u == "NOGATE", u == "RFONLY":
			return true
		}
	}
	return false
}

// isFixedPositionBeacon reports whether a packet is a plain stationary
// position report (no course/speed, no message or telemetry), for the
// dedup fixed-beacon exemption (>1min apart is not suppressed).
func isFixedPositionBeacon(pkt *aprs.DecodedAPRSPacket) bool {
	if pkt.Type != aprs.PacketPosition || pkt.Position == nil {
		return false
	}
	if pkt.Position.HasCourse && pkt.Position.Speed > 0 {
		return false
	}
	return true
}

// SetSimulationMode toggles simulation-mode at runtime.
func (ig *Igate) SetSimulationMode(on bool) error {
	ig.simulation.Store(on)
	ig.logger.Info("igate simulation mode", "enabled", on)
	return nil
}

// Status returns a runtime snapshot of the iGate for REST consumers.
func (ig *Igate) Status() Status {
	ig.mu.Lock()
	defer ig.mu.Unlock()
	return Status{
		Connected:      ig.connected,
		Server:         ig.cfg.Server,
		Callsign:       ig.cfg.Callsign,
		SimulationMode: ig.simulation.Load(),
		LastConnected:  ig.lastConnected,
		Gated:          atomic.LoadUint64(&ig.statGated),
		Downlinked:     atomic.LoadUint64(&ig.statDownlinked),
		Filtered:       atomic.LoadUint64(&ig.statFiltered),
		DroppedOffline: atomic.LoadUint64(&ig.statDropped),
	}
}
