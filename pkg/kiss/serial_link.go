package kiss

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.bug.st/serial"
)

// Link watchdog modes for SerialConfig.LinkWatchdog / the
// kiss_interfaces.link_watchdog column.
const (
	// LinkWatchdogAuto enables the stale-link watchdog only for
	// bluetooth interfaces (type "bluetooth" or a device path that looks
	// like a bluetooth serial port).
	LinkWatchdogAuto = "auto"
	LinkWatchdogOn   = "on"
	LinkWatchdogOff  = "off"
)

// Timing defaults for the serial session layer.
const (
	// DefaultIdleReconnectSec is how long a watched serial link may
	// deliver zero bytes before the supervisor recycles the port.
	DefaultIdleReconnectSec = 180
	// MinIdleReconnectSec / MaxIdleReconnectSec bound the operator value.
	MinIdleReconnectSec = 30
	MaxIdleReconnectSec = 86400

	serialProbeInterval = 10 * time.Second
	// serialSettle is the minimum pause before reopening a watched
	// (bluetooth) port so the OS can tear down the old RFCOMM channel.
	serialSettle       = 1500 * time.Millisecond
	serialCloseTimeout = 3 * time.Second
	serialAbandonGrace = 5 * time.Second
	serialOpenTimeout  = 45 * time.Second
	// probeFailLimit is the number of consecutive failed fd probes that
	// declare the link dead.
	probeFailLimit = 2
)

// Session end kinds, used to pick reconnect pacing and status text.
const (
	endNone = iota
	endManual
	endIdle
	endProbe
	endWrite
)

var errCloseTimeout = errors.New("serial close timed out")

// IsBluetoothSerialPath reports whether device looks like a bluetooth
// serial port: a macOS /dev/cu.* or /dev/tty.* node with BT in its name
// (e.g. cu.ICOMBTID-5200, tty.Bluetooth-Incoming-Port) or a Linux
// /dev/rfcomm* node.
func IsBluetoothSerialPath(device string) bool {
	lower := strings.ToLower(device)
	if strings.Contains(lower, "bluetooth") {
		return true
	}
	base := filepath.Base(lower)
	if strings.HasPrefix(base, "rfcomm") {
		return true
	}
	if strings.HasPrefix(base, "cu.") || strings.HasPrefix(base, "tty.") {
		return strings.Contains(base, "bt")
	}
	return false
}

// watchdogActive resolves LinkWatchdog (auto/on/off) for this config.
func (c SerialConfig) watchdogActive() bool {
	switch strings.ToLower(c.LinkWatchdog) {
	case LinkWatchdogOn:
		return true
	case LinkWatchdogOff:
		return false
	default:
		// The native macOS Bluetooth transport reports link loss itself
		// (EOF when the radio drops), so the idle recycle would only
		// interrupt a quiet but healthy link. "on" still forces it.
		if nativeBluetoothDevice(c.Device) {
			return false
		}
		return c.Bluetooth || IsBluetoothSerialPath(c.Device)
	}
}

// idleTimeout resolves IdleReconnectSec, applying the default and clamps.
func (c SerialConfig) idleTimeout() time.Duration {
	sec := c.IdleReconnectSec
	if sec == 0 {
		sec = DefaultIdleReconnectSec
	}
	if sec < MinIdleReconnectSec {
		sec = MinIdleReconnectSec
	}
	if sec > MaxIdleReconnectSec {
		sec = MaxIdleReconnectSec
	}
	return time.Duration(sec) * time.Second
}

// modemProber is satisfied by go.bug.st/serial ports. Reading the modem
// status lines is a non-writing way to ask the kernel whether the fd is
// still backed by a live device; it never sends bytes to the radio.
type modemProber interface {
	GetModemStatusBits() (*serial.ModemStatusBits, error)
}

// serialSession wraps one opened serial port for the lifetime of a
// single ServeTransport run. It adds RX liveness tracking, write-error /
// write-timeout detection, and a bounded Close so a wedged OS driver
// cannot stall the supervisor.
type serialSession struct {
	inner  io.ReadWriteCloser
	logger *slog.Logger
	device string
	start  time.Time

	writeTimeout time.Duration
	closeTimeout time.Duration
	txErrors     *atomic.Uint64

	lastRx  atomic.Int64 // unix nanos of the last non-empty read
	rxBytes atomic.Uint64
	lines   atomic.Value // last modem-line snapshot (string)

	closed    atomic.Bool
	closeOnce sync.Once
	closeErr  error

	failOnce sync.Once
	failed   chan struct{} // closed on the first fail()
	mu       sync.Mutex
	reason   string
	kind     int
}

func newSerialSession(inner io.ReadWriteCloser, device string, logger *slog.Logger,
	writeTimeout, closeTimeout time.Duration, txErrors *atomic.Uint64) *serialSession {
	return &serialSession{
		inner:        inner,
		logger:       logger,
		device:       device,
		start:        time.Now(),
		writeTimeout: writeTimeout,
		closeTimeout: closeTimeout,
		txErrors:     txErrors,
		failed:       make(chan struct{}),
	}
}

// fail records why the session is ending (first caller wins) and closes
// the port asynchronously so a blocked Read/Write returns.
func (p *serialSession) fail(kind int, reason string) {
	p.failOnce.Do(func() {
		p.mu.Lock()
		p.kind = kind
		p.reason = reason
		p.mu.Unlock()
		close(p.failed)
		go func() { _ = p.Close() }()
	})
}

func (p *serialSession) endInfo() (int, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.kind, p.reason
}

// lastRxTime returns the time of the last received byte, or the session
// start when nothing has arrived yet.
func (p *serialSession) lastRxTime() time.Time {
	if ns := p.lastRx.Load(); ns != 0 {
		return time.Unix(0, ns)
	}
	return p.start
}

func (p *serialSession) Read(b []byte) (int, error) {
	n, err := p.inner.Read(b)
	if n > 0 {
		p.rxBytes.Add(uint64(n))
		p.lastRx.Store(time.Now().UnixNano())
	}
	return n, err
}

func (p *serialSession) Write(b []byte) (int, error) {
	if p.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	timer := time.AfterFunc(p.writeTimeout, func() {
		p.fail(endWrite, fmt.Sprintf("write timed out after %s", p.writeTimeout))
	})
	n, err := p.inner.Write(b)
	timer.Stop()
	if err == nil && n < len(b) {
		err = io.ErrShortWrite
	}
	if err != nil && !p.closed.Load() {
		p.txErrors.Add(1)
		p.logger.Warn("kiss serial write failed", "device", p.device, "err", err)
		p.fail(endWrite, "write error: "+err.Error())
	}
	return n, err
}

// Close closes the underlying port, waiting at most closeTimeout. It is
// idempotent and safe to call from several goroutines.
func (p *serialSession) Close() error {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		done := make(chan error, 1)
		go func() { done <- p.inner.Close() }()
		select {
		case p.closeErr = <-done:
		case <-time.After(p.closeTimeout):
			p.closeErr = errCloseTimeout
			p.logger.Warn("kiss serial close timed out; abandoning port",
				"device", p.device, "timeout", p.closeTimeout)
		}
	})
	return p.closeErr
}

// prober returns a non-writing liveness check for the underlying port,
// or false when the port cannot be probed. Each successful probe records
// the modem status lines; on macOS bluetooth ports these show whether the
// OS considers the serial channel connected (DCD/DSR), which the
// "connected" state (port merely open) cannot.
func (p *serialSession) prober() (func() error, bool) {
	mp, ok := p.inner.(modemProber)
	if !ok {
		return nil, false
	}
	return func() error {
		bits, err := mp.GetModemStatusBits()
		if err != nil {
			return err
		}
		p.recordLines(bits)
		return nil
	}, true
}

func (p *serialSession) recordLines(b *serial.ModemStatusBits) {
	bit := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	lines := fmt.Sprintf("DCD=%d DSR=%d CTS=%d RI=%d", bit(b.DCD), bit(b.DSR), bit(b.CTS), bit(b.RI))
	if prev, _ := p.lines.Load().(string); prev == lines {
		return
	}
	p.lines.Store(lines)
	p.logger.Info("kiss serial modem lines", "device", p.device, "lines", lines)
}

// linesString returns the most recent modem line snapshot ("" if never read).
func (p *serialSession) linesString() string {
	s, _ := p.lines.Load().(string)
	return s
}
