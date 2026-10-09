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
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chrissnell/graywolf/pkg/internal/backoff"
	"github.com/chrissnell/graywolf/pkg/internal/serialopen"
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
	// GateTxToIs mirrors ServerConfig.GateTxToIs for the wrapping
	// Server constructed in StartSerial. Only meaningful when Mode ==
	// ModeModem; the field is read unconditionally and the server's
	// dispatch path enforces the mode gate.
	GateTxToIs bool
	// AllowConnectedMode mirrors ServerConfig.AllowConnectedMode for the
	// wrapping Server constructed in StartSerial.
	AllowConnectedMode bool
	// OnReload fires on every state transition so the wiring layer can
	// rebuild the tx backend. Mirrors ClientConfig.OnReload.
	OnReload func()
	// Bluetooth marks the interface as bluetooth-backed (interface type
	// "bluetooth"); it makes LinkWatchdog "auto" resolve to enabled.
	Bluetooth bool
	// LinkWatchdog is "auto" (default), "on" or "off". When active, the
	// supervisor probes the fd, recycles the port after IdleReconnectSec
	// of RX silence, opens with a timeout, and waits a settle delay
	// before reopening. Write errors and manual reconnects always end
	// the session regardless of this setting.
	LinkWatchdog string
	// IdleReconnectSec is the RX-silence threshold for the watchdog.
	// Zero selects DefaultIdleReconnectSec.
	IdleReconnectSec uint32
	// OpenFunc, when non-nil, replaces the go.bug.st/serial open. Tests
	// inject a fake returning an in-memory pipe. Mirrors
	// ClientConfig.DialFunc (client.go:131).
	OpenFunc func(device string, baud uint32) (io.ReadWriteCloser, error)
}

// defaultSerialOpen opens device at baud with KISS-standard line
// settings: 8 data bits, no parity, 1 stop bit, no flow control.
// serial.Open returns serial.Port, an interface that satisfies io.ReadWriteCloser.
func defaultSerialOpen(device string, baud uint32) (io.ReadWriteCloser, error) {
	// A Bluetooth Classic address (macOS) is dialed through IOBluetooth,
	// not a cu.* tty: the tty only works while macOS itself holds the link
	// and goes silently dead after the radio restarts and reconnects.
	if addr, ch, ok := ParseBluetoothAddress(device); ok && classicBluetoothNative {
		return OpenClassicBluetooth(addr, ch)
	}
	// serialopen.Open sets close-on-exec so graywolf-modem (and any other
	// child) cannot inherit the fd and keep the tty open after we close it.
	return serialopen.Open(device, &serial.Mode{
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

	// Session telemetry. cur is the live session (nil between sessions);
	// the *Total / last* fields accumulate across finished sessions.
	cur            *serialSession
	rxBytesTotal   uint64
	lastRxUnixMs   int64
	lastSessionEnd string
	txErrors       atomic.Uint64

	// watchdog is the resolved LinkWatchdog setting. The durations below
	// are unexported so tests can shrink them.
	watchdog     bool
	idle         time.Duration
	probeEvery   time.Duration
	settle       time.Duration
	writeTimeout time.Duration
	closeTimeout time.Duration
	abandonGrace time.Duration
	openTimeout  time.Duration

	// txq is the per-instance TX queue, gated down while no session is
	// live so governor TX gets ErrBackendDown instead of vanishing.
	txq *instanceTxQueue

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
		cfg:          cfg,
		srv:          srv,
		open:         serialOpenOrDefault(cfg),
		state:        StateStopped,
		watchdog:     cfg.watchdogActive(),
		idle:         cfg.idleTimeout(),
		probeEvery:   serialProbeInterval,
		settle:       serialSettle,
		writeTimeout: instanceTxSocketDeadline,
		closeTimeout: serialCloseTimeout,
		abandonGrace: serialAbandonGrace,
		openTimeout:  serialOpenTimeout,
		wakeBackoff:  make(chan struct{}, 1),
		done:         make(chan struct{}),
		logger:       lg,
	}
}

// attachTxQueue registers the per-instance TX queue. It starts gated
// down; run() opens the gate while a session is live.
func (s *SerialSupervisor) attachTxQueue(q *instanceTxQueue) {
	s.txq = q
	q.setDown(true)
}

func (s *SerialSupervisor) setTxDown(down bool) {
	if s.txq != nil {
		s.txq.setDown(down)
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

// Reconnect short-circuits the current backoff wait and, when a session
// is live, ends it so the port is closed and reopened. This is the only
// way out of a link the OS still reports as open but that no longer
// carries data. Safe to call concurrently; coalesces (buffered size-1,
// non-blocking send).
func (s *SerialSupervisor) Reconnect() {
	s.mu.Lock()
	cur := s.cur
	s.mu.Unlock()
	if cur != nil {
		cur.fail(endManual, "manual reconnect")
	}
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
	rx := s.rxBytesTotal
	lastRx := s.lastRxUnixMs
	lines := ""
	if s.cur != nil {
		lines = s.cur.linesString()
		rx += s.cur.rxBytes.Load()
		if s.cur.lastRx.Load() != 0 {
			lastRx = s.cur.lastRxTime().UnixMilli()
		}
	}
	return InterfaceStatus{
		State:          s.state,
		LastError:      s.lastError,
		RetryAtUnixMs:  s.retryAtUnixMs,
		PeerAddr:       s.cfg.Device,
		ConnectedSince: s.connectedSince,
		ReconnectCount: s.reconnectCount,
		BackoffSeconds: s.backoffSeconds,
		RxBytes:        rx,
		LastRxUnixMs:   lastRx,
		TxWriteErrors:  s.txErrors.Load(),
		LastSessionEnd: s.lastSessionEnd,
		LinkLines:      lines,
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

		port, err := s.openPort(ctx)
		if err != nil {
			if ctx.Err() != nil {
				s.setTxDown(true)
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
		sess := newSerialSession(port, s.cfg.Device, s.logger,
			s.writeTimeout, s.closeTimeout, &s.txErrors)
		s.mu.Lock()
		s.cur = sess
		s.reconnectCount++
		s.connectedSince = time.Now().UnixMilli()
		s.backoffSeconds = 0
		s.mu.Unlock()
		if s.onReconnect != nil {
			s.onReconnect()
		}
		s.setTxDown(false)
		s.setState(StateConnected, "", 0)
		s.logger.Info("kiss serial supervisor: connected",
			"device", s.cfg.Device, "baud", s.cfg.BaudRate,
			"link_watchdog", s.watchdog)

		serveErr := s.serve(ctx, sess)

		s.setTxDown(true)
		kind, reason := sess.endInfo()
		s.mu.Lock()
		s.cur = nil
		s.connectedSince = 0
		s.rxBytesTotal += sess.rxBytes.Load()
		if sess.lastRx.Load() != 0 {
			s.lastRxUnixMs = sess.lastRxTime().UnixMilli()
		}
		s.mu.Unlock()

		if ctx.Err() != nil {
			s.setState(StateStopped, "", 0)
			return
		}

		errStr := "device closed"
		switch {
		case reason != "":
			errStr = reason
			s.logger.Warn("kiss serial session ended",
				"device", s.cfg.Device, "reason", reason,
				"rx_bytes", sess.rxBytes.Load(),
				"session", time.Since(sess.start).Round(time.Second))
		case serveErr == nil:
			// Clean EOF (e.g. USB removal). Surface disconnected then
			// fall into backoff to retry the device.
			s.setState(StateDisconnected, errStr, 0)
			s.logger.Info("kiss serial session ended cleanly", "device", s.cfg.Device)
		default:
			errStr = "read error: " + serveErr.Error()
			s.logger.Warn("kiss serial session ended", "device", s.cfg.Device, "err", serveErr)
		}
		s.mu.Lock()
		s.lastSessionEnd = errStr
		s.mu.Unlock()

		if kind == endManual {
			// Reconnect() also queued a wake token; drop it so the
			// settle delay below still applies.
			select {
			case <-s.wakeBackoff:
			default:
			}
		}
		delay := bo.Next()
		if s.watchdog && delay < s.settle {
			delay = s.settle
		}
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

// openPort runs the open callback in its own goroutine so a driver that
// blocks in open() (macOS bluetooth cu.* nodes can) cannot wedge the
// supervisor: ctx cancel and, when the watchdog is active, an open
// timeout both return promptly. A port that opens after we gave up is
// closed.
func (s *SerialSupervisor) openPort(ctx context.Context) (io.ReadWriteCloser, error) {
	type result struct {
		port io.ReadWriteCloser
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		p, err := s.open(s.cfg.Device, s.cfg.BaudRate)
		ch <- result{p, err}
	}()
	reap := func() {
		if r := <-ch; r.port != nil {
			_ = r.port.Close()
		}
	}
	var timeout <-chan time.Time
	if s.watchdog {
		t := time.NewTimer(s.openTimeout)
		defer t.Stop()
		timeout = t.C
	}
	select {
	case r := <-ch:
		return r.port, r.err
	case <-ctx.Done():
		go reap()
		return nil, ctx.Err()
	case <-timeout:
		go reap()
		return nil, fmt.Errorf("open timed out after %s", s.openTimeout)
	}
}

// serve runs ServeTransport over sess until the transport fails, the
// session is failed by a watchdog / write error / manual reconnect, or
// ctx is cancelled. If the transport does not unwind within
// abandonGrace of the port being closed (a wedged driver), the stuck
// goroutine is abandoned so the supervisor can reopen.
func (s *SerialSupervisor) serve(ctx context.Context, sess *serialSession) error {
	serveDone := make(chan error, 1)
	go func() { serveDone <- s.srv.ServeTransport(ctx, sess) }()

	monitorStop := make(chan struct{})
	defer close(monitorStop)
	go s.monitor(sess, monitorStop)

	select {
	case err := <-serveDone:
		return err
	case <-sess.failed:
	case <-ctx.Done():
		go func() { _ = sess.Close() }()
	}
	select {
	case err := <-serveDone:
		return err
	case <-time.After(s.abandonGrace):
		s.logger.Warn("kiss serial transport did not unwind after close; abandoning",
			"device", s.cfg.Device, "grace", s.abandonGrace)
		return nil
	}
}

// monitor is the stale-link watchdog for one session. It never writes to
// the port: it asks the kernel whether the fd is still backed by a live
// device and recycles the port after a long stretch of RX silence.
func (s *SerialSupervisor) monitor(sess *serialSession, stop <-chan struct{}) {
	if !s.watchdog {
		return
	}
	probe, canProbe := sess.prober()
	if canProbe && probe() != nil {
		// Platform cannot report modem status for this device; don't
		// treat that as a dead link.
		canProbe = false
	}
	tick := s.probeEvery
	if s.idle/4 < tick {
		tick = s.idle / 4
	}
	if tick <= 0 {
		tick = time.Millisecond
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	probeFails := 0
	for {
		select {
		case <-stop:
			return
		case <-sess.failed:
			return
		case <-t.C:
		}
		if canProbe {
			if err := probe(); err != nil {
				probeFails++
				if probeFails >= probeFailLimit {
					sess.fail(endProbe, "link probe failed: "+err.Error())
					return
				}
			} else {
				probeFails = 0
			}
		}
		if quiet := time.Since(sess.lastRxTime()); quiet > s.idle {
			sess.fail(endIdle, fmt.Sprintf("no data received for %s", quiet.Round(time.Second)))
			return
		}
	}
}
