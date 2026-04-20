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
}

func (r KissRequest) Validate() error {
	if r.Type != "tcp" && r.Type != "serial" && r.Type != "bluetooth" {
		return fmt.Errorf("type must be tcp, serial, or bluetooth")
	}
	if r.Type == "tcp" && r.TcpPort <= 0 {
		return fmt.Errorf("tcp_port is required for tcp interfaces")
	}
	if (r.Type == "serial" || r.Type == "bluetooth") && r.SerialDevice == "" {
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
	ki := configstore.KissInterface{
		InterfaceType:    r.Type,
		Device:           r.SerialDevice,
		BaudRate:         r.BaudRate,
		Channel:          ch,
		Enabled:          true,
		Broadcast:        true,
		Mode:             mode,
		TncIngressRateHz: r.TncIngressRateHz,
		TncIngressBurst:  r.TncIngressBurst,
	}
	if r.Type == "tcp" && r.TcpPort > 0 {
		ki.ListenAddr = fmt.Sprintf("0.0.0.0:%d", r.TcpPort)
		ki.Name = fmt.Sprintf("kiss-tcp-%d", r.TcpPort)
	} else if r.SerialDevice != "" {
		ki.Name = fmt.Sprintf("kiss-serial-%s", r.SerialDevice)
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
type KissResponse struct {
	ID               uint32 `json:"id"`
	Type             string `json:"type"`
	TcpPort          int    `json:"tcp_port"`
	SerialDevice     string `json:"serial_device"`
	BaudRate         uint32 `json:"baud_rate"`
	Channel          uint32 `json:"channel"`
	Mode             string `json:"mode"`
	TncIngressRateHz uint32 `json:"tnc_ingress_rate_hz"`
	TncIngressBurst  uint32 `json:"tnc_ingress_burst"`
}

func KissFromModel(m configstore.KissInterface) KissResponse {
	out := KissResponse{
		ID:               m.ID,
		Type:             m.InterfaceType,
		SerialDevice:     m.Device,
		BaudRate:         m.BaudRate,
		Channel:          m.Channel,
		Mode:             m.Mode,
		TncIngressRateHz: m.TncIngressRateHz,
		TncIngressBurst:  m.TncIngressBurst,
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
