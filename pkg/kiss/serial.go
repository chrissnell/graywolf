// Package kiss — serial transport supervisor.
//
// Serial is NOT a Client clone. Server.ServeTransport already
// implements the entire KISS data path over an io.ReadWriteCloser
// (RX decode, TX writer, channel routing, ingress limiting). This
// file only adds the open → run → backoff → retry supervision,
// modeled on Client.run / Client.setState / Client.sleepWithWake.
//
// Shaped to absorb bluetooth-RFCOMM later: open an rfcomm device as an
// io.ReadWriteCloser and hand it to the same ServeTransport.
package kiss

import (
	"io"
	"log/slog"

	"go.bug.st/serial"
)

// SerialConfig mirrors the ClientConfig supervisor-knob surface for a
// serial KISS transport. Sink / RxIngress / InterfaceID /
// OnDecodeError / OnFrameIngress / Clock are deliberately NOT here:
// they are Manager-owned and injected into the owned *Server by
// Manager.StartSerial exactly as Manager.Start does at
// manager.go:231-262 (Correction A in the source spec).
type SerialConfig struct {
	Name                string
	Device              string
	BaudRate            uint32
	Mode                Mode
	ChannelMap          map[uint8]uint32
	ReconnectInitMs     uint32
	ReconnectMaxMs      uint32
	Logger              *slog.Logger
	TncIngressRateHz    uint32
	TncIngressBurst     uint32
	AllowTxFromGovernor bool
	// OnReload fires on every state transition so the wiring layer can
	// rebuild the tx backend. Mirrors ClientConfig.OnReload.
	OnReload func()
	// OpenFunc, when non-nil, replaces the go.bug.st/serial open. Tests
	// inject a fake returning an in-memory pipe. Mirrors
	// ClientConfig.DialFunc (client.go:131).
	OpenFunc func(device string, baud uint32) (io.ReadWriteCloser, error)
}

// defaultSerialOpen opens device at baud with KISS-standard line
// settings: 8 data bits, no parity, 1 stop bit, no flow control. The
// returned serial.Port satisfies io.ReadWriteCloser.
func defaultSerialOpen(device string, baud uint32) (io.ReadWriteCloser, error) {
	return serial.Open(device, &serial.Mode{
		BaudRate: int(baud),
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
}

func serialOpenOrDefault(cfg SerialConfig) func(string, uint32) (io.ReadWriteCloser, error) {
	if cfg.OpenFunc != nil {
		return cfg.OpenFunc
	}
	return defaultSerialOpen
}
