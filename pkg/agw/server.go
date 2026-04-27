package agw

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/metrics"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// ServerConfig configures the AGW TCP server.
type ServerConfig struct {
	ListenAddr string
	// PortCallsigns lists the mycall of each radio port, in AGWPE port
	// order (index 0 = port 0). Used in the 'G' response.
	PortCallsigns []string
	// PortToChannel maps an AGW port number to a graywolf channel. If a
	// port isn't listed it defaults to PortToChannel[0] or channel 1.
	PortToChannel map[uint8]uint32
	// Sink receives parsed AX.25 frames for transmission. Typically
	// *txgovernor.Governor in production.
	Sink txgovernor.TxSink
	// Logger is optional.
	Logger *slog.Logger
	// OnClientChange is invoked with the new total-client count on connect
	// and disconnect. Optional.
	OnClientChange func(active int)
	// OnDecodeError is invoked for each raw-frame decoding attempt that
	// fails, with stage == "initial" when the first ax25.Decode fails (and
	// the skip-byte fallback is about to be tried) or stage == "fallback"
	// when both attempts fail and the frame is dropped. Optional.
	OnDecodeError func(stage string)
}

// Server is a multi-client AGWPE-compatible TCP server.
type Server struct {
	cfg    ServerConfig
	logger *slog.Logger
	// decodeErrLog rate-limits the "fallback decode failed" warn log
	// so a talkative misbehaving client cannot drown the operator's
	// log in its own confetti. Keyed per remote address so a flood on
	// one client does not mute a separate client hitting the same bug.
	decodeErrLog *metrics.RateLimitedLogger
	mu           sync.Mutex
	ln           net.Listener
	wg           sync.WaitGroup
	// shutdownCh is created by ListenAndServe and closed by Shutdown so
	// callers can tear the server down without having to cancel the
	// parent context.
	shutdownMu sync.Mutex
	shutdownCh chan struct{}
	clients    map[*clientState]struct{}
	active     int32
}

type clientState struct {
	conn      net.Conn
	writeMu   sync.Mutex
	mu        sync.Mutex
	monitor   bool
	callsigns map[string]struct{}
	// viaPath is the digipeater list supplied by the most recent 'V'
	// message. It is consumed (cleared) by the next 'M' (UNPROTO) send so
	// a client can choose a path per transmission.
	viaPath []string
}

// NewServer builds an AGW server. Does not listen until ListenAndServe.
func NewServer(cfg ServerConfig) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Server{
		cfg:          cfg,
		logger:       cfg.Logger.With("component", "agw"),
		decodeErrLog: metrics.NewRateLimitedLogger(10 * time.Second),
		clients:      make(map[*clientState]struct{}),
	}
}

// ActiveClients returns the current client count.
func (s *Server) ActiveClients() int { return int(atomic.LoadInt32(&s.active)) }

// LocalAddr returns the actual bound listener address. Returns nil until
// ListenAndServe has successfully bound. Useful for tests that pass
// ":0" and want the OS-assigned port.
func (s *Server) LocalAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return nil
	}
	return s.ln.Addr()
}

// ListenAndServe binds and serves until ctx is cancelled or Shutdown is
// called. Blocks. When it returns, the listener is closed and the bound
// port is free.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	s.logger.Info("agw server listening", "addr", ln.Addr().String())

	// shutdownCh lets Shutdown tear the listener down without requiring
	// the caller's context to be cancelled. Recreated on every call so
	// a restarted server gets a fresh channel.
	s.shutdownMu.Lock()
	s.shutdownCh = make(chan struct{})
	shutdownCh := s.shutdownCh
	s.shutdownMu.Unlock()

	// Close the listener on ctx cancel or Shutdown. Tracked in s.wg so
	// ListenAndServe cannot return until the listener is actually closed
	// and its port released.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		select {
		case <-ctx.Done():
		case <-shutdownCh:
		}
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				break
			}
			s.logger.Warn("accept error", "err", err)
			continue
		}
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			s.handleClient(ctx, c)
		}(conn)
	}
	// Ensure the cancel-watcher exits even if we broke out for a reason
	// other than ctx cancellation or Shutdown.
	s.shutdownMu.Lock()
	if s.shutdownCh != nil {
		select {
		case <-s.shutdownCh:
		default:
			close(s.shutdownCh)
		}
		s.shutdownCh = nil
	}
	s.shutdownMu.Unlock()
	s.wg.Wait()
	return nil
}

// Shutdown triggers an orderly exit of ListenAndServe without requiring
// the caller's context to be cancelled. It closes the listener (breaking
// Accept) and closes every live client connection to unblock their
// readers, then waits for all tracked goroutines up to ctx's deadline.
// Safe to call more than once; subsequent calls are no-ops.
func (s *Server) Shutdown(ctx context.Context) error {
	s.shutdownMu.Lock()
	ch := s.shutdownCh
	if ch != nil {
		select {
		case <-ch:
		default:
			close(ch)
		}
		s.shutdownCh = nil
	}
	s.shutdownMu.Unlock()

	// Close every live client connection so their handleClient loops
	// unblock and drain via s.wg.Wait() below.
	s.mu.Lock()
	conns := make([]net.Conn, 0, len(s.clients))
	for c := range s.clients {
		conns = append(conns, c.conn)
	}
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}

	waitCh := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(waitCh)
	}()
	select {
	case <-waitCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) handleClient(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	cs := &clientState{conn: conn, callsigns: make(map[string]struct{})}
	s.addClient(cs)
	defer s.removeClient(cs)
	remote := conn.RemoteAddr().String()
	s.logger.Info("agw client connected", "remote", remote)
	defer s.logger.Info("agw client disconnected", "remote", remote)

	// Close the connection on ctx cancel so ReadFrame unblocks. Tracked
	// in s.wg so Shutdown's wg.Wait cannot return until this watcher
	// has observed done and exited.
	done := make(chan struct{})
	defer close(done)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	for {
		h, data, err := ReadFrame(conn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			s.logger.Warn("agw read error", "remote", remote, "err", err)
			return
		}
		if err := s.dispatch(ctx, cs, h, data); err != nil {
			s.logger.Warn("agw dispatch error", "remote", remote, "kind", string(h.DataKind), "err", err)
			return
		}
	}
}

func (s *Server) dispatch(ctx context.Context, cs *clientState, h *Header, data []byte) error {
	switch h.DataKind {
	case KindVersion:
		// Reply: 4 bytes major (LE), 4 bytes minor — direwolf reports 2004.1.
		payload := make([]byte, 8)
		binary.LittleEndian.PutUint32(payload[0:4], 2004)
		binary.LittleEndian.PutUint32(payload[4:8], 1)
		return s.writeFrame(cs, &Header{DataKind: KindVersion}, payload)

	case KindPortInfo:
		return s.sendPortInfo(cs)

	case KindPortCaps:
		// 12-byte capabilities blob: on_air_baud, traffic_level, tx_delay,
		// tx_tail, persist, slot_time, max_frame, active_connections, ...
		// Fill plausible defaults.
		payload := make([]byte, 12)
		payload[0] = 0 // on-air baud index 0 = 1200
		payload[1] = 0xFF
		payload[2] = 30
		payload[3] = 10
		payload[4] = 63
		payload[5] = 10
		payload[6] = 7
		return s.writeFrame(cs, &Header{Port: h.Port, DataKind: KindPortCaps}, payload)

	case KindRegisterCallsign:
		cs.mu.Lock()
		cs.callsigns[h.CallFrom] = struct{}{}
		cs.mu.Unlock()
		// Ack: 1 byte, 0x01 = success.
		return s.writeFrame(cs, &Header{
			DataKind: KindRegisterCallsign,
			CallFrom: h.CallFrom,
		}, []byte{0x01})

	case KindUnregisterCallsign:
		cs.mu.Lock()
		delete(cs.callsigns, h.CallFrom)
		cs.mu.Unlock()
		return nil

	case KindMonitorOn:
		cs.mu.Lock()
		cs.monitor = true
		cs.mu.Unlock()
		return nil

	case KindSendUnproto:
		// data layout (direwolf): header.PID is the PID byte; data is the
		// info field. CallFrom → CallTo. If a prior 'V' frame stashed a
		// via-list on this client state, consume it here.
		cs.mu.Lock()
		via := cs.viaPath
		cs.viaPath = nil
		cs.mu.Unlock()
		return s.submitUnproto(ctx, cs, h, via, data)

	case KindSendUnprotoVia:
		// 'V' layout matches the AGWPE spec: one byte N = number of
		// digipeaters, then N×10 NUL-padded callsigns, then the info
		// field. direwolf's implementation stores these on the client
		// until the matching 'M' arrives.
		via, info, err := parseViaPayload(data)
		if err != nil {
			s.logger.Debug("agw V parse", "err", err)
			return nil
		}
		// APRSIS32 / UI-View send the V frame as a self-contained UI
		// transmission (the info payload rides along with the via list),
		// so we submit immediately. Clients that instead split V+M and
		// expect the via to cling to the next M also work because we
		// stash the via list in case no info was supplied.
		if len(info) == 0 {
			cs.mu.Lock()
			cs.viaPath = via
			cs.mu.Unlock()
			return nil
		}
		return s.submitUnproto(ctx, cs, h, via, info)

	case KindSendRaw:
		// Raw AX.25 frame in data.
		if len(data) < 1 {
			return nil
		}
		// direwolf format prepends one byte (the port?) in some clients;
		// keep it simple and try to decode directly. If decode fails, skip
		// the first byte and retry — a common direwolf idiom.
		ax, err := ax25.Decode(data)
		if err != nil {
			// Initial decode failed; the skip-byte retry may still
			// succeed, so this is a "fallback was needed" event, not
			// (yet) a dropped frame.
			if s.cfg.OnDecodeError != nil {
				s.cfg.OnDecodeError("initial")
			}
			if ax, err = ax25.Decode(data[1:]); err != nil {
				// Both attempts failed — the frame is dropped.
				if s.cfg.OnDecodeError != nil {
					s.cfg.OnDecodeError("fallback")
				}
				remote := cs.conn.RemoteAddr().String()
				s.decodeErrLog.Log(s.logger, slog.LevelWarn, remote,
					"agw raw frame failed ax25 decode",
					"remote", remote, "len", len(data), "err", err)
				return nil
			}
		}
		if !ax.IsUI() {
			s.logger.Debug("ignoring non-UI agw raw frame")
			return nil
		}
		if s.cfg.Sink != nil {
			return s.cfg.Sink.Submit(ctx, s.channelFor(h.Port), ax, txgovernor.SubmitSource{
				Kind:     "agw",
				Detail:   cs.conn.RemoteAddr().String(),
				Priority: ax25.PriorityClient,
			})
		}
		return nil

	default:
		// Connected-mode frames: 'C', 'D', 'd', 'v', 'V', 'c' etc. Log and drop.
		s.logger.Debug("unsupported agw frame kind", "kind", string(h.DataKind))
		return nil
	}
}

// submitUnproto builds a UI frame from an AGW 'M'/'V' submission and
// funnels it through the TxSink.
func (s *Server) submitUnproto(ctx context.Context, cs *clientState, h *Header, via []string, info []byte) error {
	src, err := ax25.ParseAddress(h.CallFrom)
	if err != nil {
		return nil
	}
	dst, err := ax25.ParseAddress(h.CallTo)
	if err != nil {
		return nil
	}
	path := make([]ax25.Address, 0, len(via))
	for _, v := range via {
		a, err := ax25.ParseAddress(v)
		if err != nil {
			s.logger.Debug("agw via parse", "addr", v, "err", err)
			continue
		}
		path = append(path, a)
	}
	f, err := ax25.NewUIFrame(src, dst, path, info)
	if err != nil {
		return nil
	}
	// Honor the client-supplied PID byte instead of forcing 0xF0.
	if h.PID != 0 {
		f.PID = h.PID
	}
	if s.cfg.Sink == nil {
		return nil
	}
	return s.cfg.Sink.Submit(ctx, s.channelFor(h.Port), f, txgovernor.SubmitSource{
		Kind:     "agw",
		Detail:   cs.conn.RemoteAddr().String(),
		Priority: ax25.PriorityClient,
	})
}

// parseViaPayload decodes the 'V' frame data field: 1-byte digi count,
// then N*10 bytes of NUL-padded digipeater callsigns, then the info
// field. Returns the digi list and the info payload.
func parseViaPayload(data []byte) ([]string, []byte, error) {
	if len(data) < 1 {
		return nil, nil, errors.New("agw: V frame empty")
	}
	n := int(data[0])
	const callLen = 10
	need := 1 + n*callLen
	if n > 8 {
		return nil, nil, fmt.Errorf("agw: V frame too many digis: %d", n)
	}
	if len(data) < need {
		return nil, nil, errors.New("agw: V frame truncated")
	}
	via := make([]string, 0, n)
	for i := 0; i < n; i++ {
		off := 1 + i*callLen
		call := trimNul(string(data[off : off+callLen]))
		if call != "" {
			via = append(via, call)
		}
	}
	return via, data[need:], nil
}

func (s *Server) sendPortInfo(cs *clientState) error {
	n := len(s.cfg.PortCallsigns)
	if n == 0 {
		n = 1
	}
	// direwolf format: text payload "<n>;Port1 desc;Port2 desc;..."
	msg := fmt.Sprintf("%d;", n)
	for i := 0; i < n; i++ {
		call := ""
		if i < len(s.cfg.PortCallsigns) {
			call = s.cfg.PortCallsigns[i]
		}
		msg += fmt.Sprintf("Port%d %s;", i+1, call)
	}
	payload := []byte(msg)
	payload = append(payload, 0)
	return s.writeFrame(cs, &Header{DataKind: KindPortInfo}, payload)
}

func (s *Server) writeFrame(cs *clientState, h *Header, data []byte) error {
	cs.writeMu.Lock()
	defer cs.writeMu.Unlock()
	return WriteFrame(cs.conn, h, data)
}

func (s *Server) channelFor(port uint8) uint32 {
	if ch, ok := s.cfg.PortToChannel[port]; ok {
		return ch
	}
	return 1
}

func (s *Server) addClient(c *clientState) {
	s.mu.Lock()
	s.clients[c] = struct{}{}
	n := int32(len(s.clients))
	s.mu.Unlock()
	atomic.StoreInt32(&s.active, n)
	if s.cfg.OnClientChange != nil {
		s.cfg.OnClientChange(int(n))
	}
}

func (s *Server) removeClient(c *clientState) {
	s.mu.Lock()
	delete(s.clients, c)
	n := int32(len(s.clients))
	s.mu.Unlock()
	atomic.StoreInt32(&s.active, n)
	if s.cfg.OnClientChange != nil {
		s.cfg.OnClientChange(int(n))
	}
}

// BroadcastMonitoredUI sends a received UI frame to every connected
// monitoring client as an AGW 'U' record.
func (s *Server) BroadcastMonitoredUI(port uint8, f *ax25.Frame) {
	text := f.String() + "\r"
	h := &Header{
		Port:     port,
		DataKind: KindMonitoredUI,
		PID:      f.PID,
		CallFrom: f.Source.String(),
		CallTo:   f.Dest.String(),
	}
	s.mu.Lock()
	targets := make([]*clientState, 0, len(s.clients))
	for c := range s.clients {
		c.mu.Lock()
		if c.monitor {
			targets = append(targets, c)
		}
		c.mu.Unlock()
	}
	s.mu.Unlock()
	for _, cs := range targets {
		if err := s.writeFrame(cs, h, []byte(text)); err != nil {
			s.logger.Debug("agw monitor write failed", "err", err)
		}
	}
}
