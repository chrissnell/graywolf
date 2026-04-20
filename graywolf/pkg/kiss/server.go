package kiss

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	"github.com/chrissnell/graywolf/pkg/ax25"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// tncIngressQueueCap bounds the per-interface queue that buffers decoded
// KISS-TNC frames between the socket reader and the drain goroutine
// forwarding into the shared RX fanout. Sized deliberately small: the
// rate limiter is the primary ingress cap; this queue is the backstop
// when the shared fanout consumer is momentarily slow.
const tncIngressQueueCap = 64

// Mode selects the per-interface routing policy for inbound KISS data
// frames. Values are stored lowercase and matched exactly by the
// configstore layer; see configstore.ValidKissMode.
type Mode string

const (
	// ModeModem is the default: the peer is an APRS app and inbound
	// frames are submitted to the TX governor for RF transmission.
	ModeModem Mode = "modem"
	// ModeTnc marks the peer as a hardware TNC supplying off-air RX.
	// Inbound frames are fanned out to digi/igate/messages/station cache
	// and never auto-submitted to TX. Phase 3 wires this branch; in
	// Phase 2 the field is stored and surfaced but both modes dispatch
	// through the existing TX path.
	ModeTnc Mode = "tnc"
)

// ServerConfig configures a KISS TCP server instance.
type ServerConfig struct {
	// InterfaceID is the KissInterface DB row ID. Used to tag inbound
	// frames with ingress.KissTnc(InterfaceID) when Mode == ModeTnc and
	// to suppress self-echo on the broadcast fanout. Zero is acceptable
	// for unit tests that don't exercise the RX fanout; production
	// startup and hot reload both populate it.
	InterfaceID uint32
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
	// OnFrameIngress is invoked for every KISS data frame that
	// successfully AX.25-decodes, in both Modem and TNC modes, before
	// dispatch. Observation hook; nil is a no-op. The Mode argument
	// mirrors the server's configured routing so the subscriber can
	// label a single counter covering both modes.
	OnFrameIngress func(mode Mode)
	// Broadcast, when false, disables BroadcastFromChannel fan-out (the
	// interface is TX-only from the KISS client's perspective). Default
	// true — kiss_interfaces.broadcast in the configstore drives this.
	Broadcast bool
	// Mode selects the inbound-frame routing policy. Empty is treated as
	// ModeModem for backwards compatibility with callers that predate
	// the field.
	Mode Mode
	// TncIngressRateHz and TncIngressBurst configure the per-interface
	// token-bucket ingress cap applied in ModeTnc. Zero (either field)
	// disables rate limiting — every frame is allowed.
	TncIngressRateHz uint32
	TncIngressBurst  uint32
	// RxIngress, when non-nil and Mode == ModeTnc, receives every inbound
	// KISS data frame that survives the rate limiter and the per-interface
	// queue. Phase 3 wires this to the shared modem-RX fanout in
	// pkg/app/wiring.go. nil in ModeModem — that path submits to Sink
	// instead, preserving byte-for-byte existing behavior.
	RxIngress func(rf *pb.ReceivedFrame, src ingress.Source)
	// Clock is the rate-limiter's time source. nil selects wall time.
	// Tests inject a fake clock to exercise burst/refill determinism.
	Clock Clock
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

	// rateLimiter gates ModeTnc ingress. nil in ModeModem and in tests
	// that construct a server without a Mode; see NewServer.
	rateLimiter *RateLimiter
	// ingressQ buffers rate-limited frames between the socket reader and
	// the drain goroutine that calls RxIngress. Allocated only when
	// Mode == ModeTnc and RxIngress is non-nil; nil otherwise. A
	// non-blocking send into a nil channel blocks forever, which is why
	// the handleFrame path checks cfg.Mode before touching it.
	ingressQ chan *pb.ReceivedFrame
	// queueOverflow counts frames dropped at ingressQ because the drain
	// goroutine was behind. Distinct from rateLimiter.Dropped (which
	// counts upstream drops by the token bucket) so operators can
	// distinguish a stuck TNC from a stuck consumer downstream.
	queueOverflow atomic.Uint64
	// drainOnce ensures the ingress drain goroutine is started at most
	// once per server lifetime across ListenAndServe and ServeTransport.
	drainOnce sync.Once
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
	s := &Server{
		cfg:     cfg,
		logger:  cfg.Logger.With("kiss_iface", cfg.Name),
		clients: make(map[*clientConn]struct{}),
		rateLimiter: NewRateLimiter(
			cfg.TncIngressRateHz,
			cfg.TncIngressBurst,
			cfg.Clock,
		),
	}
	if cfg.Mode == ModeTnc && cfg.RxIngress != nil {
		s.ingressQ = make(chan *pb.ReceivedFrame, tncIngressQueueCap)
	}
	return s
}

// Dropped returns the number of ModeTnc inbound frames rejected by this
// server's rate limiter since construction. Zero when the limiter is in
// unlimited mode (rate or burst == 0) or in ModeModem.
func (s *Server) Dropped() uint64 {
	if s.rateLimiter == nil {
		return 0
	}
	return s.rateLimiter.Dropped()
}

// QueueOverflow returns the number of ModeTnc frames that passed the
// rate limiter but were dropped because the per-interface ingress queue
// was full when the frame arrived. Non-zero indicates the downstream
// fanout consumer is slower than sustained ingest on this interface.
func (s *Server) QueueOverflow() uint64 { return s.queueOverflow.Load() }

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

	s.startIngressDrain(ctx)

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
		mode := s.cfg.Mode
		if mode == "" {
			mode = ModeModem
		}
		if s.cfg.OnFrameIngress != nil {
			s.cfg.OnFrameIngress(mode)
		}
		s.dispatchDataFrame(ctx, remote, channel, ax, f.Data)
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

// startIngressDrain launches the drain goroutine once per Server. The
// goroutine forwards rate-limited ModeTnc frames to cfg.RxIngress until
// ctx is cancelled; tracked in s.wg so callers can observe shutdown.
// No-op when the server isn't configured for ModeTnc ingress.
func (s *Server) startIngressDrain(ctx context.Context) {
	if s.ingressQ == nil || s.cfg.RxIngress == nil {
		return
	}
	src := ingress.KissTnc(s.cfg.InterfaceID)
	s.drainOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case rf := <-s.ingressQ:
					s.cfg.RxIngress(rf, src)
				}
			}
		}()
	})
}

// dispatchDataFrame routes a decoded KISS data frame per the configured
// Mode. ModeModem submits to the TX governor (existing behavior,
// byte-for-byte identical to pre-Phase-3). ModeTnc runs the rate limiter,
// then non-blocking-enqueues onto the per-interface queue whose drain
// goroutine invokes RxIngress — the D2 loop guarantee is structural:
// there is no code path from this branch to Sink.Submit.
func (s *Server) dispatchDataFrame(ctx context.Context, remote string, channel uint32, ax *ax25.Frame, rawAX []byte) {
	mode := s.cfg.Mode
	if mode == "" {
		mode = ModeModem
	}
	switch mode {
	case ModeTnc:
		if s.cfg.RxIngress == nil || s.ingressQ == nil {
			// Misconfigured — TNC mode with no RX ingress means the frame
			// has nowhere to go. Drop loudly so operators notice.
			s.logger.Warn("kiss tnc-mode frame dropped: no RxIngress wired",
				"remote", remote, "channel", channel)
			return
		}
		if !s.rateLimiter.Allow() {
			return
		}
		rf := &pb.ReceivedFrame{Channel: channel, Data: rawAX}
		select {
		case s.ingressQ <- rf:
		default:
			s.queueOverflow.Add(1)
			s.logger.Debug("kiss tnc ingress queue full; dropping",
				"interface_id", s.cfg.InterfaceID, "channel", channel)
		}
	default:
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
	s.startIngressDrain(ctx)
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
