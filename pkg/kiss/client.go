package kiss

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/internal/backoff"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// Defaults for tcp-client supervisor configuration. Values match the
// Phase 4 plan (D2).
const (
	defaultDialTimeout         = 10 * time.Second
	defaultReconnectJitterFrac = 0.25
	defaultReconnectInitMs     = 1000
	defaultReconnectMaxMs      = 300000
)

// Client is the outbound-dial KISS interface: a single supervised TCP
// connection to a remote KISS TNC, with exponential-backoff reconnect
// and unified Status() telemetry for the UI countdown + Retry-now
// affordance. One Client per KissInterface row with
// InterfaceType="tcp-client".
//
// Design (D2 + D20):
//
//   - Supervisor owns a single net.Conn at a time. On connect, installs
//     a live tx queue used by Manager.TransmitOnChannel; on disconnect,
//     the queue goes back to returning ErrBackendDown.
//   - Supervisor reads KISS frames via a shared frameLoop helper (same
//     decoder the server path uses) and dispatches through the same
//     dispatchDataFrame branch (modem mode submits to governor; TNC
//     mode forwards to RxIngress).
//   - Backoff: pkg/internal/backoff (Initial/Max from config,
//     JitterFrac=0.25). Reset on successful connection. Reconnect()
//     cancels the current backoff wait and dials immediately.
type Client struct {
	cfg    ClientConfig
	logger *slog.Logger

	// lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	// state protected by mu.
	mu             sync.Mutex
	state          string
	lastError      string
	retryAtUnixMs  int64
	peerAddr       string
	connectedSince int64
	reconnectCount uint64
	backoffSeconds uint32
	conn           net.Conn

	// tx queue: per-instance (depth 16), non-blocking enqueue, drained
	// by the writer goroutine. The drainer writes to the currently-live
	// conn (nil when disconnected → Enqueue returns ErrBackendDown via
	// the queue's stopped state or writes are dropped when no conn).
	// Constructed at newClient() time so the manager can publish it
	// synchronously on StartClient — no sleep-loop polling.
	txQueue *instanceTxQueue
	channel uint32

	// rateLimiter gates ModeTnc inbound frames. nil in ModeModem. Same
	// semantics as server.go: zero rate OR zero burst means unlimited
	// (see NewRateLimiter).
	rateLimiter *RateLimiter

	// onTxDrop observes writer-goroutine failures so ops can distinguish
	// socket-write failures from queue-full / backend-down drops. Set by
	// the manager from the same OnClientStateChange/reconnect hook
	// surface the kiss metrics use.
	onTxDrop func(reason string)

	// wakeBackoff fires when Reconnect() is called during the backoff
	// wait — the select inside supervise() observes it and re-dials
	// immediately. Buffered size 1 + non-blocking send coalesces bursts.
	wakeBackoff chan struct{}

	// onReload is invoked on every state transition so the wiring layer
	// can nudge the txbackend dispatcher to rebuild its snapshot (so
	// backend.Enqueue surfaces ErrBackendDown when we're in backoff,
	// ErrBackendBusy otherwise). Optional; nil is a no-op.
	onReload func()
	// onReconnect fires exactly once per successful dial so the
	// wiring layer can increment a Prometheus counter. Nil is a
	// no-op.
	onReconnect func()
}

// ClientConfig configures a tcp-client Client instance.
type ClientConfig struct {
	// InterfaceID is the KissInterface DB row ID. Used to tag ingress
	// frames (TNC mode) with ingress.KissTnc(InterfaceID).
	InterfaceID uint32
	// Name identifies the interface in logs and metrics.
	Name string
	// RemoteHost / RemotePort are the dial target.
	RemoteHost string
	RemotePort uint16
	// ChannelMap mirrors the server side — the per-port → channel
	// translation applied to inbound frames. Outbound TX uses the
	// reverse mapping (first port whose value equals the target
	// channel, default 0).
	ChannelMap map[uint8]uint32
	// ReconnectInitMs / ReconnectMaxMs size the backoff schedule. Both
	// must be > 0; the manager normalizes zero to sensible defaults
	// before calling.
	ReconnectInitMs uint32
	ReconnectMaxMs  uint32
	// Dialer is the net.Dialer used for every dial. nil → default
	// Dialer with a 10s connect timeout. Tests inject a custom dialer
	// to point at net.Pipe endpoints.
	Dialer *net.Dialer
	// DialFunc, when non-nil, replaces the net.Dialer. Used by tests
	// that don't have a real listener (e.g. net.Pipe harnesses).
	DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)
	// Sink receives parsed AX.25 frames for TX (modem mode). Typically
	// *txgovernor.Governor.
	Sink txgovernor.TxSink
	// Logger is optional.
	Logger *slog.Logger
	// Mode selects RX routing — identical semantics to the server
	// path. Modem-mode Clients submit inbound frames to Sink; TNC-mode
	// Clients forward via RxIngress.
	Mode Mode
	// AllowTxFromGovernor mirrors the server-side knob: when true AND
	// Mode==ModeTnc, the manager wires a per-instance tx queue.
	AllowTxFromGovernor bool
	// TncIngressRateHz / TncIngressBurst size the TNC-mode rate
	// limiter on inbound frames. Zero disables rate limiting.
	TncIngressRateHz uint32
	TncIngressBurst  uint32
	// RxIngress is the TNC-mode inbound sink. Mirrors server.go.
	RxIngress func(rf *pb.ReceivedFrame, src ingress.Source)
	// Clock is the rate-limiter's time source. nil → wall time.
	Clock Clock
	// OnFrameIngress is invoked for every successfully-decoded data
	// frame (observation hook).
	OnFrameIngress func(mode Mode)
	// OnDecodeError is invoked per ax.25 decode failure.
	OnDecodeError func()
	// OnReload is invoked on every state transition so the wiring
	// layer can nudge the txbackend dispatcher to rebuild. Optional.
	OnReload func()
}

// newClient constructs a Client from cfg. It does NOT start the
// supervisor; callers start it via run(ctx). The per-instance tx queue
// is constructed here (not inside run) so Manager.StartClient can
// publish it synchronously without polling.
func newClient(cfg ClientConfig) *Client {
	lg := cfg.Logger
	if lg == nil {
		lg = slog.Default()
	}
	// Normalize zero-valued reconnect bounds so backoff.New does not
	// panic when the caller omits them (DTO layer validates ranges for
	// user-facing paths, but tests + in-process smoke callers may
	// leave them unset).
	if cfg.ReconnectInitMs == 0 {
		cfg.ReconnectInitMs = defaultReconnectInitMs
	}
	if cfg.ReconnectMaxMs == 0 {
		cfg.ReconnectMaxMs = defaultReconnectMaxMs
	}
	// Deterministic port selection: map iteration order is randomized
	// in Go. Pick the lowest port whose value maps to any channel, and
	// remember the channel for reuse in writeFrame's reverse lookup.
	ch := uint32(1)
	if len(cfg.ChannelMap) > 0 {
		keys := make([]int, 0, len(cfg.ChannelMap))
		for p := range cfg.ChannelMap {
			keys = append(keys, int(p))
		}
		sort.Ints(keys)
		ch = cfg.ChannelMap[uint8(keys[0])]
	}
	c := &Client{
		cfg:         cfg,
		logger:      lg.With("kiss_iface", cfg.Name, "kiss_type", "tcp-client"),
		state:       StateDisconnected,
		channel:     ch,
		wakeBackoff: make(chan struct{}, 1),
		done:        make(chan struct{}),
		onReload:    cfg.OnReload,
	}
	// TNC-mode ingress rate limiter mirrors server.go's gate. Zero
	// rate or zero burst → unlimited mode inside NewRateLimiter.
	if cfg.Mode == ModeTnc {
		c.rateLimiter = NewRateLimiter(cfg.TncIngressRateHz, cfg.TncIngressBurst, cfg.Clock)
	}
	// Per-instance tx queue: available immediately so the manager can
	// wire metric observers without polling. Drainer goroutine starts
	// on construction; it exits when Close is called (from close()).
	if cfg.AllowTxFromGovernor && cfg.Mode == ModeTnc {
		queueCh := ch
		broadcast := func(axBytes []byte) {
			c.writeFrame(queueCh, axBytes)
		}
		c.txQueue = newInstanceTxQueue(context.Background(), broadcast)
	}
	return c
}

// run blocks until ctx is cancelled or Close() is called. Runs the
// dial → read → backoff loop in the current goroutine. Returns when
// the supervisor has fully exited and the writer goroutine has
// stopped.
func (c *Client) run(parent context.Context) {
	defer close(c.done)
	c.mu.Lock()
	c.ctx, c.cancel = context.WithCancel(parent)
	ctx := c.ctx
	c.mu.Unlock()

	// TX queue was constructed in newClient so the manager sees it
	// immediately (see Client.txQueue godoc). Nothing to do here.

	bo := backoff.New(backoff.Config{
		Initial:    time.Duration(c.cfg.ReconnectInitMs) * time.Millisecond,
		Max:        time.Duration(c.cfg.ReconnectMaxMs) * time.Millisecond,
		JitterFrac: defaultReconnectJitterFrac,
		// Seed a per-client source so two clients constructed in the
		// same process don't share a stream. rand.Int63 uses Go's
		// auto-seeded global source (Go 1.20+).
		Rand: rand.New(rand.NewSource(rand.Int63())),
	})

	for {
		if ctx.Err() != nil {
			c.setState(StateStopped, "", 0, "")
			c.closeTxQueue()
			return
		}
		c.setState(StateConnecting, "", 0, "")

		conn, err := c.dial(ctx)
		if err != nil {
			if ctx.Err() != nil {
				c.setState(StateStopped, "", 0, "")
				c.closeTxQueue()
				return
			}
			delay := bo.Next()
			retryAt := time.Now().Add(delay).UnixMilli()
			c.mu.Lock()
			c.backoffSeconds = uint32((delay + time.Second - 1) / time.Second)
			c.mu.Unlock()
			c.setState(StateBackoff, err.Error(), retryAt, "")
			c.logger.Warn("kiss client dial failed", "err", err, "retry_in", delay.Round(time.Millisecond))
			if !c.sleepWithWake(ctx, delay) {
				c.setState(StateStopped, "", 0, "")
				c.closeTxQueue()
				return
			}
			continue
		}

		// Successful dial: reset backoff, install live conn.
		bo.Reset()
		c.mu.Lock()
		c.conn = conn
		c.reconnectCount++
		c.connectedSince = time.Now().UnixMilli()
		c.peerAddr = conn.RemoteAddr().String()
		c.backoffSeconds = 0
		c.mu.Unlock()
		if c.onReconnect != nil {
			c.onReconnect()
		}
		c.setState(StateConnected, "", 0, conn.RemoteAddr().String())
		c.logger.Info("kiss client connected", "remote", conn.RemoteAddr().String())

		// Read frames until the conn dies or ctx cancels. frameLoop
		// respects ctx by closing conn on cancel (handled by the
		// watcher goroutine it spawns).
		err = c.frameLoop(ctx, conn)
		// Clear live conn before deciding next state.
		c.mu.Lock()
		c.conn = nil
		c.connectedSince = 0
		c.peerAddr = ""
		c.mu.Unlock()
		_ = conn.Close()

		if ctx.Err() != nil {
			c.setState(StateStopped, "", 0, "")
			c.closeTxQueue()
			return
		}
		errStr := ""
		if err != nil {
			errStr = err.Error()
			c.logger.Warn("kiss client session ended", "err", err)
		} else {
			c.logger.Info("kiss client session ended cleanly")
		}
		delay := bo.Next()
		retryAt := time.Now().Add(delay).UnixMilli()
		c.mu.Lock()
		c.backoffSeconds = uint32((delay + time.Second - 1) / time.Second)
		c.mu.Unlock()
		c.setState(StateBackoff, errStr, retryAt, "")
		if !c.sleepWithWake(ctx, delay) {
			c.setState(StateStopped, "", 0, "")
			c.closeTxQueue()
			return
		}
	}
}

// closeTxQueue closes the per-instance tx queue if it exists. Safe to
// call multiple times (the queue's Close is idempotent).
func (c *Client) closeTxQueue() {
	c.mu.Lock()
	q := c.txQueue
	c.mu.Unlock()
	if q != nil {
		q.Close()
	}
}

// dial performs one net.Dial, respecting the configured dialer. Fails
// fast when ctx is already cancelled so we don't attempt a connect
// during shutdown.
func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	addr := net.JoinHostPort(c.cfg.RemoteHost, strconv.FormatUint(uint64(c.cfg.RemotePort), 10))
	if c.cfg.DialFunc != nil {
		return c.cfg.DialFunc(ctx, "tcp", addr)
	}
	dialer := c.cfg.Dialer
	if dialer == nil {
		dialer = &net.Dialer{Timeout: defaultDialTimeout}
	}
	return dialer.DialContext(ctx, "tcp", addr)
}

// frameLoop reads frames from conn and dispatches them via the shared
// handleFrame helper. Closes the conn on ctx cancel via a small
// watcher goroutine; returns when the decoder yields EOF/err or the
// watcher closes the conn.
func (c *Client) frameLoop(ctx context.Context, conn net.Conn) error {
	// ctx-cancel watcher: closes the conn to unblock the decoder's
	// read. done signals orderly shutdown (frameLoop's own return)
	// so the watcher doesn't also close the conn.
	done := make(chan struct{})
	var watcherWG sync.WaitGroup
	watcherWG.Add(1)
	go func() {
		defer watcherWG.Done()
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	// Defers run LIFO: wait-for-watcher registers FIRST, close-done
	// registers SECOND — so on return, close(done) fires first,
	// watcher unblocks on done, then Wait() completes. If we
	// swapped the registration order we'd deadlock (Wait would
	// block forever because done was never closed). Don't change
	// without reverifying with `go test -race`.
	defer watcherWG.Wait()
	defer close(done)

	d := NewDecoder(conn)
	for {
		f, err := d.Next()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		c.handleFrame(ctx, conn.RemoteAddr().String(), f)
	}
}

// handleFrame runs the decoded AX.25 + dispatch logic. Mirrors the
// server's handleFrame; kept separate so the Client doesn't have to
// synthesize a Server just for dispatch.
func (c *Client) handleFrame(ctx context.Context, remote string, f *Frame) {
	switch f.Command {
	case CmdDataFrame:
		ax, err := ax25.Decode(f.Data)
		if err != nil {
			if c.cfg.OnDecodeError != nil {
				c.cfg.OnDecodeError()
			}
			c.logger.Warn("kiss client frame is not valid ax.25",
				"remote", remote, "len", len(f.Data), "err", err)
			return
		}
		if !ax.IsUI() {
			c.logger.Debug("dropping non-UI frame", "remote", remote)
			return
		}
		channel := c.channelFor(f.Port)
		mode := c.cfg.Mode
		if mode == "" {
			mode = ModeModem
		}
		if c.cfg.OnFrameIngress != nil {
			c.cfg.OnFrameIngress(mode)
		}
		c.dispatchDataFrame(ctx, remote, channel, ax, f.Data, mode)
	case CmdTxDelay, CmdPersistence, CmdSlotTime, CmdTxTail, CmdFullDuplex, CmdSetHardware:
		c.logger.Debug("ignoring kiss timing command", "cmd", f.Command, "remote", remote)
	case CmdReturn:
		c.logger.Info("kiss return command received", "remote", remote)
	default:
		c.logger.Debug("unknown kiss command", "cmd", f.Command, "remote", remote)
	}
}

// dispatchDataFrame applies the Mode routing rule. Matches the
// server's implementation but inline to avoid building a Server
// shell just to consume its dispatcher.
func (c *Client) dispatchDataFrame(ctx context.Context, remote string, channel uint32, ax *ax25.Frame, rawAX []byte, mode Mode) {
	switch mode {
	case ModeTnc:
		if c.cfg.RxIngress == nil {
			c.logger.Warn("kiss client tnc-mode frame dropped: no RxIngress wired",
				"remote", remote, "channel", channel)
			return
		}
		// Rate-limit TNC-mode ingress so a flapping or hostile peer
		// cannot flood the ingest fanout. Mirrors server.go's gate.
		if c.rateLimiter != nil && !c.rateLimiter.Allow() {
			return
		}
		rf := &pb.ReceivedFrame{Channel: channel, Data: rawAX}
		c.cfg.RxIngress(rf, ingress.KissTnc(c.cfg.InterfaceID))
	default:
		if c.cfg.Sink != nil {
			err := c.cfg.Sink.Submit(ctx, channel, ax, txgovernor.SubmitSource{
				Kind:     "kiss",
				Detail:   c.cfg.Name + " " + remote,
				Priority: ax25.PriorityClient,
			})
			if err != nil {
				c.logger.Warn("tx governor rejected kiss frame", "err", err)
			}
		}
	}
}

// channelFor returns the channel configured for the given KISS port
// number, or 1 as the default.
func (c *Client) channelFor(port uint8) uint32 {
	if ch, ok := c.cfg.ChannelMap[port]; ok {
		return ch
	}
	return 1
}

// writeFrame encodes axBytes into a KISS data frame on the port that
// matches channel, then writes it to the live conn with a 10s
// deadline (same hung-peer guard the server uses). Called from the
// per-instance queue's drainer goroutine.
//
// On write failure: proactively close the conn so frameLoop's read
// side observes EOF / net.ErrClosed and the supervisor advances to
// Backoff + reconnect. Without the close, a half-closed write side
// would silently drop frames for up to the read timeout.
func (c *Client) writeFrame(channel uint32, axBytes []byte) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		if c.onTxDrop != nil {
			c.onTxDrop("no-conn")
		}
		return
	}
	// Deterministic reverse lookup: sort port keys ascending so the
	// choice of outbound port is stable across process restarts.
	port := uint8(0)
	if len(c.cfg.ChannelMap) > 0 {
		keys := make([]int, 0, len(c.cfg.ChannelMap))
		for p, ch := range c.cfg.ChannelMap {
			if ch == channel {
				keys = append(keys, int(p))
			}
		}
		if len(keys) > 0 {
			sort.Ints(keys)
			port = uint8(keys[0])
		}
	}
	raw := Encode(port, axBytes)
	if err := conn.SetWriteDeadline(time.Now().Add(instanceTxSocketDeadline)); err != nil {
		c.logger.Debug("kiss client SetWriteDeadline failed", "err", err)
	}
	defer func() { _ = conn.SetWriteDeadline(time.Time{}) }()
	if _, err := conn.Write(raw); err != nil {
		c.logger.Warn("kiss client tx write failed; forcing reconnect", "err", err)
		if c.onTxDrop != nil {
			c.onTxDrop("write-failed")
		}
		// Force the read side to EOF so the supervisor learns the peer
		// is dead and starts a reconnect. Close is idempotent — frameLoop
		// already closes the conn on exit.
		_ = conn.Close()
	}
}

// setState atomically updates the state fields and invokes onReload.
// Called from the supervisor on every transition.
func (c *Client) setState(state, lastErr string, retryAt int64, peerAddr string) {
	c.mu.Lock()
	c.state = state
	c.lastError = lastErr
	c.retryAtUnixMs = retryAt
	if peerAddr != "" {
		c.peerAddr = peerAddr
	}
	if state == StateConnecting || state == StateDisconnected || state == StateStopped {
		c.backoffSeconds = 0
	}
	c.mu.Unlock()
	if c.onReload != nil {
		c.onReload()
	}
}

// sleepWithWake blocks for d, returning true if the delay elapsed
// cleanly (caller should re-dial) or false if ctx was cancelled
// (caller should exit). Reconnect() sends on wakeBackoff to
// short-circuit the sleep.
func (c *Client) sleepWithWake(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-c.wakeBackoff:
		return true
	case <-t.C:
		return true
	}
}

// Reconnect cancels the current backoff wait (if any) so the
// supervisor dials immediately. Safe to call concurrently; the
// wakeBackoff channel is buffered size 1 and the send is
// non-blocking, so repeated calls coalesce.
func (c *Client) Reconnect() {
	select {
	case c.wakeBackoff <- struct{}{}:
	default:
	}
}

// Status returns a snapshot of the supervisor's current state. Safe
// to call concurrently.
func (c *Client) Status() InterfaceStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return InterfaceStatus{
		State:          c.state,
		LastError:      c.lastError,
		RetryAtUnixMs:  c.retryAtUnixMs,
		PeerAddr:       c.peerAddr,
		ConnectedSince: c.connectedSince,
		ReconnectCount: c.reconnectCount,
		BackoffSeconds: c.backoffSeconds,
	}
}

// close cancels the supervisor's context and blocks until the loop
// exits. Idempotent.
func (c *Client) close() {
	c.mu.Lock()
	cancel := c.cancel
	c.cancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	<-c.done
}

// instanceQueue returns the per-instance tx queue used by the
// Manager's TransmitOnChannel path. Nil when the client was
// constructed without AllowTxFromGovernor.
func (c *Client) instanceQueue() *instanceTxQueue {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.txQueue
}

// SetTxDropObserver wires a per-interface metric hook that fires on
// writer-goroutine drop reasons ("write-failed", "no-conn"). Called by
// Manager.StartClient alongside the queue-depth / queue-drop hooks so
// the Phase 4 metric surface is unified.
func (c *Client) SetTxDropObserver(fn func(reason string)) {
	c.mu.Lock()
	c.onTxDrop = fn
	c.mu.Unlock()
}
