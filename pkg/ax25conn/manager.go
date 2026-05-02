package ax25conn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// Sentinel errors returned by Open. Callers can errors.Is these to
// classify failures without substring-matching error text.
var (
	// ErrManagerClosed is returned by Open after Close.
	ErrManagerClosed = errors.New("ax25conn: manager closed")
	// ErrChannelAPRSOnly is returned when Open targets a channel whose
	// configured Mode is APRS-only.
	ErrChannelAPRSOnly = errors.New("ax25conn: channel is APRS-only")
	// ErrMaxTotal is returned when MaxTotal sessions are already open.
	ErrMaxTotal = errors.New("ax25conn: max total sessions reached")
	// ErrMaxPerOperator is returned when an operator has hit MaxPerOperator.
	ErrMaxPerOperator = errors.New("ax25conn: per-operator session cap reached")
	// ErrSessionExists is returned when (channel, local, peer) is already
	// bound to a live session.
	ErrSessionExists = errors.New("ax25conn: session already exists for this triple")
)

// ManagerConfig is the static configuration for the per-process
// session manager.
type ManagerConfig struct {
	TxSink         txgovernor.TxSink
	ChannelModes   configstore.ChannelModeLookup
	Logger         *slog.Logger
	MaxPerOperator int // default 4
	MaxTotal       int // default 64
}

// Manager owns the per-process LAPB session table. Caps are
// process-local — they limit concurrent live sessions inside one
// graywolf instance. After a graywolf restart, prior sessions are
// gone and operators may reconnect freely. The same operator across
// multiple browser sessions or API tokens accumulates against the
// cap inside this process.
type Manager struct {
	cfg    ManagerConfig
	mu     sync.Mutex
	byKey  map[sessionKey]*managedSession
	byID   map[uint64]*managedSession
	nextID atomic.Uint64
	closed bool
}

// sessionKey routes inbound LAPB frames to the owning Session. Keying
// is intentionally `(channel, local, peer)` — *not* the via path —
// because:
//
//  1. AX.25 v2.x semantics: a (local, peer) pair has at most one
//     logical link. Two sessions over different via paths to the same
//     peer would race.
//  2. Inbound frames carry the *forward* path digipeaters already
//     consumed; the Session was opened with the *outbound* via list.
//     Path-fingerprint comparison is reserved for telemetry display
//     (Task 1.13b) and is not used for routing.
type sessionKey struct {
	Channel uint32
	Local   string
	Peer    string
}

type managedSession struct {
	id       uint64
	s        *Session
	cancel   context.CancelFunc
	op       string
	lastPath []ax25.Address // most recent inbound digipeater chain
}

// NewManager constructs a Manager. Caps default to 4 per operator, 64
// total when zero.
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.MaxPerOperator == 0 {
		cfg.MaxPerOperator = 4
	}
	if cfg.MaxTotal == 0 {
		cfg.MaxTotal = 64
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Manager{
		cfg:   cfg,
		byKey: make(map[sessionKey]*managedSession),
		byID:  make(map[uint64]*managedSession),
	}
}

// Open creates and starts a new session bound to (Channel, Local,
// Peer) from scfg. Operator is the caller's identity for cap
// accounting. Returns the session id and the *Session handle the
// bridge uses to push events.
func (m *Manager) Open(scfg SessionConfig, operator string) (uint64, *Session, error) {
	if scfg.TxSink == nil {
		scfg.TxSink = m.cfg.TxSink
	}
	if scfg.Logger == nil {
		scfg.Logger = m.cfg.Logger
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, nil, ErrManagerClosed
	}
	if m.cfg.ChannelModes != nil {
		mode, _ := m.cfg.ChannelModes.ModeForChannel(context.Background(), scfg.Channel)
		if mode == configstore.ChannelModeAPRS {
			return 0, nil, fmt.Errorf("%w: channel %d", ErrChannelAPRSOnly, scfg.Channel)
		}
	}
	if len(m.byKey) >= m.cfg.MaxTotal {
		return 0, nil, ErrMaxTotal
	}
	count := 0
	for _, ms := range m.byKey {
		if ms.op == operator {
			count++
		}
	}
	if count >= m.cfg.MaxPerOperator {
		return 0, nil, ErrMaxPerOperator
	}
	key := sessionKey{
		Channel: scfg.Channel,
		Local:   scfg.Local.String(),
		Peer:    scfg.Peer.String(),
	}
	if _, exists := m.byKey[key]; exists {
		return 0, nil, ErrSessionExists
	}
	s, err := NewSession(scfg)
	if err != nil {
		return 0, nil, err
	}
	id := m.nextID.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	ms := &managedSession{id: id, s: s, cancel: cancel, op: operator}
	m.byKey[key] = ms
	m.byID[id] = ms
	go func() {
		s.Run(ctx)
		m.remove(key, id)
	}()
	return id, s, nil
}

func (m *Manager) remove(k sessionKey, id uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byKey, k)
	delete(m.byID, id)
}

// Dispatch routes a non-UI frame to the matching session. Caller is
// the pkg/app rxfanout; per the brainstorm §4.1, the frame is
// guaranteed non-UI by the caller.
func (m *Manager) Dispatch(channel uint32, f *Frame) {
	if f == nil {
		return
	}
	key := sessionKey{
		Channel: channel,
		Local:   f.Dest.String(),
		Peer:    f.Source.String(),
	}
	m.mu.Lock()
	ms := m.byKey[key]
	if ms != nil {
		ms.lastPath = m.normalizeInboundPath(f)
	}
	m.mu.Unlock()
	if ms == nil {
		return
	}
	ms.s.Submit(Event{Kind: EventFrameRX, Frame: f})
}

// LastPath returns the path the most recent inbound frame for sid
// traversed (digipeater chain), normalized to drop the trailing
// not-yet-repeated entries that should not appear on inbound frames.
// Empty if no inbound frame has been routed to the session yet.
func (m *Manager) LastPath(sid uint64) []ax25.Address {
	m.mu.Lock()
	defer m.mu.Unlock()
	ms := m.byID[sid]
	if ms == nil {
		return nil
	}
	out := make([]ax25.Address, len(ms.lastPath))
	copy(out, ms.lastPath)
	return out
}

// normalizeInboundPath returns the digipeater chain the frame
// traversed, dropping any unrepeated entries that should not appear
// on inbound frames addressed to us. Used only for telemetry display.
//
// Rule: every entry with Repeated=true is part of the traversed
// chain; an unrepeated entry interleaved between repeated ones means
// the frame is mid-traversal and should not have been routed to us —
// log + drop the unrepeated tail.
func (m *Manager) normalizeInboundPath(f *Frame) []ax25.Address {
	if len(f.Path) == 0 {
		return nil
	}
	out := make([]ax25.Address, 0, len(f.Path))
	for _, a := range f.Path {
		if a.Repeated {
			b := a
			b.Repeated = false
			out = append(out, b)
			continue
		}
		// Unrepeated entry — log and stop the traversal record here.
		m.cfg.Logger.Debug("ax25conn: inbound frame has unrepeated digipeater entry",
			"call", a.String(), "frame_source", f.Source.String())
		break
	}
	return out
}

// Count returns the number of live sessions (for tests and metrics).
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.byKey)
}

// Close cancels every running session. Safe to call multiple times.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	for _, ms := range m.byKey {
		ms.cancel()
	}
}

// Run blocks until ctx is done; provided for symmetric shape with
// other graywolf services. Sessions are managed via Open; Run is a
// no-op loop that just waits for shutdown.
func (m *Manager) Run(ctx context.Context) {
	<-ctx.Done()
	m.Close()
}
