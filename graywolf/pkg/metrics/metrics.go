// Package metrics exposes graywolf's Prometheus metrics and a helper to
// fold Rust-side StatusUpdate messages into them.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// Metrics owns a Prometheus registry and the graywolf metric vectors.
type Metrics struct {
	Registry *prometheus.Registry

	RxFrames       *prometheus.CounterVec
	DcdTransitions *prometheus.CounterVec
	DcdDropped     prometheus.Counter
	ChildRestarts  prometheus.Counter
	AudioLevel     *prometheus.GaugeVec
	DcdActive      *prometheus.GaugeVec
	ChildUp        prometheus.Gauge

	// Phase 2: protocol + tx governor metrics.
	KissClientsActive *prometheus.GaugeVec // per interface name
	KissDecodeErrors  prometheus.Counter
	AgwClientsActive  prometheus.Gauge
	AgwDecodeErrors   *prometheus.CounterVec // label: stage ("initial" | "fallback")
	TxFrames          *prometheus.CounterVec // per channel
	TxRateLimited     prometheus.Counter
	TxDeduped         prometheus.Counter
	TxQueueDropped    prometheus.Counter
	AprsOutDropped    prometheus.Counter

	// digipeater, packet log, and beacon metrics.
	DigipeaterPackets  prometheus.Counter
	DigipeaterDeduped  prometheus.Counter
	PacketlogEntries   prometheus.Gauge
	BeaconPackets      *prometheus.CounterVec // label: "type"
	BeaconFired        *prometheus.CounterVec // labels: "beacon_name", "result" ("sent" | "skipped_busy")
	BeaconEncodeErrors *prometheus.CounterVec // label: "beacon_name"
	BeaconSubmitErrors *prometheus.CounterVec // labels: "beacon_name", "reason"
	SmartBeaconRate    *prometheus.GaugeVec   // label: "channel"
	GpsParseErrors     *prometheus.CounterVec // label: "source" ("gpsd" | "nmea")

	// KISS modem/TNC-mode observability. Phase 5 of the KISS modem/TNC
	// plan.
	KissIngressFrames       *prometheus.CounterVec // labels: "interface_id", "mode"
	KissTncRxDispatched     *prometheus.CounterVec // label: "interface_id"
	KissTncIngressDropped   *prometheus.CounterVec // labels: "interface_id", "reason" ("rate_limit" | "queue_full")
	KissBroadcastSuppressed *prometheus.CounterVec // labels: "interface_id", "reason" ("self_loop")
	RxFanoutDropped         *prometheus.CounterVec // label: "producer" ("kiss_tnc"; "modem" is a blocking producer and never drops at the fanout)

	// Phase 3 (KISS TCP-client / channel-backing plan): TX backend
	// dispatcher observability. Labels:
	//   TxBackendSubmits: channel, backend ("modem" | "kiss"),
	//                     instance (e.g. "modem", "kiss-3"),
	//                     outcome ("ok" | "err" | "backend_busy" | "backend_down")
	//   TxNoBackend:      channel
	//   TxBackendDuration: channel, backend
	//   KissInstanceTxQueueDepth: interface_id (gauge)
	TxBackendSubmits         *prometheus.CounterVec
	TxNoBackend              *prometheus.CounterVec
	TxBackendDuration        *prometheus.HistogramVec
	KissInstanceTxQueueDepth *prometheus.GaugeVec

	// Phase 4: KISS tcp-client supervisor observability.
	//   KissClientConnected: gauge per interface (1 = connected, 0 = not)
	//   KissClientReconnects: counter per interface (cumulative dials since process start)
	//   KissClientBackoffSeconds: gauge per interface (current backoff delay in seconds)
	//   KissClientTxDrops: counter per interface × reason ("busy" | "down")
	KissClientConnected      *prometheus.GaugeVec
	KissClientReconnects     *prometheus.CounterVec
	KissClientBackoffSeconds *prometheus.GaugeVec
	KissClientTxDrops        *prometheus.CounterVec

	// IgateFilterRecompositions counts each successful recompose-and-apply
	// cycle in the iGate reload path — i.e. a tactical mutation (or
	// iGate config save) produced a composed filter that differed from
	// the last-applied value and was pushed into the running client.
	// Cycles that compose the same filter as before do not increment
	// (no reconnect triggered).
	IgateFilterRecompositions prometheus.Counter

	// Track last-seen cumulative DCD transition counts per channel so we can
	// translate the Rust modem's absolute counters into Prometheus counter
	// deltas. (Rx frame counts come directly from ObserveReceivedFrame so we
	// don't double-count them from StatusUpdate.)
	lastDcdTransitions map[uint32]uint64
}

// New builds a Metrics with a private registry.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		Registry: reg,
		RxFrames: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_rx_frames_total",
			Help: "AX.25 frames successfully received, by channel.",
		}, []string{"channel"}),
		DcdTransitions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_dcd_transitions_total",
			Help: "Data-carrier-detect state transitions, by channel.",
		}, []string{"channel"}),
		DcdDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "graywolf_modembridge_dcd_dropped_total",
			Help: "DCD state-change events dropped because a subscriber's buffered channel was full.",
		}),
		ChildRestarts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "graywolf_child_restarts_total",
			Help: "Number of times the Rust modem child process was restarted.",
		}),
		AudioLevel: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "graywolf_audio_level",
			Help: "Latest peak audio level (0..1) reported by the modem, by channel.",
		}, []string{"channel"}),
		DcdActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "graywolf_dcd_active",
			Help: "Current DCD state (1 = carrier detected) by channel.",
		}, []string{"channel"}),
		ChildUp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "graywolf_child_up",
			Help: "1 if the Rust modem child process is currently running.",
		}),
		KissClientsActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "graywolf_kiss_clients_active",
			Help: "Connected KISS clients, by interface name.",
		}, []string{"interface"}),
		KissDecodeErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "graywolf_kiss_decode_errors_total",
			Help: "KISS data frames that failed AX.25 decoding and were dropped.",
		}),
		AgwClientsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "graywolf_agw_clients_active",
			Help: "Connected AGWPE clients.",
		}),
		AgwDecodeErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_agw_decode_errors_total",
			Help: "Count of AGW frames that failed AX.25 decoding. 'initial' counts frames that required the skip-byte fallback; 'fallback' counts frames that failed both attempts and were dropped.",
		}, []string{"stage"}),
		TxFrames: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_tx_frames_total",
			Help: "AX.25 frames transmitted by the governor, by channel.",
		}, []string{"channel"}),
		TxRateLimited: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "graywolf_tx_rate_limited_total",
			Help: "Frames deferred because a channel's rate limit was reached.",
		}),
		TxDeduped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "graywolf_tx_deduped_total",
			Help: "Frames suppressed by the tx governor deduplication window.",
		}),
		TxQueueDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "graywolf_tx_queue_dropped_total",
			Help: "Frames dropped because the tx governor queue was full.",
		}),
		AprsOutDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "graywolf_aprs_out_dropped_total",
			Help: "Decoded APRS packets dropped because the output worker queue was full.",
		}),
		DigipeaterPackets: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "graywolf_digipeater_packets_total",
			Help: "Packets successfully digipeated (path mutated and resubmitted).",
		}),
		DigipeaterDeduped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "graywolf_digipeater_deduped_total",
			Help: "Packets suppressed by the digipeater dedup window.",
		}),
		PacketlogEntries: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "graywolf_packetlog_entries_current",
			Help: "Current number of entries in the packet log ring buffer.",
		}),
		BeaconPackets: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_beacon_packets_total",
			Help: "Beacon packets transmitted, by beacon type.",
		}, []string{"type"}),
		BeaconFired: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_beacon_fired_total",
			Help: "Beacon fire attempts, labeled by result. 'sent' counts successful submissions; 'skipped_busy' counts fires dropped because the scheduler's bounded worker pool was saturated and no slot was available.",
		}, []string{"beacon_name", "result"}),
		BeaconEncodeErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_beacon_encode_errors_total",
			Help: "Beacons that failed AX.25 encoding and were not transmitted.",
		}, []string{"beacon_name"}),
		BeaconSubmitErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_beacon_submit_errors_total",
			Help: "Beacons that the TX governor rejected, classified by reason: queue_full (back-pressure), timeout (context deadline exceeded), or other.",
		}, []string{"beacon_name", "reason"}),
		SmartBeaconRate: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "graywolf_smartbeacon_rate_seconds",
			Help: "Current SmartBeaconing interval in seconds, by channel.",
		}, []string{"channel"}),
		GpsParseErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_gps_parse_errors_total",
			Help: "GPS messages that failed to parse and were dropped. Source 'gpsd' counts JSON lines from the gpsd reader; 'nmea' counts NMEA sentences from the serial/file reader.",
		}, []string{"source"}),
		KissIngressFrames: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_kiss_ingress_frames_total",
			Help: "KISS data frames decoded successfully at the server, by interface DB id and mode ('modem' | 'tnc').",
		}, []string{"interface_id", "mode"}),
		KissTncRxDispatched: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_kiss_tnc_rx_dispatched_total",
			Help: "TNC-mode KISS frames successfully injected into the shared RX fanout (survived rate-limit and per-interface queue).",
		}, []string{"interface_id"}),
		KissTncIngressDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_kiss_tnc_ingress_dropped_total",
			Help: "TNC-mode KISS frames dropped at ingress. Reason 'rate_limit' counts token-bucket denials; 'queue_full' counts overflows of the per-interface ingress queue.",
		}, []string{"interface_id", "reason"}),
		KissBroadcastSuppressed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_kiss_broadcast_suppressed_total",
			Help: "KISS broadcast recipients skipped. Reason 'self_loop' counts the originating TNC-mode interface being excluded from its own echo.",
		}, []string{"interface_id", "reason"}),
		RxFanoutDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_rx_fanout_dropped_total",
			Help: "Frames dropped at the shared RX fanout channel. Only the 'kiss_tnc' producer can drop here (non-blocking send); the 'modem' producer is a blocking send and never increments this counter.",
		}, []string{"producer"}),
		TxBackendSubmits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_tx_backend_submits_total",
			Help: "TX frames fanned out by the backend dispatcher, per instance. Outcome is 'ok' (accepted), 'backend_busy' (queue full, back-pressure), 'backend_down' (transport disconnected), or 'err' (opaque transport error).",
		}, []string{"channel", "backend", "instance", "outcome"}),
		TxNoBackend: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_tx_no_backend_total",
			Help: "TX frames dropped by the dispatcher because no backend was registered for the frame's channel.",
		}, []string{"channel"}),
		TxBackendDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "graywolf_tx_backend_duration_seconds",
			Help:    "Per-instance latency of Backend.Submit calls, seconds.",
			Buckets: prometheus.ExponentialBuckets(0.0001, 4, 10), // 100µs..~26s
		}, []string{"channel", "backend"}),
		KissInstanceTxQueueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "graywolf_kiss_instance_tx_queue_depth",
			Help: "Current depth of a KISS-TNC interface's per-instance tx queue (Phase 3 D20). Non-zero indicates backlog.",
		}, []string{"interface_id"}),
		KissClientConnected: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "graywolf_kiss_client_connected",
			Help: "1 when the KISS tcp-client supervisor's dialed connection is live, 0 otherwise.",
		}, []string{"interface_id", "name"}),
		KissClientReconnects: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_kiss_client_reconnects_total",
			Help: "Cumulative successful dials performed by the KISS tcp-client supervisor since process start.",
		}, []string{"interface_id"}),
		KissClientBackoffSeconds: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "graywolf_kiss_client_backoff_seconds",
			Help: "Current backoff delay in seconds for the KISS tcp-client supervisor while waiting to re-dial. Zero when connected or not in backoff.",
		}, []string{"interface_id"}),
		KissClientTxDrops: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "graywolf_kiss_client_tx_drops_total",
			Help: "TX frames dropped at the tcp-client's per-instance queue. Reason 'busy' = queue full; 'down' = supervisor in backoff.",
		}, []string{"interface_id", "reason"}),
		IgateFilterRecompositions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "graywolf_igate_filter_recompositions_total",
			Help: "Reloads where the composed APRS-IS server filter differed from the last-applied value and was pushed to the iGate client. Reloads that only changed RF rules or the governor without altering the filter do not increment.",
		}),
		lastDcdTransitions: make(map[uint32]uint64),
	}
	reg.MustRegister(
		m.RxFrames,
		m.DcdTransitions,
		m.DcdDropped,
		m.ChildRestarts,
		m.AudioLevel,
		m.DcdActive,
		m.ChildUp,
		m.KissClientsActive,
		m.KissDecodeErrors,
		m.AgwClientsActive,
		m.AgwDecodeErrors,
		m.TxFrames,
		m.TxRateLimited,
		m.TxDeduped,
		m.TxQueueDropped,
		m.AprsOutDropped,
		m.DigipeaterPackets,
		m.DigipeaterDeduped,
		m.PacketlogEntries,
		m.BeaconPackets,
		m.BeaconFired,
		m.BeaconEncodeErrors,
		m.BeaconSubmitErrors,
		m.SmartBeaconRate,
		m.GpsParseErrors,
		m.KissIngressFrames,
		m.KissTncRxDispatched,
		m.KissTncIngressDropped,
		m.KissBroadcastSuppressed,
		m.RxFanoutDropped,
		m.TxBackendSubmits,
		m.TxNoBackend,
		m.TxBackendDuration,
		m.KissInstanceTxQueueDepth,
		m.KissClientConnected,
		m.KissClientReconnects,
		m.KissClientBackoffSeconds,
		m.KissClientTxDrops,
		m.IgateFilterRecompositions,
	)
	return m
}

// ObserveTxFrame increments the tx counter for a channel.
func (m *Metrics) ObserveTxFrame(channel uint32) {
	m.TxFrames.WithLabelValues(strconv.FormatUint(uint64(channel), 10)).Inc()
}

// SetKissClients sets the gauge for a KISS interface name.
func (m *Metrics) SetKissClients(iface string, n int) {
	m.KissClientsActive.WithLabelValues(iface).Set(float64(n))
}

// SetAgwClients sets the AGW client gauge.
func (m *Metrics) SetAgwClients(n int) {
	m.AgwClientsActive.Set(float64(n))
}

// Handler returns an http.Handler serving /metrics from this registry.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{Registry: m.Registry})
}

// UpdateFromStatus folds a Rust-side StatusUpdate into the metric vectors.
// Counter deltas are computed against the previous update; if the modem
// restarts (counters go backwards) the gap is ignored to avoid negative
// deltas.
func (m *Metrics) UpdateFromStatus(s *pb.StatusUpdate) {
	if s == nil {
		return
	}
	label := strconv.FormatUint(uint64(s.Channel), 10)

	if prev, ok := m.lastDcdTransitions[s.Channel]; !ok || s.DcdTransitions < prev {
		m.lastDcdTransitions[s.Channel] = s.DcdTransitions
	} else if s.DcdTransitions > prev {
		m.DcdTransitions.WithLabelValues(label).Add(float64(s.DcdTransitions - prev))
		m.lastDcdTransitions[s.Channel] = s.DcdTransitions
	}

	m.AudioLevel.WithLabelValues(label).Set(float64(s.AudioLevelPeak))
	if s.DcdState {
		m.DcdActive.WithLabelValues(label).Set(1)
	} else {
		m.DcdActive.WithLabelValues(label).Set(0)
	}
}

// ObserveReceivedFrame bumps the rx-frames counter for a channel. Called
// from the modembridge frame forwarder so individual frame arrivals are
// reflected immediately without waiting for the next StatusUpdate.
func (m *Metrics) ObserveReceivedFrame(channel uint32) {
	m.RxFrames.WithLabelValues(strconv.FormatUint(uint64(channel), 10)).Inc()
}

// SetChildUp records whether the Rust child is running.
func (m *Metrics) SetChildUp(up bool) {
	if up {
		m.ChildUp.Set(1)
	} else {
		m.ChildUp.Set(0)
	}
}

// ObserveKissIngressFrame increments the per-interface/per-mode ingress
// frame counter. Called for every inbound KISS data frame that
// successfully AX.25-decodes, regardless of whether the interface is in
// Modem or TNC mode.
func (m *Metrics) ObserveKissIngressFrame(ifaceID uint32, mode string) {
	m.KissIngressFrames.WithLabelValues(strconv.FormatUint(uint64(ifaceID), 10), mode).Inc()
}

// ObserveKissTncRxDispatched increments when a TNC-mode frame survives
// rate-limit + queue and is enqueued onto the shared RX fanout.
func (m *Metrics) ObserveKissTncRxDispatched(ifaceID uint32) {
	m.KissTncRxDispatched.WithLabelValues(strconv.FormatUint(uint64(ifaceID), 10)).Inc()
}

// ObserveKissBroadcastSuppressed increments when a KISS broadcast skips
// the originating TNC interface (self-loop guard).
func (m *Metrics) ObserveKissBroadcastSuppressed(ifaceID uint32) {
	m.KissBroadcastSuppressed.WithLabelValues(strconv.FormatUint(uint64(ifaceID), 10), "self_loop").Inc()
}

// ObserveTxBackendSubmit records one per-instance fan-out outcome from
// the Phase 3 TX dispatcher. d is the Submit duration used by the
// companion histogram.
func (m *Metrics) ObserveTxBackendSubmit(channel uint32, backend, instance, outcome string, d time.Duration) {
	chLbl := strconv.FormatUint(uint64(channel), 10)
	m.TxBackendSubmits.WithLabelValues(chLbl, backend, instance, outcome).Inc()
	m.TxBackendDuration.WithLabelValues(chLbl, backend).Observe(d.Seconds())
}

// ObserveTxNoBackend increments when the dispatcher drops a frame
// because the channel has no registered backend.
func (m *Metrics) ObserveTxNoBackend(channel uint32) {
	m.TxNoBackend.WithLabelValues(strconv.FormatUint(uint64(channel), 10)).Inc()
}

// SetKissInstanceTxQueueDepth sets the per-interface tx queue depth
// gauge. Wired in from kiss.Manager via OnTxQueueDepth.
func (m *Metrics) SetKissInstanceTxQueueDepth(ifaceID uint32, depth int32) {
	m.KissInstanceTxQueueDepth.
		WithLabelValues(strconv.FormatUint(uint64(ifaceID), 10)).
		Set(float64(depth))
}

// SetKissClientConnected sets the per-interface tcp-client connected
// gauge. 1 means "live dialed connection"; 0 means "not connected"
// (includes backoff, connecting, disconnected, stopped).
func (m *Metrics) SetKissClientConnected(ifaceID uint32, name string, connected bool) {
	v := 0.0
	if connected {
		v = 1
	}
	m.KissClientConnected.WithLabelValues(strconv.FormatUint(uint64(ifaceID), 10), name).Set(v)
}

// ObserveKissClientReconnect increments the per-interface tcp-client
// reconnect counter. Called once per successful dial by the wiring
// layer's OnReload handler (which observes supervisor state transitions).
func (m *Metrics) ObserveKissClientReconnect(ifaceID uint32) {
	m.KissClientReconnects.WithLabelValues(strconv.FormatUint(uint64(ifaceID), 10)).Inc()
}

// SetKissClientBackoffSeconds sets the current backoff delay gauge.
// Consumed by the UI via /api/kiss, but also surfaced to Prometheus
// so alerting can catch stuck-in-backoff supervisors independently.
func (m *Metrics) SetKissClientBackoffSeconds(ifaceID uint32, secs uint32) {
	m.KissClientBackoffSeconds.
		WithLabelValues(strconv.FormatUint(uint64(ifaceID), 10)).
		Set(float64(secs))
}

// ObserveKissClientTxDrop increments the per-interface tx-drop
// counter. reason is "busy" (queue full) or "down" (supervisor in
// backoff / not connected). Wired via kiss.Manager.OnTxQueueDrop —
// tcp-client instances share the same queue plumbing as server-listen
// instances, so this counter is a strict superset of the Phase 3
// queue's drop events.
func (m *Metrics) ObserveKissClientTxDrop(ifaceID uint32, reason string) {
	m.KissClientTxDrops.
		WithLabelValues(strconv.FormatUint(uint64(ifaceID), 10), reason).
		Inc()
}
