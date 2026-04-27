package dto

import (
	"fmt"
	"net"
	"strconv"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// Upper bounds on the TNC ingress rate fields. Values above these are
// almost certainly a typo or a unit confusion — APRS traffic realistic
// for one interface is well under 50 frames/sec. The bounds are wide
// enough that a legitimately busy deployment won't bump into them and
// tight enough that "100000000" from a UI typo fails loud at the API
// boundary rather than silently being stored.
const (
	maxTncIngressRateHz = 10000
	maxTncIngressBurst  = 100000
)

// KissRequest is the body accepted by POST /api/kiss and
// PUT /api/kiss/{id}. The frontend uses tcp_port (int) rather than
// listen_addr (host:port string); the store converts between them.
//
// Mode defaults to "modem" when the client omits the field. The two
// TncIngress* fields default to the KissInterface struct tags (50/100)
// via the store-layer normalizer when sent as zero; the handler still
// rejects obviously-wrong non-zero values up front so the error lands
// at the API boundary instead of the SQLite boundary.
type KissRequest struct {
	Type             string `json:"type"`
	TcpPort          int    `json:"tcp_port"`
	SerialDevice     string `json:"serial_device"`
	BaudRate         uint32 `json:"baud_rate"`
	Channel          uint32 `json:"channel"`
	Mode             string `json:"mode"`
	TncIngressRateHz uint32 `json:"tnc_ingress_rate_hz"`
	TncIngressBurst  uint32 `json:"tnc_ingress_burst"`
	// AllowTxFromGovernor opts this TNC-mode interface in to receive
	// frames from the TX governor (beacon / digipeater / iGate /
	// KISS / AGW submissions). Only meaningful when Mode == "tnc";
	// the validator rejects true with any other mode. Default false
	// on migrated rows so existing TNC servers do not silently start
	// transmitting; Phase 4 sets the DTO default to true for newly
	// created tcp-client rows.
	AllowTxFromGovernor bool `json:"allow_tx_from_governor"`
	// Tcp-client fields (Phase 4): RemoteHost:RemotePort is the dial
	// target; ReconnectInitMs / ReconnectMaxMs size the supervisor's
	// exponential-backoff reconnect schedule. Unused / zero for
	// Type != "tcp-client".
	RemoteHost      string `json:"remote_host"`
	RemotePort      uint16 `json:"remote_port"`
	ReconnectInitMs uint32 `json:"reconnect_init_ms"`
	ReconnectMaxMs  uint32 `json:"reconnect_max_ms"`
}

// tcp-client reconnect bounds. init ≥ 250ms so a flapping peer can't
// storm us with reconnect attempts; max ≤ 1h so the UI's countdown
// text never implies something will resume "this millennium". init ≤
// max is enforced separately so a fat-fingered swap lands as a clear
// error, not an exponential blow-up.
const (
	minReconnectInitMs uint32 = 250
	maxReconnectMaxMs  uint32 = 3600000 // 1 hour
)

func (r KissRequest) Validate() error {
	if !configstore.ValidKissInterfaceType(r.Type) {
		return fmt.Errorf("type must be tcp, tcp-client, serial, or bluetooth")
	}
	if r.Type == configstore.KissTypeTCP && r.TcpPort <= 0 {
		return fmt.Errorf("tcp_port is required for tcp interfaces")
	}
	if r.Type == configstore.KissTypeTCPClient {
		if r.RemoteHost == "" {
			return fmt.Errorf("remote_host is required for tcp-client interfaces")
		}
		if r.RemotePort == 0 {
			return fmt.Errorf("remote_port is required for tcp-client interfaces")
		}
		// init bound: anything below 250ms is reconnect-storm territory
		// and almost certainly a typo (someone meant seconds).
		if r.ReconnectInitMs != 0 && r.ReconnectInitMs < minReconnectInitMs {
			return fmt.Errorf("reconnect_init_ms %d below minimum %d", r.ReconnectInitMs, minReconnectInitMs)
		}
		if r.ReconnectMaxMs != 0 && r.ReconnectMaxMs > maxReconnectMaxMs {
			return fmt.Errorf("reconnect_max_ms %d above maximum %d", r.ReconnectMaxMs, maxReconnectMaxMs)
		}
		if r.ReconnectInitMs != 0 && r.ReconnectMaxMs != 0 && r.ReconnectInitMs > r.ReconnectMaxMs {
			return fmt.Errorf("reconnect_init_ms %d must be <= reconnect_max_ms %d", r.ReconnectInitMs, r.ReconnectMaxMs)
		}
	}
	if (r.Type == configstore.KissTypeSerial || r.Type == configstore.KissTypeBluetooth) && r.SerialDevice == "" {
		return fmt.Errorf("serial_device is required for serial/bluetooth interfaces")
	}
	if r.Mode != "" && !configstore.ValidKissMode(r.Mode) {
		return fmt.Errorf("invalid mode %q: must be %q or %q", r.Mode, configstore.KissModeModem, configstore.KissModeTnc)
	}
	if r.TncIngressRateHz > maxTncIngressRateHz {
		return fmt.Errorf("tnc_ingress_rate_hz %d exceeds maximum %d", r.TncIngressRateHz, maxTncIngressRateHz)
	}
	if r.TncIngressBurst > maxTncIngressBurst {
		return fmt.Errorf("tnc_ingress_burst %d exceeds maximum %d", r.TncIngressBurst, maxTncIngressBurst)
	}
	// AllowTxFromGovernor is a TNC-only flag: modem-mode interfaces
	// TX via Submit (they never receive governor-originated frames),
	// so setting the flag with Mode=modem is meaningless and almost
	// certainly a UI bug. Reject at the API boundary so the error
	// lands with useful context rather than silently persisting.
	if r.AllowTxFromGovernor && r.Mode != configstore.KissModeTnc {
		return fmt.Errorf("allow_tx_from_governor requires mode=%q (got %q)",
			configstore.KissModeTnc, r.Mode)
	}
	return nil
}

func (r KissRequest) ToModel() configstore.KissInterface {
	ch := r.Channel
	if ch == 0 {
		ch = 1
	}
	mode := r.Mode
	if mode == "" {
		mode = configstore.KissModeModem
	}
	// Apply reconnect defaults when caller sent zero — the DB column
	// defaults cover legacy rows, but a freshly-built DTO with a
	// zero-value RemoteHost+Port might still end up with zero bounds
	// (the validator already rejected that combination if Type is
	// tcp-client and the client sent explicit zeros; these defaults
	// only land on rows the caller left alone).
	initMs := r.ReconnectInitMs
	if initMs == 0 {
		initMs = 1000
	}
	maxMs := r.ReconnectMaxMs
	if maxMs == 0 {
		maxMs = 300000
	}
	ki := configstore.KissInterface{
		InterfaceType:       r.Type,
		Device:              r.SerialDevice,
		BaudRate:            r.BaudRate,
		Channel:             ch,
		Enabled:             true,
		Broadcast:           true,
		Mode:                mode,
		TncIngressRateHz:    r.TncIngressRateHz,
		TncIngressBurst:     r.TncIngressBurst,
		AllowTxFromGovernor: r.AllowTxFromGovernor,
		RemoteHost:          r.RemoteHost,
		RemotePort:          r.RemotePort,
		ReconnectInitMs:     initMs,
		ReconnectMaxMs:      maxMs,
	}
	switch r.Type {
	case configstore.KissTypeTCP:
		if r.TcpPort > 0 {
			ki.ListenAddr = fmt.Sprintf("0.0.0.0:%d", r.TcpPort)
			ki.Name = fmt.Sprintf("kiss-tcp-%d", r.TcpPort)
		}
	case configstore.KissTypeTCPClient:
		// Name encodes the dial target so a Grafana alert on a
		// specific interface is human-readable without cross-referencing
		// the DB row ID. Truncation not needed — hostnames are bounded.
		ki.Name = fmt.Sprintf("kiss-tcp-client-%s-%d", r.RemoteHost, r.RemotePort)
	default:
		if r.SerialDevice != "" {
			ki.Name = fmt.Sprintf("kiss-serial-%s", r.SerialDevice)
		}
	}
	return ki
}

func (r KissRequest) ToUpdate(id uint32) configstore.KissInterface {
	m := r.ToModel()
	m.ID = id
	return m
}

// KissResponse is the body returned by GET/POST/PUT for a KISS
// interface. Keeps the current shape exactly: tcp_port is derived from
// listen_addr, and bogus/unparseable ports surface as 0.
//
// AllowTxFromGovernor round-trips KissInterface.AllowTxFromGovernor —
// the Phase 3 opt-in flag that gates per-interface governor TX. The
// field is always present but meaningful only when Mode == "tnc".
// NeedsReconfig mirrors KissInterface.NeedsReconfig so the Kiss page
// can surface a "reconfigure me" banner on rows whose Channel was
// nulled by a Phase 5 cascade delete.
type KissResponse struct {
	ID                  uint32 `json:"id"`
	Type                string `json:"type"`
	TcpPort             int    `json:"tcp_port"`
	SerialDevice        string `json:"serial_device"`
	BaudRate            uint32 `json:"baud_rate"`
	Channel             uint32 `json:"channel"`
	Mode                string `json:"mode"`
	TncIngressRateHz    uint32 `json:"tnc_ingress_rate_hz"`
	TncIngressBurst     uint32 `json:"tnc_ingress_burst"`
	AllowTxFromGovernor bool   `json:"allow_tx_from_governor"`
	NeedsReconfig       bool   `json:"needs_reconfig"`
	// Tcp-client fields (Phase 4). Zero-valued for non-tcp-client rows.
	RemoteHost      string `json:"remote_host"`
	RemotePort      uint16 `json:"remote_port"`
	ReconnectInitMs uint32 `json:"reconnect_init_ms"`
	ReconnectMaxMs  uint32 `json:"reconnect_max_ms"`
	// Per-interface runtime status (Phase 4). Surfaced verbatim from
	// kiss.Manager.Status(); zero-valued when the row is not running
	// or when the manager has nothing to report. Omitted from the
	// wire when the interface is not tcp-client (State == "" +
	// omitempty).
	State          string `json:"state,omitempty"`
	LastError      string `json:"last_error,omitempty"`
	RetryAtUnixMs  int64  `json:"retry_at_unix_ms,omitempty"`
	PeerAddr       string `json:"peer_addr,omitempty"`
	ConnectedSince int64  `json:"connected_since,omitempty"`
	ReconnectCount uint64 `json:"reconnect_count,omitempty"`
	BackoffSeconds uint32 `json:"backoff_seconds,omitempty"`
}

func KissFromModel(m configstore.KissInterface) KissResponse {
	out := KissResponse{
		ID:                  m.ID,
		Type:                m.InterfaceType,
		SerialDevice:        m.Device,
		BaudRate:            m.BaudRate,
		Channel:             m.Channel,
		Mode:                m.Mode,
		TncIngressRateHz:    m.TncIngressRateHz,
		TncIngressBurst:     m.TncIngressBurst,
		AllowTxFromGovernor: m.AllowTxFromGovernor,
		NeedsReconfig:       m.NeedsReconfig,
		RemoteHost:          m.RemoteHost,
		RemotePort:          m.RemotePort,
		ReconnectInitMs:     m.ReconnectInitMs,
		ReconnectMaxMs:      m.ReconnectMaxMs,
	}
	if m.ListenAddr != "" {
		if _, portStr, err := net.SplitHostPort(m.ListenAddr); err == nil {
			if p, err := strconv.Atoi(portStr); err == nil {
				out.TcpPort = p
			}
		}
	}
	return out
}

func KissesFromModels(ms []configstore.KissInterface) []KissResponse {
	out := make([]KissResponse, len(ms))
	for i, m := range ms {
		out[i] = KissFromModel(m)
	}
	return out
}
