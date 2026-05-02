// Package igate implements graywolf's APRS-IS iGate: bidirectional
// gatewaying between the RF side (decoded APRS packets coming out of
// pkg/aprs as PacketOutput submissions) and the APRS-IS internet
// backbone. It owns a single long-lived TCP session to an APRS-IS
// server, handles login/keepalive/reconnect, and gates traffic in both
// directions.
//
// RF→IS suppresses only third-party packets, NOGATE/RFONLY paths, and
// locally-originated messages echoed back to us by a digipeater —
// aprsc's IGATE-HINTS explicitly says RX iGates must NOT dedup
// client-side.
//
// IS→RF is a two-tier policy: directed messages follow the APRS iGate
// spec (addressed to a station heard directly on RF within 30 min, not
// a bulletin/NWS broadcast), while non-message traffic (positions,
// weather, telemetry, …) is gated only when the source shares the
// iGate's base callsign but is not the iGate itself — so an operator
// can echo their internet-fed weather station onto local RF without
// re-broadcasting strangers' traffic. Every IS→RF frame is wrapped in
// APRS third-party format and passes the operator's filter engine
// before reaching txgovernor.
//
// The package exposes two adapters: IgateOutput implements
// aprs.PacketOutput for the RF→IS direction and IgateInput implements
// aprs.PacketInput for IS→RF. A simulation mode (runtime-toggleable)
// logs what would be sent to APRS-IS without actually writing to the
// socket, useful for shakedown tests on a production radio.
//
// IGATE-HINTS compliance audit
// (https://github.com/hessu/aprsc/blob/main/doc/IGATE-HINTS.md)
//
//  1. Packets modified by iGates — client.go reads with bufio.ReadString('\n')
//     and strips only "\r\n" via strings.TrimRight; whitespace, non-ASCII
//     bytes, and NULs inside the info field are preserved byte-for-byte.
//  2. C-string truncation — Go strings are byte-counted (not NUL-terminated);
//     parseTNC2/encodeTNC2 pass the info field as []byte through ax25.Frame.Info,
//     so embedded 0x00 / 0x1C survive intact.
//  3. Character encoding — no UTF-8 decoding is applied to APRS-IS lines or
//     info fields; TCP is binary by default in Go's net package.
//  4. TX-capable iGate packet selection — shouldForwardISToRF enforces the
//     APRS iGate spec for messages (directed, heard-direct addressee, not a
//     broadcast) plus loop prevention on every IS→RF packet. Non-messages
//     additionally require the source to share the iGate's base callsign
//     but not be the iGate itself — an operator can echo their own
//     SSIDs (e.g. an internet-fed weather station) but not strangers'
//     non-message traffic. Strangers' messages addressed to heard-direct
//     stations still forward normally — that is the iGate's core job.
//     The user filter then applies as a narrower layer.
//  5. Third-party wrap — every IS→RF frame passes through wrapThirdParty,
//     producing APRS101 §20 format "}origSrc>origDest[,origPath…],TCPIP,IGATECALL*:info".
//  6. Duplicate filtering — RF→IS does NOT dedup (by explicit design; APRS-IS
//     servers dedup content-aware). NOGATE / RFONLY / TCPIP path markers are
//     honored via pathBlocksGating.
//  7. DNS caching — client.go builds a fresh net.Dialer on every reconnect;
//     Go's resolver does not cache across calls, so each TCP connection
//     re-resolves the hostname (required for rotate.aprs2.net load balancing).
//  8. Multiple connections — supervise() drives a single *client with
//     serialized reconnects; there is no parallel connection to APRS-IS.
package igate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/callsign"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/igate/filters"
	"github.com/chrissnell/graywolf/pkg/internal/backoff"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
	"github.com/prometheus/client_golang/prometheus"
)

// igateSubmitTimeout bounds how long a single IS->RF submit may block on
// the TX governor. If exceeded, the packet is dropped and counted. This
// timeout exists to prevent the APRS-IS read loop (which calls Submit
// inline from its receive goroutine) from stalling when the TX queue is
// saturated: a stalled read loop stops servicing keepalives, which
// cascades into a silent reconnect loop with no IS->RF gating.
const igateSubmitTimeout = 2 * time.Second

// submitDropLogInterval rate-limits the debug log emitted when an IS->RF
// submit is dropped, so a saturated governor cannot flood the logs.
const submitDropLogInterval = 10 * time.Second

// Config is the iGate's runtime configuration. Fields marked "required"
// must be set before Start. The orchestrator sources most of these from
// configstore (igate_config row plus the StationConfig singleton for
// StationCallsign).
type Config struct {
	// Server is the APRS-IS host:port (required). Typical values are
	// "noam.aprs2.net:14580" or "rotate.aprs2.net:14580".
	Server string
	// StationCallsign is the resolved station identifier (required). The
	// iGate has no per-station override (per design D3: iGate login
	// identity and messaging identity are always the station callsign)
	// so callers resolve via ResolveStationCallsign and pass the result
	// in. The APRS-IS passcode is derived from this at login time via
	// callsign.APRSPasscode — not carried on Config.
	StationCallsign string
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
	// ChannelModes resolves Channel.Mode at TX time. When the iGate's
	// configured TxChannel is "packet"-mode, the IS->RF runtime gate
	// drops the frame and logs a Warn (see handleISLine). Nil = treat
	// every channel as ChannelModeAPRS (preserves the legacy
	// any-channel-does-anything behavior). Lookup errors are treated
	// as APRS-mode at the gate point (fail-open).
	ChannelModes configstore.ChannelModeLookup
	// Governor is the TX governor for IS->RF submissions. Required for
	// downlink; leave nil for IS->RF=disabled. Declared as the
	// canonical txgovernor.TxSink interface so tests can inject a
	// stub; *txgovernor.Governor satisfies it.
	Governor txgovernor.TxSink
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
	// IsRxHook is called for every packet successfully received from
	// APRS-IS, regardless of whether the local IS->RF filter engine
	// would allow it to be transmitted. Used to record IS-heard stations
	// in the packet log / station cache for map display, which must not
	// be coupled to the transmit-gating filter. Optional.
	IsRxHook func(pkt *aprs.DecodedAPRSPacket, line string)
	// LocalOrigin is an optional lookup for locally-originated messages.
	// When non-nil and SuppressLocalMessageReGate is true, the gateway
	// skips RF->IS gating for any message packet whose (source, msg_id)
	// is present in the ring — this prevents our own outbound messages
	// from being re-gated to APRS-IS after a digipeater repeats them
	// back onto RF.
	LocalOrigin LocalOriginRing
	// SuppressLocalMessageReGate enables the LocalOrigin consult step.
	// Defaults to true in Phase 5 wiring; operators can set false to
	// preserve legacy behavior (re-gate every packet).
	SuppressLocalMessageReGate bool
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

	// stationCallsign is the resolved-at-construction station callsign.
	// Held separately from cfg so that every downstream site (login,
	// third-party wrap, loop prevention, Status) reads from one field
	// and the cfg.StationCallsign field is only consulted at New() time.
	stationCallsign string

	filter atomic.Pointer[filters.Engine]
	heard  *heardDirect

	mu            sync.Mutex
	connected     bool
	lastConnected time.Time
	simulation    atomic.Bool

	// txChannel mirrors cfg.TxChannel but is mutable at runtime via
	// SetTxChannel so an iGate-config save can retarget IS→RF without
	// a daemon restart. Reads on the IS→RF hot path are lock-free.
	txChannel atomic.Uint32

	// inputCh fans IS->RF frames out to PacketInput consumers.
	inputCh chan *aprs.InboundPacket

	// Metrics.
	mGatedTotal     *prometheus.CounterVec // direction label: rf_to_is|is_to_rf
	mFilteredTotal  prometheus.Counter
	mConnectedGauge prometheus.Gauge
	mDroppedOffline prometheus.Counter
	mSubmitDropped  prometheus.Counter
	mFanoutDropped  prometheus.Counter

	// Stats snapshot for Status().
	statGated      uint64
	statDownlinked uint64
	statFiltered   uint64
	statDropped    uint64

	// session plumbing
	//
	// sessCtx holds the context handleISLine uses as the parent for
	// its bounded per-submit timeout. It is swapped in at Start() time
	// and loaded lock-free on every IS->RF line; keeping it out of
	// ig.mu avoids coupling the read-loop hot path to the RF->IS
	// connected/lastConnected mutex.
	sessCtx atomic.Pointer[sessCtxHolder]
	cancel  context.CancelFunc
	done    chan struct{}
	client  *client

	// lastSubmitDropLogNano holds the UnixNano of the most recent
	// rate-limited IS->RF submit-drop debug log, for throttling.
	lastSubmitDropLogNano atomic.Int64
}

// sessCtxHolder wraps a context.Context for storage in an atomic.Pointer.
// The wrapper sidesteps atomic.Value's "consistent dynamic type"
// requirement, since different context implementations (Background,
// WithCancel, WithTimeout) have different underlying types.
type sessCtxHolder struct{ ctx context.Context }

// New constructs an Igate. Call Start to open the APRS-IS session.
func New(cfg Config) (*Igate, error) {
	// iGate has no per-feature override — login identity IS the station
	// callsign (D3). An empty override + empty/N0CALL station callsign
	// is a refuse-to-start condition; the wiring layer will typically
	// avoid constructing us in that case, but the defense here guarantees
	// we never open an APRS-IS session under N0CALL.
	stationCall, err := callsign.Resolve("", cfg.StationCallsign)
	if err != nil {
		return nil, fmt.Errorf("igate: station callsign: %w", err)
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
		cfg:             cfg,
		logger:          logger,
		stationCallsign: stationCall,
		heard:           newHeardDirect(),
		inputCh:         make(chan *aprs.InboundPacket, 64),
		done:            make(chan struct{}),
	}
	ig.filter.Store(filters.New(cfg.Rules))
	ig.sessCtx.Store(&sessCtxHolder{ctx: context.Background()})
	ig.simulation.Store(cfg.SimulationMode)
	ig.txChannel.Store(cfg.TxChannel)
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
	ig.mSubmitDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "igate_is_to_rf_submit_dropped_total",
		Help: "IS->RF packets dropped because the TX governor submit timed out, was cancelled, or returned an error.",
	})
	ig.mFanoutDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "igate_is_to_rf_fanout_dropped_total",
		Help: "IS->RF frames dropped from the PacketInput fan-out because no consumer was ready.",
	})
	if ig.cfg.Registry != nil {
		for _, c := range []prometheus.Collector{
			ig.mGatedTotal, ig.mFilteredTotal, ig.mConnectedGauge, ig.mDroppedOffline, ig.mSubmitDropped, ig.mFanoutDropped,
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
	ig.sessCtx.Store(&sessCtxHolder{ctx: sessCtx})
	ig.heard.startSweeper(sessCtx, heardDirectTTL, heardSweepInterval)
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
	bo := backoff.New(backoff.Config{
		Initial:    time.Second,
		Max:        5 * time.Minute,
		JitterFrac: 0.25,
		Rand:       rand.New(rand.NewSource(time.Now().UnixNano())),
	})
	ig.client = newClient(
		ig.cfg,
		ig.stationCallsign,
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
	ig.logger.Info("aprs-is connected", "server", ig.cfg.Server, "callsign", ig.stationCallsign)
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
	// Provenance: this ingress path is APRS-IS, so downstream consumers
	// (messages router, Source badge, IS-mirror ack logic) can rely on
	// Direction to distinguish from RF arrivals.
	pkt.Direction = aprs.DirectionIS
	// Map display and packet-log capture happen for every received IS
	// packet — the hook must fire regardless of the spec/filter gates
	// below, which only govern RF transmission.
	if ig.cfg.IsRxHook != nil {
		ig.cfg.IsRxHook(pkt, line)
	}
	// Policy gate: messages follow the APRS iGate spec (heard-direct
	// addressee); non-messages require only loop prevention so the
	// user filter below can decide which sources the operator wants
	// echoed onto RF (e.g. their own internet-connected weather
	// station). See shouldForwardISToRF for the full rules.
	if !ig.shouldForwardISToRF(pkt) {
		atomic.AddUint64(&ig.statFiltered, 1)
		ig.mFilteredTotal.Inc()
		return
	}
	if !ig.filter.Load().Allow(pkt) {
		atomic.AddUint64(&ig.statFiltered, 1)
		ig.mFilteredTotal.Inc()
		return
	}
	if ig.cfg.Governor == nil {
		ig.logger.Debug("IS->RF drop: no governor configured")
		return
	}
	// Wrap in APRS third-party format so other iGates and apps can see
	// the packet originated on the internet side (prevents re-gating
	// loops and stops receivers from treating the original sender as
	// an RF-local station).
	wrapped, err := wrapThirdParty(frame, ig.stationCallsign)
	if err != nil {
		ig.logger.Debug("IS->RF drop: third-party wrap failed", "err", err)
		return
	}
	// sessCtx is initialized in New and replaced with the real session
	// context in Start, so Load always returns a non-nil holder on the
	// read-loop hot path.
	parent := ig.sessCtx.Load().ctx
	submitCtx, cancel := context.WithTimeout(parent, igateSubmitTimeout)
	txCh := ig.txChannel.Load()
	if ig.cfg.ChannelModes != nil {
		mode, _ := ig.cfg.ChannelModes.ModeForChannel(submitCtx, txCh)
		if mode == configstore.ChannelModePacket {
			ig.logger.Warn("IS->RF drop: tx channel is packet-mode",
				"channel", txCh)
			cancel()
			ig.mSubmitDropped.Inc()
			return
		}
	}
	err = ig.cfg.Governor.Submit(submitCtx, txCh, wrapped, txgovernor.SubmitSource{
		Kind:     "igate",
		Detail:   "is2rf",
		Priority: txgovernor.PriorityIGateMsg,
	})
	cancel()
	if err != nil {
		ig.mSubmitDropped.Inc()
		ig.logSubmitDrop(wrapped, err)
		return
	}
	atomic.AddUint64(&ig.statDownlinked, 1)
	ig.mGatedTotal.WithLabelValues("is_to_rf").Inc()

	// Also publish into the PacketInput fan-out for any listeners.
	// Publish the wrapped frame (what was actually transmitted) —
	// consumers of IgateInput expect the transmittable form. Drops
	// are counted but not logged: inputCh is a best-effort tap and a
	// slow consumer should not back-pressure gating.
	select {
	case ig.inputCh <- &aprs.InboundPacket{Raw: mustEncode(wrapped), Source: "aprs-is", Channel: int(txCh)}:
	default:
		ig.mFanoutDropped.Inc()
	}
}

// shouldForwardISToRF decides whether an APRS-IS packet is eligible for
// IS→RF transmission, per a two-tier policy:
//
//   - Directed messages follow the strict APRS iGate spec: addressed to
//     a station heard directly on RF within heardDirectTTL, not a
//     bulletin/NWS broadcast, and not already transited via us.
//   - Non-message traffic (positions, weather, telemetry, status, …)
//     must be sourced from one of the operator's own SSIDs (base-call
//     match) but NOT from the iGate's own transmitting call itself.
//     The intent: an operator can echo their internet-connected weather
//     station or family tracker onto local RF, but cannot inadvertently
//     re-broadcast strangers' packets or their own iGate beacons.
//
// Loop prevention applies to both branches: if our callsign already
// appears in the path, the packet has transited us and must not be
// re-transmitted. The user-configurable filter runs after this gate
// as a narrower layer.
func (ig *Igate) shouldForwardISToRF(pkt *aprs.DecodedAPRSPacket) bool {
	if pkt == nil {
		return false
	}
	if ig.pathContainsSelf(pkt.Path) {
		return false
	}
	if pkt.Type != aprs.PacketMessage {
		return ig.sourceIsOwnSSID(pkt.Source)
	}
	if pkt.Message == nil {
		return false
	}
	if pkt.Message.IsBulletin || pkt.Message.IsNWS {
		return false
	}
	addressee := strings.TrimSpace(pkt.Message.Addressee)
	if addressee == "" {
		return false
	}
	return ig.heard.HeardWithin(addressee, heardDirectTTL)
}

// sourceIsOwnSSID reports whether source shares the iGate's base call
// but is not the iGate's exact transmitting callsign. Returns false
// for an empty source or empty configured callsign. Comparison is
// case-insensitive and whitespace-tolerant. The base-call-match / not-
// exact-match pair is the non-message IS→RF ownership gate: it lets an
// N0CALL-10 iGate forward N0CALL-7 (e.g. a family tracker or weather
// rig uploading via internet) while rejecting N0CALL-10 itself (which
// would be a self-echo) and rejecting anything not on our base call.
func (ig *Igate) sourceIsOwnSSID(source string) bool {
	source = strings.ToUpper(strings.TrimSpace(source))
	if source == "" {
		return false
	}
	me := strings.ToUpper(strings.TrimSpace(ig.stationCallsign))
	if me == "" {
		return false
	}
	if source == me {
		return false
	}
	srcBase := source
	if i := strings.IndexByte(srcBase, '-'); i > 0 {
		srcBase = srcBase[:i]
	}
	myBase := me
	if i := strings.IndexByte(myBase, '-'); i > 0 {
		myBase = myBase[:i]
	}
	return srcBase == myBase
}

// pathContainsSelf reports whether our own callsign (base, case- and
// SSID-insensitive) appears anywhere in the packet's RF path. Used for
// IS→RF loop prevention: if we've already handled this packet, we
// must not re-transmit it.
func (ig *Igate) pathContainsSelf(path []string) bool {
	me := strings.ToUpper(strings.TrimSpace(ig.stationCallsign))
	if i := strings.IndexByte(me, '-'); i > 0 {
		me = me[:i]
	}
	if me == "" {
		return false
	}
	for _, p := range path {
		hop := strings.ToUpper(strings.TrimSuffix(strings.TrimSpace(p), "*"))
		if i := strings.IndexByte(hop, '-'); i > 0 {
			hop = hop[:i]
		}
		if hop == me {
			return true
		}
	}
	return false
}

// logSubmitDrop emits a rate-limited debug line for an IS->RF submit
// that was dropped (timeout, cancellation, or governor error). The full
// frame info is intentionally omitted because APRS-IS traffic is high
// volume and logs would explode under saturation.
func (ig *Igate) logSubmitDrop(frame *ax25.Frame, err error) {
	now := time.Now().UnixNano()
	last := ig.lastSubmitDropLogNano.Load()
	if now-last < int64(submitDropLogInterval) {
		return
	}
	if !ig.lastSubmitDropLogNano.CompareAndSwap(last, now) {
		return
	}
	var src, dst string
	if frame != nil {
		src = frame.Source.String()
		dst = frame.Dest.String()
	}
	ig.logger.Debug("IS->RF submit dropped", "source", src, "dest", dst, "err", err)
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
	// Record every direct-RF arrival in the heard-direct tracker so
	// the IS->RF gate knows which stations are in range for
	// message-delivery. We do this BEFORE any drop checks because
	// even packets we choose not to gate up still tell us who is
	// audible on RF. "Direct" means no digipeater has repeated the
	// frame yet (no '*' marker in the path).
	if pkt.Source != "" && pathIsDirect(pkt.Path) {
		ig.heard.Record(pkt.Source)
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
	// Rule: never gate a message packet that we originated ourselves.
	// The messages sender records (source, msg_id) in the LocalOrigin
	// ring on every submit; a later RF read of the same packet (via
	// a digipeater repeat) hits the ring and is suppressed here so
	// APRS-IS doesn't see our own outbound twice.
	if ig.shouldSuppressLocalMessage(pkt) {
		return
	}
	// NOTE: no client-side dedup. IGATE-HINTS §"iGates dropping
	// duplicate packets unnecessarily" says RX iGates must not dedup:
	// APRS-IS servers dedup content-aware and actually benefit from
	// receiving duplicates (useful for infrastructure analysis).
	//
	// Connection check. If disconnected, drop and count.
	ig.mu.Lock()
	connected := ig.connected
	ig.mu.Unlock()
	if !connected {
		atomic.AddUint64(&ig.statDropped, 1)
		ig.mDroppedOffline.Inc()
		return
	}
	line, err := encodeTNC2(pkt, ig.stationCallsign)
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

// SendLine writes a pre-formatted TNC-2 line to APRS-IS. Used by the
// beacon scheduler to duplicate a beacon to APRS-IS when the operator
// has opted in. Returns an error if the igate is not connected.
func (ig *Igate) SendLine(line string) error {
	ig.mu.Lock()
	connected := ig.connected
	ig.mu.Unlock()
	if !connected {
		return errors.New("igate: not connected")
	}
	if ig.simulation.Load() {
		ig.logger.Info("igate simulation beacon send", "line", line)
		return nil
	}
	return ig.client.WriteLine(line)
}

// Reconfigure updates the server filter, IS→RF gating rules, and
// governor at runtime. If the server filter changed, the APRS-IS
// connection is closed so the supervisor reconnects with the new
// filter (which is sent at login time). Pass a non-nil governor to
// enable IS→RF gating, or nil to disable it.
func (ig *Igate) Reconfigure(serverFilter string, rules []filters.Rule, gov txgovernor.TxSink) {
	ig.filter.Store(filters.New(rules))

	ig.mu.Lock()
	filterChanged := ig.cfg.ServerFilter != serverFilter
	ig.cfg.ServerFilter = serverFilter
	ig.cfg.Rules = rules
	ig.cfg.Governor = gov
	ig.mu.Unlock()

	if ig.client != nil {
		ig.client.mu.Lock()
		ig.client.cfg.ServerFilter = serverFilter
		ig.client.cfg.Rules = rules
		ig.client.mu.Unlock()

		if filterChanged {
			ig.logger.Info("server filter changed, reconnecting", "filter", serverFilter)
			ig.client.closeConn()
		}
	}
	ig.logger.Info("igate reconfigured", "server_filter", serverFilter, "rules", len(rules))
}

// TxChannel returns the live IS→RF channel ID. Reads are lock-free.
func (ig *Igate) TxChannel() uint32 { return ig.txChannel.Load() }

// SetTxChannel updates the IS→RF channel at runtime. A zero value is
// ignored. Concurrent with the IS→RF submit path; safe via atomic.
func (ig *Igate) SetTxChannel(ch uint32) {
	if ch == 0 {
		return
	}
	prev := ig.txChannel.Swap(ch)
	if prev != ch {
		ig.logger.Info("igate tx channel updated", "previous", prev, "new", ch)
	}
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
		Callsign:       ig.stationCallsign,
		SimulationMode: ig.simulation.Load(),
		LastConnected:  ig.lastConnected,
		Gated:          atomic.LoadUint64(&ig.statGated),
		Downlinked:     atomic.LoadUint64(&ig.statDownlinked),
		Filtered:       atomic.LoadUint64(&ig.statFiltered),
		DroppedOffline: atomic.LoadUint64(&ig.statDropped),
	}
}
