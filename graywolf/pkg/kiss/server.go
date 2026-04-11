package kiss

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// ServerConfig configures a KISS TCP server instance.
type ServerConfig struct {
	// Name identifies the interface in logs and metrics.
	Name string
	// ListenAddr is a "host:port" TCP address. For serial/bluetooth use a
	// different constructor (ServeTransport).
	ListenAddr string
	// ChannelMap translates KISS port numbers (0..15) to graywolf radio
	// channels. A missing entry defaults to channel 1.
	ChannelMap map[uint8]uint32
	// Sink receives parsed AX.25 frames for transmission. Typically
	// *txgovernor.Governor in production.
	Sink txgovernor.TxSink
	// Logger is optional.
	Logger *slog.Logger
	// OnClientChange is invoked with the new active-client count whenever
	// a client connects or disconnects. Optional.
	OnClientChange func(active int)
	// OnDecodeError is invoked for every KISS data frame whose payload
	// failed AX.25 decoding. Optional; nil is a no-op. A single counter
	// with no labels is used on purpose: per-client address would
	// explode cardinality on a server with churning clients.
	OnDecodeError func()
	// Broadcast, when false, disables BroadcastFromChannel fan-out (the
	// interface is TX-only from the KISS client's perspective). Default
	// true — kiss_interfaces.broadcast in the configstore drives this.
	Broadcast bool
}

// Server is a multi-client KISS TCP server. A single Server instance
// corresponds to one row in the kiss_interfaces table.
type Server struct {
	cfg     ServerConfig
	logger  *slog.Logger
	ln      net.Listener
	wg      sync.WaitGroup
	mu      sync.Mutex
	clients map[*clientConn]struct{}
	active  int32 // atomic: current client count
}

type clientConn struct {
	w  io.Writer
	mu sync.Mutex // serialises writes to the same client
}

// NewServer builds a Server. It does not start listening until ListenAndServe.
func NewServer(cfg ServerConfig) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Server{
		cfg:     cfg,
		logger:  cfg.Logger.With("kiss_iface", cfg.Name),
		clients: make(map[*clientConn]struct{}),
	}
}

// ActiveClients returns the current number of connected KISS clients.
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

// ListenAndServe binds the configured TCP address and serves clients until
// the context is cancelled. It blocks. When it returns, the listener is
// closed and the bound port is free — callers may immediately rebind.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	s.logger.Info("kiss server listening", "addr", ln.Addr().String())

	// Close the listener on context cancel to break Accept. Tracked in
	// s.wg so ListenAndServe cannot return until this goroutine has
	// actually finished closing the listener — otherwise a rapid
	// Stop/Start could race the old close against the new bind. A local
	// done channel lets ListenAndServe unblock the watcher if it exits
	// for any reason other than ctx cancellation.
	localDone := make(chan struct{})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		select {
		case <-ctx.Done():
		case <-localDone:
		}
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			if errors.Is(err, net.ErrClosed) {
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
	close(localDone)
	s.wg.Wait()
	return nil
}

func (s *Server) handleClient(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	addr := conn.RemoteAddr().String()
	c := &clientConn{w: conn}
	s.addClient(c)
	defer s.removeClient(c)
	s.logger.Info("kiss client connected", "remote", addr)
	defer s.logger.Info("kiss client disconnected", "remote", addr)

	// Close the connection if the context is cancelled so the decoder
	// unblocks. Tracked in s.wg so ListenAndServe's final Wait cannot
	// return until this watcher has observed done and exited.
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

	d := NewDecoder(conn)
	for {
		f, err := d.Next()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			s.logger.Warn("kiss decode error", "remote", addr, "err", err)
			return
		}
		s.handleFrame(ctx, addr, f)
	}
}

func (s *Server) handleFrame(ctx context.Context, remote string, f *Frame) {
	switch f.Command {
	case CmdDataFrame:
		ax, err := ax25.Decode(f.Data)
		if err != nil {
			if s.cfg.OnDecodeError != nil {
				s.cfg.OnDecodeError()
			}
			s.logger.Warn("kiss frame is not valid ax.25",
				"remote", remote, "len", len(f.Data), "err", err)
			return
		}
		if !ax.IsUI() {
			s.logger.Debug("dropping non-UI frame from kiss client", "remote", remote)
			return
		}
		channel := s.channelFor(f.Port)
		if s.cfg.Sink != nil {
			err := s.cfg.Sink.Submit(ctx, channel, ax, txgovernor.SubmitSource{
				Kind:     "kiss",
				Detail:   s.cfg.Name + " " + remote,
				Priority: ax25.PriorityClient,
			})
			if err != nil {
				s.logger.Warn("tx governor rejected kiss frame", "err", err)
			}
		}
	case CmdTxDelay, CmdPersistence, CmdSlotTime, CmdTxTail, CmdFullDuplex, CmdSetHardware:
		// KISS timing parameters are configured via the web UI in graywolf;
		// accept and ignore to stay compatible with direwolf kissutil etc.
		s.logger.Debug("ignoring kiss timing command", "cmd", f.Command, "remote", remote)
	case CmdReturn:
		s.logger.Info("kiss return command received", "remote", remote)
	default:
		s.logger.Debug("unknown kiss command", "cmd", f.Command, "remote", remote)
	}
}

func (s *Server) channelFor(port uint8) uint32 {
	if ch, ok := s.cfg.ChannelMap[port]; ok {
		return ch
	}
	return 1
}

func (s *Server) addClient(c *clientConn) {
	s.mu.Lock()
	s.clients[c] = struct{}{}
	n := int32(len(s.clients))
	s.mu.Unlock()
	atomic.StoreInt32(&s.active, n)
	if s.cfg.OnClientChange != nil {
		s.cfg.OnClientChange(int(n))
	}
}

func (s *Server) removeClient(c *clientConn) {
	s.mu.Lock()
	delete(s.clients, c)
	n := int32(len(s.clients))
	s.mu.Unlock()
	atomic.StoreInt32(&s.active, n)
	if s.cfg.OnClientChange != nil {
		s.cfg.OnClientChange(int(n))
	}
}

// Broadcast sends a received AX.25 frame to every connected KISS client
// (KISSCOPY equivalent). Errors on individual clients are logged but do not
// stop the broadcast. Does not consult the Broadcast flag; callers that
// want per-interface honoring should use BroadcastFromChannel instead.
func (s *Server) Broadcast(port uint8, axBytes []byte) {
	raw := Encode(port, axBytes)
	s.mu.Lock()
	clients := make([]*clientConn, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()
	for _, c := range clients {
		c.mu.Lock()
		_, err := c.w.Write(raw)
		c.mu.Unlock()
		if err != nil {
			s.logger.Debug("kiss broadcast write failed", "err", err)
		}
	}
}

// BroadcastFromChannel honors the interface's Broadcast flag and the
// ChannelMap: the received frame is only forwarded if Broadcast is true
// and at least one mapped port exists for channel. The KISS port byte
// in the outgoing frame is the first port whose ChannelMap entry equals
// channel (falling back to 0 if the map is empty).
func (s *Server) BroadcastFromChannel(channel uint32, axBytes []byte) {
	if !s.cfg.Broadcast {
		return
	}
	port := uint8(0)
	found := false
	for p, ch := range s.cfg.ChannelMap {
		if ch == channel {
			port = p
			found = true
			break
		}
	}
	// If the interface has a ChannelMap but channel isn't in it, skip —
	// this interface doesn't serve that channel. An empty map is
	// interpreted as "default channel 1 on port 0" per channelFor().
	if !found && len(s.cfg.ChannelMap) > 0 {
		return
	}
	s.Broadcast(port, axBytes)
}

// ServeTransport runs a single-client KISS session over any
// io.ReadWriteCloser — e.g. a serial port opened via go.bug.st/serial, or
// a bluetooth rfcomm device opened via os.OpenFile. Used for
// kiss_interfaces.interface_type = "serial" | "bluetooth".
//
// The transport is closed on return or context cancellation.
func (s *Server) ServeTransport(ctx context.Context, rwc io.ReadWriteCloser) error {
	c := &clientConn{w: rwc}
	s.addClient(c)
	defer s.removeClient(c)
	// Close the transport on ctx cancel so the decoder unblocks. Tracked
	// in s.wg so callers waiting on server shutdown can observe this
	// goroutine has exited.
	done := make(chan struct{})
	defer close(done)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		select {
		case <-ctx.Done():
			_ = rwc.Close()
		case <-done:
		}
	}()
	d := NewDecoder(rwc)
	for {
		f, err := d.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		s.handleFrame(ctx, "transport:"+s.cfg.Name, f)
	}
}

