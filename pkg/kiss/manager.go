package kiss

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// Interface lifecycle states reported by Manager.Status().
//
// Phase 1 only populates StateListening and StateStopped for the
// existing server-listen interface type. The Connected / Connecting /
// Backoff / Disconnected states are pre-declared here so Phase 4 (KISS
// tcp-client) can slot supervisor state in without an InterfaceStatus
// schema change.
const (
	StateListening    = "listening"
	StateStopped      = "stopped"
	StateConnected    = "connected"
	StateConnecting   = "connecting"
	StateBackoff      = "backoff"
	StateDisconnected = "disconnected"
)

// InterfaceStatus is the unified status snapshot returned by
// Manager.Status() for every managed KISS interface — server-listen
// today, tcp-client once Phase 4 lands. Fields that don't apply to a
// given interface type carry zero values.
//
// State, LastError, and RetryAtUnixMs describe the current lifecycle
// state. PeerAddr, ConnectedSince, ReconnectCount, and BackoffSeconds
// are Phase 4 placeholders (tcp-client supervisor telemetry) — Phase 1
// reports them as zero for every interface. The fields are declared
// now so the /api/channels backing DTO and the Kiss page don't need
// schema changes when Phase 4 fills them in.
type InterfaceStatus struct {
	// State is the lifecycle state. One of the State* constants above.
	State string
	// LastError is the most recent non-nil error from the transport, if
	// any. Cleared on successful (re)connection. Empty in Phase 1.
	LastError string
	// RetryAtUnixMs is the wall-clock time (Unix milliseconds) at which
	// the supervisor plans to retry a failed dial. Zero when not in
	// backoff or not applicable. Phase 4 fills this in.
	RetryAtUnixMs int64
	// PeerAddr is the dialed or accepted peer address, if known. Phase 4
	// populates this for tcp-client. Empty in Phase 1.
	PeerAddr string
	// ConnectedSince is the Unix millisecond timestamp when the current
	// session became live. Zero when not connected. Phase 4.
	ConnectedSince int64
	// ReconnectCount is the cumulative number of reconnect attempts the
	// supervisor has made since process start. Phase 4.
	ReconnectCount uint64
	// BackoffSeconds is the current backoff duration in seconds, used
	// by the UI to render a countdown. Zero when not in backoff. Phase 4.
	BackoffSeconds uint32
}

// Manager tracks running KISS TCP servers and supports hot start/stop.
type Manager struct {
	sink                  txgovernor.TxSink
	logger                *slog.Logger
	onDecodeError         func()
	onFrameIngress        func(ifaceID uint32, mode Mode)
	onBroadcastSuppressed func(recipientID uint32)
	rxIngress             func(rf *pb.ReceivedFrame, src ingress.Source)
	clock                 Clock
	mu                    sync.Mutex
	// running maps DB ID → running server state.
	running map[uint32]*managedServer
	// onTxQueueDepth is an optional gauge reporter invoked whenever a
	// per-instance tx queue's depth changes. Labels: interface_id.
	onTxQueueDepth func(ifaceID uint32, depth int32)
	// onTxQueueDrop is an optional counter incrementer invoked when a
	// per-instance tx enqueue fails. Labels: interface_id, reason.
	onTxQueueDrop func(ifaceID uint32, reason string)
	// onClientStateChange fires on every tcp-client state transition.
	onClientStateChange func(ifaceID uint32, name string, st InterfaceStatus)
	// onClientReconnect fires once per successful dial.
	onClientReconnect func(ifaceID uint32)
}

type managedServer struct {
	server *Server
	// client is set instead of server when the interface was started
	// with StartClient (tcp-client). At most one of server / client
	// is non-nil for a given row. The manager's map keys on DB row
	// ID, so the union-of-types here is the cheapest way to unify
	// lifecycle bookkeeping across both kinds.
	client *Client
	cancel context.CancelFunc
	// txQueue is the per-instance bounded tx queue used by
	// TransmitOnChannel. Non-nil only when the interface was started
	// with a channel attachment; the per-instance writer goroutine
	// runs until cancel is called by Stop / Start's stop-if-exists
	// branch. For tcp-client interfaces the queue lives on the Client
	// itself — the manager shares the same pointer so
	// TransmitOnChannel + InstanceQueueFor don't care which kind.
	txQueue *instanceTxQueue
	// channel is the default channel attached to this interface row.
	// Used by TransmitOnChannel's channel filter.
	channel uint32
}

// ManagerConfig configures a Manager.
type ManagerConfig struct {
	Sink   txgovernor.TxSink
	Logger *slog.Logger
	// OnDecodeError, if non-nil, is installed on every Server the
	// Manager starts. A shared counter across all KISS interfaces is
	// intentional: the metric is about "kiss frames that failed
	// ax25 decoding" at the system level, not per-interface.
	OnDecodeError func()
	// OnFrameIngress, if non-nil, is invoked for every KISS data frame
	// that successfully AX.25-decodes at any managed server, with the
	// server's interface ID and its configured Mode. Observation hook
	// used by Phase 5 of the KISS modem/TNC plan to drive the
	// graywolf_kiss_ingress_frames_total counter.
	OnFrameIngress func(ifaceID uint32, mode Mode)
	// OnBroadcastSuppressed, if non-nil, is invoked once per recipient
	// skipped by BroadcastFromChannel's self-loop guard (the
	// originating TNC-mode interface). Phase 5 uses this to drive the
	// graywolf_kiss_broadcast_suppressed_total counter.
	OnBroadcastSuppressed func(recipientID uint32)
	// RxIngress, if non-nil, is installed on every Server started in
	// ModeTnc. The wiring layer wraps this with a non-blocking send
	// into the shared modem-RX fanout channel. Callers may set it
	// later via SetRxIngress; configs started before it is set run
	// without TNC routing (frames are dropped with a warning).
	RxIngress func(rf *pb.ReceivedFrame, src ingress.Source)
	// Clock is the rate-limiter time source installed on every Server.
	// nil selects wall time; tests inject a fake clock.
	Clock Clock
	// OnTxQueueDepth is an optional per-instance gauge reporter.
	// Invoked on every enqueue / drain with the updated depth so the
	// wiring layer can surface graywolf_kiss_instance_tx_queue_depth.
	OnTxQueueDepth func(ifaceID uint32, depth int32)
	// OnTxQueueDrop is an optional counter incrementer invoked when a
	// per-instance tx enqueue fails ("busy" | "down").
	OnTxQueueDrop func(ifaceID uint32, reason string)
	// OnClientStateChange, if non-nil, is invoked on every state
	// transition of a tcp-client supervisor. The wiring layer uses
	// this to surface the Phase 4 kiss_client metrics (connected
	// gauge, backoff_seconds gauge) without having to wire a
	// dedicated per-start callback from every notifyKissManager
	// path. name is the interface display name; st is the live
	// status snapshot.
	OnClientStateChange func(ifaceID uint32, name string, st InterfaceStatus)
	// OnClientReconnect fires exactly once per successful dial on a
	// tcp-client supervisor. Wired to the Phase 4
	// graywolf_kiss_client_reconnects_total counter.
	OnClientReconnect func(ifaceID uint32)
}

// NewManager creates a Manager. Call Start to launch individual servers.
func NewManager(cfg ManagerConfig) *Manager {
	lg := cfg.Logger
	if lg == nil {
		lg = slog.Default()
	}
	return &Manager{
		sink:                  cfg.Sink,
		logger:                lg,
		onDecodeError:         cfg.OnDecodeError,
		onFrameIngress:        cfg.OnFrameIngress,
		onBroadcastSuppressed: cfg.OnBroadcastSuppressed,
		rxIngress:             cfg.RxIngress,
		clock:                 cfg.Clock,
		running:               make(map[uint32]*managedServer),
		onTxQueueDepth:        cfg.OnTxQueueDepth,
		onTxQueueDrop:         cfg.OnTxQueueDrop,
		onClientStateChange:   cfg.OnClientStateChange,
		onClientReconnect:     cfg.OnClientReconnect,
	}
}

// SetRxIngress replaces the RX-ingress callback for future Server
// starts. Running servers keep their previously-bound callback; since
// every config update tears the server down via Start's stop-if-exists
// branch, the next UI-driven reconfigure picks up the new callback.
// Safe to call before any Start.
func (m *Manager) SetRxIngress(fn func(rf *pb.ReceivedFrame, src ingress.Source)) {
	m.mu.Lock()
	m.rxIngress = fn
	m.mu.Unlock()
}

// Start launches a KISS TCP server for the given DB row. If a server with
// that ID is already running it is stopped first.
func (m *Manager) Start(parent context.Context, id uint32, cfg ServerConfig) {
	m.mu.Lock()

	// Stop existing if any — handle both server-listen and tcp-client
	// (client.close blocks on done, so release the lock before calling
	// into it).
	if ms, ok := m.running[id]; ok {
		delete(m.running, id)
		m.mu.Unlock()
		if ms.client != nil {
			ms.cancel()
			ms.client.close()
		} else {
			if ms.txQueue != nil {
				ms.txQueue.Close()
			}
			ms.cancel()
		}
		m.mu.Lock()
	}
	defer m.mu.Unlock()

	cfg.Sink = m.sink
	if cfg.Logger == nil {
		cfg.Logger = m.logger
	}
	if cfg.OnDecodeError == nil {
		cfg.OnDecodeError = m.onDecodeError
	}
	if cfg.OnFrameIngress == nil && m.onFrameIngress != nil {
		// Capture id so each Server's hook carries its own iface ID
		// without the Server needing to know about the Manager.
		ifaceID := id
		fn := m.onFrameIngress
		cfg.OnFrameIngress = func(mode Mode) { fn(ifaceID, mode) }
	}
	if cfg.RxIngress == nil {
		cfg.RxIngress = m.rxIngress
	}
	if cfg.Clock == nil {
		cfg.Clock = m.clock
	}
	// InterfaceID is load-bearing for TNC-mode source tagging. The
	// caller passes the DB row ID separately as `id`; mirror it onto
	// the config so ingress.KissTnc(srv.cfg.InterfaceID) is correct
	// regardless of which call site built the literal.
	cfg.InterfaceID = id

	ctx, cancel := context.WithCancel(parent)
	srv := NewServer(cfg)
	ms := &managedServer{server: srv, cancel: cancel, channel: cfg.firstChannel()}

	// Per-instance tx queue: only when Mode=tnc AND the operator has
	// explicitly enabled governor TX (D4). Modem-mode interfaces TX
	// via Submit (they don't receive TX from the governor), so no
	// queue is needed. The queue's writer goroutine lives on ctx so
	// Stop / Start's replacement cancels it cleanly.
	if cfg.Mode == ModeTnc && cfg.AllowTxFromGovernor {
		ch := ms.channel
		broadcast := func(axBytes []byte) {
			srv.TxBroadcast(ch, axBytes, instanceTxSocketDeadline)
		}
		q := newInstanceTxQueue(ctx, broadcast)
		// Wire metric observers with the interface ID captured.
		ifaceID := id
		var onEnqueue func()
		var onDrop func(string)
		var onDepth func(int32)
		if m.onTxQueueDepth != nil {
			onDepth = func(d int32) { m.onTxQueueDepth(ifaceID, d) }
		}
		if m.onTxQueueDrop != nil {
			onDrop = func(reason string) { m.onTxQueueDrop(ifaceID, reason) }
		}
		q.SetObservers(onEnqueue, onDrop, onDepth)
		ms.txQueue = q
	}

	m.running[id] = ms

	go func() {
		if err := srv.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
			m.logger.Error("kiss server", "name", cfg.Name, "err", err)
		}
	}()
}

// StopAll shuts down every running server and waits for their goroutines
// to exit. Called by the wiring layer's shutdown orchestrator (D15) so
// kiss-owned resources are released in the correct sequence relative
// to the dispatcher + modembridge. Safe to call multiple times.
func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]uint32, 0, len(m.running))
	for id := range m.running {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

// Stop shuts down the server for the given DB row ID.
func (m *Manager) Stop(id uint32) {
	m.mu.Lock()
	ms, ok := m.running[id]
	if ok {
		delete(m.running, id)
	}
	m.mu.Unlock()
	if !ok {
		return
	}
	if ms.client != nil {
		// For clients the tx queue is owned by the client and will be
		// closed as part of its run() shutdown.
		ms.cancel()
		ms.client.close()
		return
	}
	if ms.txQueue != nil {
		ms.txQueue.Close()
	}
	ms.cancel()
}

// StartClient launches a tcp-client KISS supervisor for the given DB
// row. Mirrors Start (server-listen) — any previously-running
// interface under id is stopped first. Exactly one of Start /
// StartClient runs per row depending on InterfaceType.
//
// The supervisor goroutine runs until ctx is cancelled (either at
// process shutdown or by a subsequent Start / StartClient / Stop
// call that replaces this row). Dial attempts, backoff, and reconnect
// telemetry are all surfaced via Manager.Status(). Reconnect(id)
// short-circuits the current backoff wait.
func (m *Manager) StartClient(parent context.Context, id uint32, cfg ClientConfig) {
	m.mu.Lock()
	if existing, ok := m.running[id]; ok {
		delete(m.running, id)
		m.mu.Unlock()
		if existing.client != nil {
			existing.cancel()
			existing.client.close()
		} else {
			if existing.txQueue != nil {
				existing.txQueue.Close()
			}
			existing.cancel()
		}
		m.mu.Lock()
	}

	if cfg.Logger == nil {
		cfg.Logger = m.logger
	}
	if cfg.Sink == nil {
		cfg.Sink = m.sink
	}
	if cfg.OnDecodeError == nil {
		cfg.OnDecodeError = m.onDecodeError
	}
	if cfg.OnFrameIngress == nil && m.onFrameIngress != nil {
		ifaceID := id
		fn := m.onFrameIngress
		cfg.OnFrameIngress = func(mode Mode) { fn(ifaceID, mode) }
	}
	if cfg.RxIngress == nil {
		cfg.RxIngress = m.rxIngress
	}
	if cfg.Clock == nil {
		cfg.Clock = m.clock
	}
	cfg.InterfaceID = id
	userOnReload := cfg.OnReload
	ctx, cancel := context.WithCancel(parent)
	cli := newClient(cfg)
	// Chain OnReload with the manager-level state change callback so
	// every caller (wiring.go, webapi notifyKissManager) gets both
	// the dispatcher nudge and the Phase 4 metric updates without
	// duplicating the wiring at every call site. Assigned on cli
	// directly (rather than via cfg) so the closure sees the live
	// pointer for its Status() call.
	if m.onClientStateChange != nil {
		managerHook := m.onClientStateChange
		ifaceID := id
		ifaceName := cfg.Name
		cli.onReload = func() {
			if userOnReload != nil {
				userOnReload()
			}
			managerHook(ifaceID, ifaceName, cli.Status())
		}
	}
	if m.onClientReconnect != nil {
		ifaceID := id
		reconnectHook := m.onClientReconnect
		cli.onReconnect = func() { reconnectHook(ifaceID) }
	}
	ch := uint32(1)
	for _, v := range cfg.ChannelMap {
		ch = v
		break
	}
	ms := &managedServer{client: cli, cancel: cancel, channel: ch}
	m.running[id] = ms
	m.mu.Unlock()

	// Publish the per-instance tx queue synchronously. newClient
	// constructed it already (no more sleep-loop polling), so the
	// pointer is reachable right after registration. Wire metric
	// observers and socket-write drop observer before the supervisor
	// goroutine starts, guaranteeing the first connected frame is
	// already instrumented.
	if cfg.AllowTxFromGovernor && cfg.Mode == ModeTnc {
		if q := cli.instanceQueue(); q != nil {
			m.mu.Lock()
			ms.txQueue = q
			m.mu.Unlock()
			ifaceID := id
			var onEnqueue func()
			var onDrop func(string)
			var onDepth func(int32)
			if m.onTxQueueDepth != nil {
				onDepth = func(d int32) { m.onTxQueueDepth(ifaceID, d) }
			}
			if m.onTxQueueDrop != nil {
				onDrop = func(reason string) { m.onTxQueueDrop(ifaceID, reason) }
			}
			q.SetObservers(onEnqueue, onDrop, onDepth)
			if m.onTxQueueDrop != nil {
				cli.SetTxDropObserver(func(reason string) { m.onTxQueueDrop(ifaceID, reason) })
			}
		}
	}

	// Launch the supervisor now that the queue is published.
	go func() {
		cli.run(ctx)
	}()
}

// Reconnect short-circuits the current backoff wait on the tcp-client
// interface under id. Returns nil on success; returns a non-nil error
// when id is not registered or is not a tcp-client supervisor.
func (m *Manager) Reconnect(id uint32) error {
	m.mu.Lock()
	ms, ok := m.running[id]
	m.mu.Unlock()
	if !ok {
		return errors.New("kiss: interface not running")
	}
	if ms.client == nil {
		return errors.New("kiss: interface is not a tcp-client")
	}
	ms.client.Reconnect()
	return nil
}

// InstanceQueueFor returns the per-instance tx queue for the running
// interface under the given DB row ID, or nil if no interface is
// running or the interface was started without TX-from-governor
// enabled. Exposed so the txbackend layer can construct a
// KissTncBackend wrapper for each eligible interface; the returned
// queue's Enqueue method is the backend's non-blocking submit path.
func (m *Manager) InstanceQueueFor(id uint32) InstanceQueue {
	m.mu.Lock()
	defer m.mu.Unlock()
	ms, ok := m.running[id]
	if !ok || ms.txQueue == nil {
		return nil
	}
	return ms.txQueue
}

// InstanceQueue is the public face of the per-instance tx queue.
// Purely an interface so the txbackend package can hold references
// without depending on the unexported queue type.
type InstanceQueue interface {
	Enqueue(frame []byte, frameID uint64) error
	Depth() int32
}

// TransmitOnChannel enqueues frame on every TNC-mode server interface
// attached to channel ch with AllowTxFromGovernor=true. Returns
// (acceptedCount, errors.Join(perInstanceFailures...)): accepted is
// the number of queues that accepted the frame; err is nil when every
// enqueue succeeded, a joined error when one or more returned
// ErrBackendBusy / ErrBackendDown / transport failures.
//
// Non-blocking — a full queue or stopped writer returns immediately
// with the corresponding sentinel. Governor callers treat accepted>0
// as success for the fan-out semantics (see txbackend.Dispatcher).
func (m *Manager) TransmitOnChannel(ctx context.Context, ch uint32, frame []byte, frameID uint64) (int, error) {
	_ = ctx // enqueue is non-blocking; ctx reserved for future use.
	m.mu.Lock()
	type target struct {
		q InstanceQueue
	}
	targets := make([]target, 0, len(m.running))
	for _, ms := range m.running {
		if ms.txQueue == nil {
			continue
		}
		if ms.channel != ch {
			continue
		}
		targets = append(targets, target{q: ms.txQueue})
	}
	m.mu.Unlock()

	if len(targets) == 0 {
		return 0, nil
	}

	var (
		accepted int
		errs     []error
	)
	for _, t := range targets {
		if err := t.q.Enqueue(frame, frameID); err != nil {
			errs = append(errs, err)
			continue
		}
		accepted++
	}
	var joined error
	if len(errs) > 0 {
		joined = errors.Join(errs...)
	}
	return accepted, joined
}

// Dropped returns the cumulative rate-limit drop count for the running
// server under the given DB ID. Returns 0 if the ID is not running or
// is a tcp-client (clients have no server-side rate limiter).
// Phase 5 wires this into a Prometheus counter.
func (m *Manager) Dropped(id uint32) uint64 {
	m.mu.Lock()
	ms, ok := m.running[id]
	m.mu.Unlock()
	if !ok || ms.server == nil {
		return 0
	}
	return ms.server.Dropped()
}

// ActiveClients returns the current count of connected KISS clients on
// the running server under the given DB ID. Returns 0 if the ID is
// not running or is a tcp-client. Primarily consumed by tests that need
// to block until a client is registered before exercising a broadcast
// path.
func (m *Manager) ActiveClients(id uint32) int {
	m.mu.Lock()
	ms, ok := m.running[id]
	m.mu.Unlock()
	if !ok || ms.server == nil {
		return 0
	}
	return ms.server.ActiveClients()
}

// QueueOverflow returns the cumulative per-interface ingress-queue
// overflow count for the running server under the given DB ID.
// Returns 0 if the ID is not running or is a tcp-client.
// Phase 5 wires this into a Prometheus counter.
func (m *Manager) QueueOverflow(id uint32) uint64 {
	m.mu.Lock()
	ms, ok := m.running[id]
	m.mu.Unlock()
	if !ok || ms.server == nil {
		return 0
	}
	return ms.server.QueueOverflow()
}

// Status returns a snapshot of the current lifecycle state for every
// managed KISS interface keyed by DB row ID. Server-listen interfaces
// report StateListening with zero-valued supervisor telemetry; tcp-client
// interfaces report their supervisor's full state (connected / connecting
// / backoff / disconnected / stopped, plus LastError, RetryAtUnixMs,
// PeerAddr, ConnectedSince, ReconnectCount, BackoffSeconds).
func (m *Manager) Status() map[uint32]InterfaceStatus {
	m.mu.Lock()
	type entry struct {
		client *Client
	}
	snapshot := make(map[uint32]entry, len(m.running))
	for id, ms := range m.running {
		snapshot[id] = entry{client: ms.client}
	}
	m.mu.Unlock()

	out := make(map[uint32]InterfaceStatus, len(snapshot))
	for id, e := range snapshot {
		if e.client != nil {
			out[id] = e.client.Status()
			continue
		}
		out[id] = InterfaceStatus{State: StateListening}
	}
	return out
}

// BroadcastFromChannel fans out a received frame to all running servers.
// When skip is true, the server registered under skipID is excluded —
// used to suppress echo back to a KISS-TNC interface that just injected
// the frame. skipID is ignored when skip is false.
//
// tcp-client interfaces are NOT broadcast targets: the RX-fanout of a
// locally-decoded frame is an RF-originated event that only belongs on
// server-listen interfaces (where connected KISS apps expect to see
// over-the-air traffic). A tcp-client's remote peer is already the
// authoritative source of its channel's traffic.
func (m *Manager) BroadcastFromChannel(channel uint32, axBytes []byte, skipID uint32, skip bool) {
	m.mu.Lock()
	type idServer struct {
		id  uint32
		srv *Server
	}
	servers := make([]idServer, 0, len(m.running))
	for id, ms := range m.running {
		if ms.server == nil {
			continue
		}
		servers = append(servers, idServer{id: id, srv: ms.server})
	}
	m.mu.Unlock()

	for _, s := range servers {
		if skip && s.id == skipID {
			if m.onBroadcastSuppressed != nil {
				m.onBroadcastSuppressed(s.id)
			}
			continue
		}
		s.srv.BroadcastFromChannel(channel, axBytes)
	}
}
