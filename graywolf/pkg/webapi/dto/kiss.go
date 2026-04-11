package dto

import (
	"fmt"
	"net"
	"strconv"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// KissRequest is the body accepted by POST /api/kiss and
// PUT /api/kiss/{id}. The frontend uses tcp_port (int) rather than
// listen_addr (host:port string); the store converts between them.
type KissRequest struct {
	Type         string `json:"type"`
	TcpPort      int    `json:"tcp_port"`
	SerialDevice string `json:"serial_device"`
	BaudRate     uint32 `json:"baud_rate"`
	Channel      uint32 `json:"channel"`
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
	return nil
}

func (r KissRequest) ToModel() configstore.KissInterface {
	ch := r.Channel
	if ch == 0 {
		ch = 1
	}
	ki := configstore.KissInterface{
		InterfaceType: r.Type,
		Device:        r.SerialDevice,
		BaudRate:      r.BaudRate,
		Channel:       ch,
		Enabled:       true,
		Broadcast:     true,
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
	ID           uint32 `json:"id"`
	Type         string `json:"type"`
	TcpPort      int    `json:"tcp_port"`
	SerialDevice string `json:"serial_device"`
	BaudRate     uint32 `json:"baud_rate"`
	Channel      uint32 `json:"channel"`
}

func KissFromModel(m configstore.KissInterface) KissResponse {
	out := KissResponse{
		ID:           m.ID,
		Type:         m.InterfaceType,
		SerialDevice: m.Device,
		BaudRate:     m.BaudRate,
		Channel:      m.Channel,
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
