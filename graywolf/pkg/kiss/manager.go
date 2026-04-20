package kiss

import (
	"context"
	"log/slog"
	"sync"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

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
}

type managedServer struct {
	server *Server
	cancel context.CancelFunc
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
	defer m.mu.Unlock()

	// Stop existing if any.
	if ms, ok := m.running[id]; ok {
		ms.cancel()
		delete(m.running, id)
	}

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
	m.running[id] = &managedServer{server: srv, cancel: cancel}

	go func() {
		if err := srv.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
			m.logger.Error("kiss server", "name", cfg.Name, "err", err)
		}
	}()
}

// Stop shuts down the server for the given DB row ID.
func (m *Manager) Stop(id uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ms, ok := m.running[id]; ok {
		ms.cancel()
		delete(m.running, id)
	}
}

// Dropped returns the cumulative rate-limit drop count for the running
// server under the given DB ID. Returns 0 if the ID is not running.
// Phase 5 wires this into a Prometheus counter.
func (m *Manager) Dropped(id uint32) uint64 {
	m.mu.Lock()
	ms, ok := m.running[id]
	m.mu.Unlock()
	if !ok {
		return 0
	}
	return ms.server.Dropped()
}

// ActiveClients returns the current count of connected KISS clients on
// the running server under the given DB ID. Returns 0 if the ID is
// not running. Primarily consumed by tests that need to block until a
// client is registered before exercising a broadcast path.
func (m *Manager) ActiveClients(id uint32) int {
	m.mu.Lock()
	ms, ok := m.running[id]
	m.mu.Unlock()
	if !ok {
		return 0
	}
	return ms.server.ActiveClients()
}

// QueueOverflow returns the cumulative per-interface ingress-queue
// overflow count for the running server under the given DB ID.
// Returns 0 if the ID is not running. Phase 5 wires this into a
// Prometheus counter.
func (m *Manager) QueueOverflow(id uint32) uint64 {
	m.mu.Lock()
	ms, ok := m.running[id]
	m.mu.Unlock()
	if !ok {
		return 0
	}
	return ms.server.QueueOverflow()
}

// BroadcastFromChannel fans out a received frame to all running servers.
// When skip is true, the server registered under skipID is excluded —
// used to suppress echo back to a KISS-TNC interface that just injected
// the frame. skipID is ignored when skip is false.
func (m *Manager) BroadcastFromChannel(channel uint32, axBytes []byte, skipID uint32, skip bool) {
	m.mu.Lock()
	type idServer struct {
		id  uint32
		srv *Server
	}
	servers := make([]idServer, 0, len(m.running))
	for id, ms := range m.running {
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
