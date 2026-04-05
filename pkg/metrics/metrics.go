// Package metrics exposes graywolf's Prometheus metrics and a helper to
// fold Rust-side StatusUpdate messages into them.
package metrics

import (
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// Metrics owns a Prometheus registry and the graywolf metric vectors.
type Metrics struct {
	Registry *prometheus.Registry

	RxFrames        *prometheus.CounterVec
	DcdTransitions  *prometheus.CounterVec
	IpcReconnects   prometheus.Counter
	ChildRestarts   prometheus.Counter
	AudioLevel      *prometheus.GaugeVec
	DcdActive       *prometheus.GaugeVec
	ChildUp         prometheus.Gauge

	// Phase 2: protocol + tx governor metrics.
	KissClientsActive *prometheus.GaugeVec // per interface name
	AgwClientsActive  prometheus.Gauge
	TxFrames          *prometheus.CounterVec // per channel
	TxRateLimited     prometheus.Counter
	TxDeduped         prometheus.Counter
	TxQueueDropped    prometheus.Counter
	AprsOutDropped    prometheus.Counter

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
		IpcReconnects: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "graywolf_ipc_reconnects_total",
			Help: "Number of times the Go side reconnected to the modem IPC socket.",
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
		AgwClientsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "graywolf_agw_clients_active",
			Help: "Connected AGWPE clients.",
		}),
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
		lastDcdTransitions: make(map[uint32]uint64),
	}
	reg.MustRegister(
		m.RxFrames,
		m.DcdTransitions,
		m.IpcReconnects,
		m.ChildRestarts,
		m.AudioLevel,
		m.DcdActive,
		m.ChildUp,
		m.KissClientsActive,
		m.AgwClientsActive,
		m.TxFrames,
		m.TxRateLimited,
		m.TxDeduped,
		m.TxQueueDropped,
		m.AprsOutDropped,
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
