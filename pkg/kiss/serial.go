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
	"context"
	"io"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/internal/backoff"
	"go.bug.st/serial"
)

// OpenFunc is the type of SerialConfig.OpenFunc. Exported so build-tagged
// factories in other packages can return it without duplicating the
// signature.
type OpenFunc = func(device string, baud uint32) (io.ReadWriteCloser, error)

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
// settings: 8 data bits, no parity, 1 stop bit, no flow control.
// serial.Open returns serial.Port, an interface that satisfies io.ReadWriteCloser.
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

// SerialSupervisor owns a finalized *Server and runs an
// open→serve→backoff→retry loop over a serial port. Lifecycle (stop)
// is owned by Manager, identical to Client: Manager cancels the ctx
// and calls close(). There is intentionally no public Stop/Enqueue/
// Submit — that surface does not exist on the existing supervisors.
type SerialSupervisor struct {
	cfg  SerialConfig
	srv  *Server
	open func(string, uint32) (io.ReadWriteCloser, error)

	mu             sync.Mutex
	state          string
	lastError      string
	retryAtUnixMs  int64
	connectedSince int64
	reconnectCount uint64
	backoffSeconds uint32
	cancel         context.CancelFunc

	wakeBackoff chan struct{}
	done        chan struct{}
	logger      *slog.Logger

	// onReload / onReconnect are set by Manager.StartSerial (mirroring
	// StartClient's cli.onReload / cli.onReconnect chaining at
	// manager.go:406-421) so manager-level metric hooks fire without
	// the supervisor knowing about the Manager.
	onReload    func()
	onReconnect func()
}

// NewSerial builds a supervisor around an already-finalized *Server.
// Manager.StartSerial MUST finalize the ServerConfig (Sink/RxIngress/
// InterfaceID/...) before calling NewServer, or RX silently drops
// (server logs "no RxIngress wired"). The supervisor never sees an
// unfinalized ServerConfig.
func NewSerial(cfg SerialConfig, srv *Server) *SerialSupervisor {
	if cfg.ReconnectInitMs == 0 {
		cfg.ReconnectInitMs = defaultReconnectInitMs
	}
	if cfg.ReconnectMaxMs == 0 {
		cfg.ReconnectMaxMs = defaultReconnectMaxMs
	}
	lg := cfg.Logger
	if lg == nil {
		lg = slog.Default()
	}
	return &SerialSupervisor{
		cfg:         cfg,
		srv:         srv,
		open:        serialOpenOrDefault(cfg),
		state:       StateStopped,
		wakeBackoff: make(chan struct{}, 1),
		done:        make(chan struct{}),
		logger:      lg,
	}
}

// setState mirrors Client.setState: atomic update + onReload fire.
func (s *SerialSupervisor) setState(state, lastErr string, retryAt int64) {
	s.mu.Lock()
	s.state = state
	s.lastError = lastErr
	s.retryAtUnixMs = retryAt
	if state == StateConnecting || state == StateDisconnected || state == StateStopped {
		s.backoffSeconds = 0
	}
	s.mu.Unlock()
	if s.onReload != nil {
		s.onReload()
	}
}

// sleepWithWake mirrors Client.sleepWithWake: returns true if the
// delay elapsed (or was woken by Reconnect), false if ctx cancelled.
func (s *SerialSupervisor) sleepWithWake(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-s.wakeBackoff:
		return true
	case <-t.C:
		return true
	}
}

// Reconnect short-circuits the current backoff wait. Safe to call
// concurrently; coalesces (buffered size-1, non-blocking send).
func (s *SerialSupervisor) Reconnect() {
	select {
	case s.wakeBackoff <- struct{}{}:
	default:
	}
}

// Status returns a snapshot. Same shape Client.Status returns so the
// Manager union and the UI render identically across transports.
func (s *SerialSupervisor) Status() InterfaceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return InterfaceStatus{
		State:          s.state,
		LastError:      s.lastError,
		RetryAtUnixMs:  s.retryAtUnixMs,
		PeerAddr:       s.cfg.Device,
		ConnectedSince: s.connectedSince,
		ReconnectCount: s.reconnectCount,
		BackoffSeconds: s.backoffSeconds,
	}
}

// close cancels the supervisor and blocks until run() exits.
// Idempotent. Lifecycle owner is Manager (mirrors Client.close).
func (s *SerialSupervisor) close() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	<-s.done
}

// run is the supervise loop: open → ServeTransport → classify →
// backoff → retry; ctx cancel → Stopped. Modeled on Client.run.
func (s *SerialSupervisor) run(parent context.Context) {
	defer close(s.done)
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	bo := backoff.New(backoff.Config{
		Initial:    time.Duration(s.cfg.ReconnectInitMs) * time.Millisecond,
		Max:        time.Duration(s.cfg.ReconnectMaxMs) * time.Millisecond,
		JitterFrac: defaultReconnectJitterFrac,
		Rand:       rand.New(rand.NewSource(rand.Int63())),
	})

	for {
		if ctx.Err() != nil {
			s.setState(StateStopped, "", 0)
			return
		}
		s.setState(StateConnecting, "", 0)

		port, err := s.open(s.cfg.Device, s.cfg.BaudRate)
		if err != nil {
			if ctx.Err() != nil {
				s.setState(StateStopped, "", 0)
				return
			}
			delay := bo.Next()
			s.mu.Lock()
			s.backoffSeconds = uint32((delay + time.Second - 1) / time.Second)
			s.mu.Unlock()
			s.setState(StateBackoff, err.Error(), time.Now().Add(delay).UnixMilli())
			s.logger.Warn("kiss serial open failed",
				"device", s.cfg.Device, "err", err,
				"retry_in", delay.Round(time.Millisecond))
			if !s.sleepWithWake(ctx, delay) {
				s.setState(StateStopped, "", 0)
				return
			}
			continue
		}

		bo.Reset()
		s.mu.Lock()
		s.reconnectCount++
		s.connectedSince = time.Now().UnixMilli()
		s.backoffSeconds = 0
		s.mu.Unlock()
		if s.onReconnect != nil {
			s.onReconnect()
		}
		s.setState(StateConnected, "", 0)
		s.logger.Info("kiss serial supervisor: connected",
			"device", s.cfg.Device, "baud", s.cfg.BaudRate)

		serveErr := s.srv.ServeTransport(ctx, port)
		s.mu.Lock()
		s.connectedSince = 0
		s.mu.Unlock()

		if ctx.Err() != nil {
			s.setState(StateStopped, "", 0)
			return
		}

		errStr := "device closed"
		if serveErr == nil {
			// Clean EOF (e.g. USB removal). Surface disconnected then
			// fall into backoff to retry the device.
			s.setState(StateDisconnected, errStr, 0)
			s.logger.Info("kiss serial session ended cleanly", "device", s.cfg.Device)
		} else {
			errStr = serveErr.Error()
			s.logger.Warn("kiss serial session ended", "device", s.cfg.Device, "err", serveErr)
		}
		delay := bo.Next()
		s.mu.Lock()
		s.backoffSeconds = uint32((delay + time.Second - 1) / time.Second)
		s.mu.Unlock()
		s.setState(StateBackoff, errStr, time.Now().Add(delay).UnixMilli())
		if !s.sleepWithWake(ctx, delay) {
			s.setState(StateStopped, "", 0)
			return
		}
	}
}
