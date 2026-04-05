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

	"github.com/chrissnell/graywolf/pkg/ax25"
)

// TxSink is the consumer for AGW-originated transmit requests. Normally
// the graywolf txgovernor.
type TxSink interface {
	Submit(ctx context.Context, channel uint32, frame *ax25.Frame, source SubmitSource) error
}

// SubmitSource mirrors the kiss package's type so txgovernor can treat
// KISS and AGW symmetrically. Duplicated here (not re-exported) to avoid
// an import cycle between transport packages.
type SubmitSource struct {
	Kind     string
	Detail   string
	Priority int
}

// ServerConfig configures the AGW TCP server.
type ServerConfig struct {
	ListenAddr string
	// PortCallsigns lists the mycall of each radio port, in AGWPE port
	// order (index 0 = port 0). Used in the 'G' response.
	PortCallsigns []string
	// PortToChannel maps an AGW port number to a graywolf channel. If a
	// port isn't listed it defaults to PortToChannel[0] or channel 1.
	PortToChannel map[uint8]uint32
	// Sink receives parsed AX.25 frames for transmission.
	Sink TxSink
	// Logger is optional.
	Logger *slog.Logger
	// OnClientChange is invoked with the new total-client count on connect
	// and disconnect. Optional.
	OnClientChange func(active int)
}

// Server is a multi-client AGWPE-compatible TCP server.
type Server struct {
	cfg     ServerConfig
	logger  *slog.Logger
	mu      sync.Mutex
	clients map[*clientState]struct{}
	active  int32
}

type clientState struct {
	conn      net.Conn
	writeMu   sync.Mutex
	mu        sync.Mutex
	monitor   bool
	callsigns map[string]struct{}
}

// NewServer builds an AGW server. Does not listen until ListenAndServe.
func NewServer(cfg ServerConfig) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Server{
		cfg:     cfg,
		logger:  cfg.Logger.With("component", "agw"),
		clients: make(map[*clientState]struct{}),
	}
}

// ActiveClients returns the current client count.
func (s *Server) ActiveClients() int { return int(atomic.LoadInt32(&s.active)) }

// ListenAndServe binds and serves until ctx is cancelled. Blocks.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	s.logger.Info("agw server listening", "addr", s.cfg.ListenAddr)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	var wg sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				break
			}
			s.logger.Warn("accept error", "err", err)
			continue
		}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			s.handleClient(ctx, c)
		}(conn)
	}
	wg.Wait()
	return nil
}

func (s *Server) handleClient(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	cs := &clientState{conn: conn, callsigns: make(map[string]struct{})}
	s.addClient(cs)
	defer s.removeClient(cs)
	remote := conn.RemoteAddr().String()
	s.logger.Info("agw client connected", "remote", remote)
	defer s.logger.Info("agw client disconnected", "remote", remote)

	done := make(chan struct{})
	defer close(done)
	go func() {
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
		// data layout (direwolf): first byte PID is in header.PID; data is
		// the info field. CallFrom → CallTo, via optional digipeater path
		// carried inside data as "VIA CALL,CALL,..." prefix? In practice
		// APRSIS32 and UI-View put the info field directly in data and use
		// CallFrom/CallTo as source/dest, with the path either empty or
		// provided via a preceding 'V' message. Graywolf implements the
		// common simple path: treat data as info field, no digipeater path.
		src, err := ax25.ParseAddress(h.CallFrom)
		if err != nil {
			return nil // ignore invalid
		}
		dst, err := ax25.ParseAddress(h.CallTo)
		if err != nil {
			return nil
		}
		f, err := ax25.NewUIFrame(src, dst, nil, data)
		if err != nil {
			return nil
		}
		if s.cfg.Sink != nil {
			return s.cfg.Sink.Submit(ctx, s.channelFor(h.Port), f, SubmitSource{
				Kind:     "agw",
				Detail:   cs.conn.RemoteAddr().String(),
				Priority: 2,
			})
		}
		return nil

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
			if ax, err = ax25.Decode(data[1:]); err != nil {
				return nil
			}
		}
		if !ax.IsUI() {
			s.logger.Debug("ignoring non-UI agw raw frame")
			return nil
		}
		if s.cfg.Sink != nil {
			return s.cfg.Sink.Submit(ctx, s.channelFor(h.Port), ax, SubmitSource{
				Kind:     "agw",
				Detail:   cs.conn.RemoteAddr().String(),
				Priority: 2,
			})
		}
		return nil

	default:
		// Connected-mode frames: 'C', 'D', 'd', 'v', 'V', 'c' etc. Log and drop.
		s.logger.Debug("unsupported agw frame kind", "kind", string(h.DataKind))
		return nil
	}
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
