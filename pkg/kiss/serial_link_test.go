package kiss

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.bug.st/serial"
)

// scriptedPort is a controllable serial port double.
type scriptedPort struct {
	closed  chan struct{}
	release chan struct{} // unblocks anything stuck on purpose (test cleanup)
	once    sync.Once
	rx      chan []byte

	writeErr   error
	blockWrite bool
	blockClose bool
	// ignoreClose makes Read keep blocking after Close (wedged driver).
	ignoreClose bool
}

func newScriptedPort() *scriptedPort {
	return &scriptedPort{
		closed:  make(chan struct{}),
		release: make(chan struct{}),
		rx:      make(chan []byte, 16),
	}
}

func (p *scriptedPort) Read(b []byte) (int, error) {
	if p.ignoreClose {
		select {
		case d := <-p.rx:
			return copy(b, d), nil
		case <-p.release:
			return 0, io.EOF
		}
	}
	select {
	case d := <-p.rx:
		return copy(b, d), nil
	case <-p.closed:
		return 0, io.EOF
	}
}

func (p *scriptedPort) Write(b []byte) (int, error) {
	if p.blockWrite {
		select {
		case <-p.closed:
			return 0, io.ErrClosedPipe
		case <-p.release:
			return 0, io.ErrClosedPipe
		}
	}
	if p.writeErr != nil {
		return 0, p.writeErr
	}
	return len(b), nil
}

func (p *scriptedPort) Close() error {
	if p.blockClose {
		<-p.release
	}
	p.once.Do(func() { close(p.closed) })
	return nil
}

func (p *scriptedPort) cleanup() {
	select {
	case <-p.release:
	default:
		close(p.release)
	}
	p.once.Do(func() { close(p.closed) })
}

// probePort adds a modem-status probe to scriptedPort.
type probePort struct {
	*scriptedPort
	probe func() error
}

func (p *probePort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	if err := p.probe(); err != nil {
		return nil, err
	}
	return &serial.ModemStatusBits{}, nil
}

// openRecorder hands out ports from a factory and counts opens.
type openRecorder struct {
	mu    sync.Mutex
	n     int
	ports []io.ReadWriteCloser
	make  func(n int) (io.ReadWriteCloser, error)
}

func (o *openRecorder) open(string, uint32) (io.ReadWriteCloser, error) {
	o.mu.Lock()
	o.n++
	n := o.n
	o.mu.Unlock()
	p, err := o.make(n)
	if p != nil {
		o.mu.Lock()
		o.ports = append(o.ports, p)
		o.mu.Unlock()
	}
	return p, err
}

func (o *openRecorder) opens() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.n
}

func (o *openRecorder) waitOpens(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if o.opens() >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("opens = %d, want >= %d", o.opens(), want)
}

func fastSerialSup(t *testing.T, cfg SerialConfig, rec *openRecorder, tweak ...func(*SerialSupervisor)) (*SerialSupervisor, *Server, func()) {
	t.Helper()
	cfg.Name, cfg.Device, cfg.BaudRate = "s", "d", 9600
	cfg.ReconnectInitMs, cfg.ReconnectMaxMs = 10, 20
	cfg.OpenFunc = rec.open
	srv := newTestSerialServer(t, &txSinkRecorder{})
	sup := NewSerial(cfg, srv)
	sup.settle = 5 * time.Millisecond
	sup.closeTimeout = 50 * time.Millisecond
	sup.abandonGrace = 50 * time.Millisecond
	sup.probeEvery = 5 * time.Millisecond
	for _, fn := range tweak {
		fn(sup)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go sup.run(ctx)
	stop := func() {
		cancel()
		sup.close()
		rec.mu.Lock()
		defer rec.mu.Unlock()
		for _, p := range rec.ports {
			if sp, ok := p.(*scriptedPort); ok {
				sp.cleanup()
			}
			if pp, ok := p.(*probePort); ok {
				pp.cleanup()
			}
		}
	}
	return sup, srv, stop
}

func waitLastEnd(t *testing.T, sup *SerialSupervisor, substr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(sup.Status().LastSessionEnd, substr) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("LastSessionEnd = %q, want substring %q", sup.Status().LastSessionEnd, substr)
}

func TestIsBluetoothSerialPath(t *testing.T) {
	yes := []string{
		"/dev/cu.ICOMBTID-5200",
		"/dev/tty.ICOMBTID-5200",
		"/dev/cu.Bluetooth-Incoming-Port",
		"/dev/rfcomm0",
		"/dev/serial/by-id/usb-Bluetooth_Dongle",
	}
	no := []string{
		"/dev/ttyUSB0",
		"/dev/ttyACM0",
		"/dev/cu.usbserial-1410",
		"/dev/cu.usbmodem14201",
		"COM3",
		"",
	}
	for _, d := range yes {
		if !IsBluetoothSerialPath(d) {
			t.Errorf("IsBluetoothSerialPath(%q) = false, want true", d)
		}
	}
	for _, d := range no {
		if IsBluetoothSerialPath(d) {
			t.Errorf("IsBluetoothSerialPath(%q) = true, want false", d)
		}
	}
}

func TestSerialConfig_WatchdogResolution(t *testing.T) {
	cases := []struct {
		name string
		cfg  SerialConfig
		want bool
	}{
		{"auto plain usb", SerialConfig{Device: "/dev/ttyUSB0"}, false},
		{"auto bt path", SerialConfig{Device: "/dev/cu.ICOMBTID-5200"}, true},
		// The native macOS transport reports link loss itself, so auto is off there.
		{"auto bt type", SerialConfig{Device: "AA:BB:CC:DD:EE:FF", Bluetooth: true}, !classicBluetoothNative},
		{"auto bt type, forced on", SerialConfig{Device: "AA:BB:CC:DD:EE:FF", Bluetooth: true, LinkWatchdog: "on"}, true},
		{"explicit auto", SerialConfig{Device: "/dev/ttyUSB0", LinkWatchdog: "auto"}, false},
		{"on overrides", SerialConfig{Device: "/dev/ttyUSB0", LinkWatchdog: "on"}, true},
		{"off overrides", SerialConfig{Device: "/dev/rfcomm0", Bluetooth: true, LinkWatchdog: "off"}, false},
	}
	for _, c := range cases {
		if got := c.cfg.watchdogActive(); got != c.want {
			t.Errorf("%s: watchdogActive = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSerialConfig_IdleTimeoutDefaultsAndClamps(t *testing.T) {
	cases := map[uint32]time.Duration{
		0:       DefaultIdleReconnectSec * time.Second,
		5:       MinIdleReconnectSec * time.Second,
		120:     120 * time.Second,
		9999999: MaxIdleReconnectSec * time.Second,
	}
	for in, want := range cases {
		if got := (SerialConfig{IdleReconnectSec: in}).idleTimeout(); got != want {
			t.Errorf("idleTimeout(%d) = %v, want %v", in, got, want)
		}
	}
}

// A blocked Read that never errors (the stale bluetooth fd) is recycled
// by the idle watchdog.
func TestSerialSupervisor_IdleWatchdogRecyclesSilentLink(t *testing.T) {
	rec := &openRecorder{make: func(int) (io.ReadWriteCloser, error) { return newScriptedPort(), nil }}
	cfg := SerialConfig{LinkWatchdog: LinkWatchdogOn}
	cfg.Name, cfg.Device, cfg.BaudRate = "s", "d", 9600
	cfg.ReconnectInitMs, cfg.ReconnectMaxMs = 10, 20
	cfg.OpenFunc = rec.open
	sup := NewSerial(cfg, newTestSerialServer(t, &txSinkRecorder{}))
	sup.idle = 60 * time.Millisecond
	sup.settle = 5 * time.Millisecond
	sup.closeTimeout = 50 * time.Millisecond
	sup.abandonGrace = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go sup.run(ctx)
	defer func() { cancel(); sup.close() }()
	rec.waitOpens(t, 2)
	waitLastEnd(t, sup, "no data received")
}

// Steady RX keeps the session alive.
func TestSerialSupervisor_IdleWatchdogSparedByTraffic(t *testing.T) {
	p := newScriptedPort()
	rec := &openRecorder{make: func(int) (io.ReadWriteCloser, error) { return p, nil }}
	cfg := SerialConfig{LinkWatchdog: LinkWatchdogOn}
	cfg.Name, cfg.Device, cfg.BaudRate = "s", "d", 9600
	cfg.OpenFunc = rec.open
	sup := NewSerial(cfg, newTestSerialServer(t, &txSinkRecorder{}))
	sup.idle = 80 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go sup.run(ctx)
	defer func() { cancel(); sup.close(); p.cleanup() }()

	waitState(t, sup, StateConnected)
	end := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(end) {
		p.rx <- []byte{0x00}
		time.Sleep(10 * time.Millisecond)
	}
	if n := rec.opens(); n != 1 {
		t.Fatalf("opens = %d, want 1 (traffic should reset the idle timer)", n)
	}
	if st := sup.Status(); st.RxBytes == 0 || st.LastRxUnixMs == 0 {
		t.Fatalf("rx telemetry not recorded: %+v", st)
	}
}

// With the watchdog off a silent link is left alone.
func TestSerialSupervisor_WatchdogOffDoesNotRecycle(t *testing.T) {
	p := newScriptedPort()
	rec := &openRecorder{make: func(int) (io.ReadWriteCloser, error) { return p, nil }}
	cfg := SerialConfig{LinkWatchdog: LinkWatchdogOff, Device: "/dev/rfcomm0"}
	cfg.Name, cfg.BaudRate = "s", 9600
	cfg.OpenFunc = rec.open
	sup := NewSerial(cfg, newTestSerialServer(t, &txSinkRecorder{}))
	sup.idle = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go sup.run(ctx)
	defer func() { cancel(); sup.close(); p.cleanup() }()
	waitState(t, sup, StateConnected)
	time.Sleep(150 * time.Millisecond)
	if n := rec.opens(); n != 1 {
		t.Fatalf("opens = %d, want 1", n)
	}
}

func TestSerialSupervisor_ProbeFailureRecyclesLink(t *testing.T) {
	var dead atomic.Bool
	rec := &openRecorder{make: func(int) (io.ReadWriteCloser, error) {
		return &probePort{scriptedPort: newScriptedPort(), probe: func() error {
			if dead.Load() {
				return errors.New("ENXIO")
			}
			return nil
		}}, nil
	}}
	sup, _, stop := fastSerialSup(t, SerialConfig{LinkWatchdog: LinkWatchdogOn}, rec)
	defer stop()
	waitState(t, sup, StateConnected)
	dead.Store(true)
	waitLastEnd(t, sup, "link probe failed")
	dead.Store(false)
	rec.waitOpens(t, 2)
}

// A port that can never be probed (probe fails from the start) must not
// be declared dead.
func TestSerialSupervisor_UnprobeablePortIsNotDead(t *testing.T) {
	rec := &openRecorder{make: func(int) (io.ReadWriteCloser, error) {
		return &probePort{scriptedPort: newScriptedPort(), probe: func() error { return errors.New("ENOTTY") }}, nil
	}}
	sup, _, stop := fastSerialSup(t, SerialConfig{LinkWatchdog: LinkWatchdogOn}, rec)
	defer stop()
	waitState(t, sup, StateConnected)
	time.Sleep(100 * time.Millisecond)
	if n := rec.opens(); n != 1 {
		t.Fatalf("opens = %d, want 1", n)
	}
}

// A failing write ends the session (no watchdog needed) and the TX gate
// reports the link down.
func TestSerialSupervisor_WriteErrorReconnects(t *testing.T) {
	rec := &openRecorder{make: func(n int) (io.ReadWriteCloser, error) {
		p := newScriptedPort()
		if n == 1 {
			p.writeErr = errors.New("EIO")
		}
		return p, nil
	}}
	sup, srv, stop := fastSerialSup(t, SerialConfig{LinkWatchdog: LinkWatchdogOff}, rec)
	defer stop()
	waitState(t, sup, StateConnected)

	srv.TxBroadcast(1, []byte{0x01, 0x02}, time.Second)
	waitLastEnd(t, sup, "write error")
	rec.waitOpens(t, 2)
	waitState(t, sup, StateConnected)
	if st := sup.Status(); st.TxWriteErrors != 1 {
		t.Fatalf("TxWriteErrors = %d, want 1", st.TxWriteErrors)
	}
}

func TestSerialSupervisor_WriteTimeoutReconnects(t *testing.T) {
	rec := &openRecorder{make: func(n int) (io.ReadWriteCloser, error) {
		p := newScriptedPort()
		p.blockWrite = n == 1
		return p, nil
	}}
	sup, srv, stop := fastSerialSup(t, SerialConfig{LinkWatchdog: LinkWatchdogOff}, rec,
		func(s *SerialSupervisor) { s.writeTimeout = 30 * time.Millisecond })
	defer stop()
	waitState(t, sup, StateConnected)

	go srv.TxBroadcast(1, []byte{0x01}, time.Second)
	waitLastEnd(t, sup, "write timed out")
	rec.waitOpens(t, 2)
}

// Reconnect() while connected must close and reopen the port.
func TestSerialSupervisor_ReconnectWhileConnectedReopens(t *testing.T) {
	rec := &openRecorder{make: func(int) (io.ReadWriteCloser, error) { return newScriptedPort(), nil }}
	sup, _, stop := fastSerialSup(t, SerialConfig{LinkWatchdog: LinkWatchdogOff}, rec)
	defer stop()
	waitState(t, sup, StateConnected)

	sup.Reconnect()
	waitLastEnd(t, sup, "manual reconnect")
	rec.waitOpens(t, 2)
	waitState(t, sup, StateConnected)
}

// A wedged driver (Close blocks, Read never unblocks) cannot stall the
// supervisor.
func TestSerialSupervisor_HungCloseDoesNotWedge(t *testing.T) {
	rec := &openRecorder{make: func(n int) (io.ReadWriteCloser, error) {
		p := newScriptedPort()
		if n == 1 {
			p.blockClose = true
			p.ignoreClose = true
		}
		return p, nil
	}}
	sup, _, stop := fastSerialSup(t, SerialConfig{LinkWatchdog: LinkWatchdogOff}, rec)
	defer stop()
	waitState(t, sup, StateConnected)

	sup.Reconnect()
	rec.waitOpens(t, 2)
	waitState(t, sup, StateConnected)
}

// An open() that never returns times out under the watchdog and Stop
// stays prompt.
func TestSerialSupervisor_OpenTimeoutUnderWatchdog(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	rec := &openRecorder{make: func(int) (io.ReadWriteCloser, error) {
		<-release
		return nil, errors.New("late")
	}}
	cfg := SerialConfig{LinkWatchdog: LinkWatchdogOn}
	cfg.Name, cfg.Device, cfg.BaudRate = "s", "d", 9600
	cfg.ReconnectInitMs, cfg.ReconnectMaxMs = 10, 20
	cfg.OpenFunc = rec.open
	sup := NewSerial(cfg, newTestSerialServer(t, &txSinkRecorder{}))
	sup.openTimeout = 30 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go sup.run(ctx)
	waitState(t, sup, StateBackoff)
	if !strings.Contains(sup.Status().LastError, "timed out") {
		t.Fatalf("LastError = %q, want open timeout", sup.Status().LastError)
	}
	cancel()
	sup.close()
}

func TestSerialSupervisor_StopIsPromptWhileOpenHangs(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	rec := &openRecorder{make: func(int) (io.ReadWriteCloser, error) {
		<-release
		return nil, errors.New("late")
	}}
	cfg := SerialConfig{LinkWatchdog: LinkWatchdogOff}
	cfg.Name, cfg.Device, cfg.BaudRate = "s", "d", 9600
	cfg.OpenFunc = rec.open
	sup := NewSerial(cfg, newTestSerialServer(t, &txSinkRecorder{}))
	ctx, cancel := context.WithCancel(context.Background())
	go sup.run(ctx)
	rec.waitOpens(t, 1)
	cancel()
	done := make(chan struct{})
	go func() { sup.close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("close() blocked on a hung open()")
	}
}

// The TX queue refuses frames while no session is live and accepts them
// once connected.
func TestSerialSupervisor_TxQueueGatedByLink(t *testing.T) {
	rec := &openRecorder{make: func(n int) (io.ReadWriteCloser, error) {
		if n > 1 {
			return nil, errors.New("radio off")
		}
		return newScriptedPort(), nil
	}}
	cfg := SerialConfig{LinkWatchdog: LinkWatchdogOff}
	cfg.Name, cfg.Device, cfg.BaudRate = "s", "d", 9600
	cfg.ReconnectInitMs, cfg.ReconnectMaxMs = 10, 20
	cfg.OpenFunc = rec.open
	srv := newTestSerialServer(t, &txSinkRecorder{})
	sup := NewSerial(cfg, srv)
	ctx, cancel := context.WithCancel(context.Background())
	q := newInstanceTxQueue(ctx, func([]byte) {})
	defer q.Close()
	sup.attachTxQueue(q)

	if err := q.Enqueue([]byte{1}, 1); !errors.Is(err, ErrBackendDown) {
		t.Fatalf("before connect: err = %v, want ErrBackendDown", err)
	}
	go sup.run(ctx)
	defer func() {
		cancel()
		sup.close()
		rec.mu.Lock()
		defer rec.mu.Unlock()
		for _, p := range rec.ports {
			p.(*scriptedPort).cleanup()
		}
	}()
	waitState(t, sup, StateConnected)
	if err := q.Enqueue([]byte{1}, 2); err != nil {
		t.Fatalf("connected: err = %v, want nil", err)
	}
	sup.Reconnect()
	waitState(t, sup, StateBackoff)
	if err := q.Enqueue([]byte{1}, 3); !errors.Is(err, ErrBackendDown) {
		t.Fatalf("during backoff: err = %v, want ErrBackendDown", err)
	}
}

// Bluetooth-shaped config round trip: auto watchdog resolves on for a
// bluetooth device path and a normal session still comes up.
func TestSerialSupervisor_BluetoothAutoWatchdogConnects(t *testing.T) {
	p := newScriptedPort()
	rec := &openRecorder{make: func(int) (io.ReadWriteCloser, error) { return p, nil }}
	cfg := SerialConfig{Name: "bt", Device: "/dev/cu.ICOMBTID-5200", BaudRate: 9600, OpenFunc: rec.open}
	sup := NewSerial(cfg, newTestSerialServer(t, &txSinkRecorder{}))
	if !sup.watchdog {
		t.Fatal("watchdog should auto-enable for a bluetooth device path")
	}
	ctx, cancel := context.WithCancel(context.Background())
	go sup.run(ctx)
	defer func() { cancel(); sup.close(); p.cleanup() }()
	waitState(t, sup, StateConnected)
}

// The watchdog's modem-line probe is surfaced in Status for diagnosis.
func TestSerialSupervisor_StatusReportsModemLines(t *testing.T) {
	rec := &openRecorder{make: func(int) (io.ReadWriteCloser, error) {
		return &probePort{scriptedPort: newScriptedPort(), probe: func() error { return nil }}, nil
	}}
	sup, _, stop := fastSerialSup(t, SerialConfig{LinkWatchdog: LinkWatchdogOn}, rec)
	defer stop()
	waitState(t, sup, StateConnected)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := sup.Status().LinkLines; got != "" {
			if got != "DCD=0 DSR=0 CTS=0 RI=0" {
				t.Fatalf("LinkLines = %q", got)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("LinkLines never populated")
}
