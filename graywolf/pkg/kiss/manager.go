package kiss

import (
	"context"
	"log/slog"
	"sync"
)

// Manager tracks running KISS TCP servers and supports hot start/stop.
type Manager struct {
	sink   TxSink
	logger *slog.Logger
	mu     sync.Mutex
	// running maps DB ID → running server state.
	running map[uint32]*managedServer
}

type managedServer struct {
	server *Server
	cancel context.CancelFunc
}

// ManagerConfig configures a Manager.
type ManagerConfig struct {
	Sink   TxSink
	Logger *slog.Logger
}

// NewManager creates a Manager. Call Start to launch individual servers.
func NewManager(cfg ManagerConfig) *Manager {
	lg := cfg.Logger
	if lg == nil {
		lg = slog.Default()
	}
	return &Manager{
		sink:    cfg.Sink,
		logger:  lg,
		running: make(map[uint32]*managedServer),
	}
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

// BroadcastFromChannel fans out a received frame to all running servers.
func (m *Manager) BroadcastFromChannel(channel uint32, axBytes []byte) {
	m.mu.Lock()
	servers := make([]*Server, 0, len(m.running))
	for _, ms := range m.running {
		servers = append(servers, ms.server)
	}
	m.mu.Unlock()

	for _, s := range servers {
		s.BroadcastFromChannel(channel, axBytes)
	}
}
