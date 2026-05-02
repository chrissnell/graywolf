package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/chrissnell/graywolf/pkg/ax25termws"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webauth"
	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// maxRecentAX25Profiles caps the unpinned recents list. Pinned
// profiles survive the trim. Plan §3d.2.
const maxRecentAX25Profiles = 20

const (
	// terminalPingInterval is the keepalive ping cadence. Must be
	// shorter than terminalIdleTimeout so an idle peer is detected.
	terminalPingInterval = 30 * time.Second
	// terminalIdleTimeout bounds how long a single Ping round-trip
	// may take before the bridge declares the link dead and tears
	// down the WebSocket. Browsers and most middle boxes drop idle
	// WebSockets after ~120s, so 90s gives the ping a generous
	// detection window without going over that limit.
	terminalIdleTimeout = 90 * time.Second
	// terminalReadLimit caps each inbound JSON envelope. Operator
	// keystrokes are tiny; 64 KiB is far above any realistic packet.
	terminalReadLimit int64 = 64 * 1024
	// terminalOutBuf is the buffered envelope queue between bridge
	// observe callbacks and the writer goroutine. 32 absorbs typical
	// state/stats bursts without back-pressuring the session goroutine
	// for control envelopes; KindDataRX uses a blocking send and is
	// the only kind that intentionally exerts back-pressure.
	terminalOutBuf = 32
)

func (s *Server) registerAX25Terminal(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ax25/terminal", s.handleAX25Terminal)
}

// handleAX25Terminal upgrades the request to a WebSocket and runs the
// envelope read/write loops for one session bridge until the operator
// disconnects, the link goes idle, or the request context is cancelled.
//
// The endpoint is mounted under webauth.RequireAuth in production
// wiring (pkg/app/wiring.go); the explicit nil check here is defense
// in depth so this handler also rejects an unauthenticated request if
// it somehow reaches the inner mux directly (e.g. test harnesses that
// skip RequireAuth, or a future wiring refactor).
func (s *Server) handleAX25Terminal(w http.ResponseWriter, r *http.Request) {
	user := webauth.AuthenticatedUser(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, webtypes.ErrorResponse{Error: "authentication required"})
		return
	}
	if s.ax25Mgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: "ax25 manager not initialized"})
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// OriginPatterns left empty -> coder/websocket enforces
		// strict same-origin (Host header equality). The terminal
		// is only intended to be opened from the graywolf SPA
		// served by this same process; no cross-origin embed.
	})
	if err != nil {
		// Accept already wrote a response on failure.
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(terminalReadLimit)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	out := make(chan ax25termws.Envelope, terminalOutBuf)
	bridge := ax25termws.New(ax25termws.BridgeConfig{
		Manager:          s.ax25Mgr,
		Logger:           s.logger.With("component", "ax25termws", "user", user.Username),
		Operator:         user.Username,
		Ctx:              ctx,
		Out:              out,
		OnFirstConnected: s.recordRecentAX25Connection,
		Transcripts:      transcriptRecorder{store: s.store},
	})

	// Writer + ping live in the same goroutine so we never have two
	// goroutines writing the WebSocket concurrently. The reader runs
	// inline below; on read error or ctx done both sides unwind.
	writerDone := make(chan struct{})
	go s.runTerminalWriter(ctx, c, out, writerDone, user.Username)

	s.runTerminalReader(ctx, c, bridge, user.Username)

	// Bridge.Close submits EventDisconnect on the active session
	// before we tear down the writer + WebSocket so the LAPB peer
	// sees a clean DISC frame on the wire instead of waiting for N2
	// retries to time out. Critical for radio neighbours: every
	// browser tab close, OS-level WS reaper kill, or mobile suspend
	// would otherwise leak a ghost link.
	bridge.Close()
	cancel()
	<-writerDone
	_ = c.Close(websocket.StatusNormalClosure, "session ended")
}

func (s *Server) runTerminalReader(ctx context.Context, c *websocket.Conn, bridge *ax25termws.Bridge, user string) {
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			if !isExpectedClose(err) {
				s.logger.Debug("ax25termws: read loop ended",
					"user", user, "err", err)
			}
			return
		}
		var env ax25termws.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			s.logger.Warn("ax25termws: bad envelope JSON", "user", user, "err", err)
			continue
		}
		if err := bridge.Handle(ctx, env); err != nil {
			s.logger.Warn("ax25termws: bridge handle failed",
				"user", user, "kind", env.Kind, "err", err)
		}
	}
}

// runTerminalWriter is the sole sender on the WebSocket. It also
// owns the keepalive ping ticker so we never have two goroutines
// writing the connection concurrently. The `out` channel is owned
// by the bridge; we only read from it. Critically we DO NOT close
// `out` here -- the bridge's pump goroutine is the single sender,
// and closing under it would race observer events that may still be
// in flight when the WS tears down. Bridge.Close + ctx cancel are
// the bridge's only synchronization points.
func (s *Server) runTerminalWriter(ctx context.Context, c *websocket.Conn, out <-chan ax25termws.Envelope, done chan<- struct{}, user string) {
	defer close(done)
	ticker := time.NewTicker(terminalPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-out:
			if !ok {
				return
			}
			payload, err := json.Marshal(env)
			if err != nil {
				s.logger.Warn("ax25termws: encode envelope failed",
					"user", user, "kind", env.Kind, "err", err)
				continue
			}
			wctx, cancel := context.WithTimeout(ctx, terminalIdleTimeout)
			err = c.Write(wctx, websocket.MessageText, payload)
			cancel()
			if err != nil {
				s.logger.Debug("ax25termws: write failed; closing",
					"user", user, "err", err)
				return
			}
		case <-ticker.C:
			pctx, cancel := context.WithTimeout(ctx, terminalIdleTimeout)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				s.logger.Debug("ax25termws: ping failed; closing",
					"user", user, "err", err)
				return
			}
		}
	}
}

// transcriptRecorder adapts the configstore to the ax25termws bridge's
// TranscriptRecorder interface. Plan §3e.2.
type transcriptRecorder struct {
	store *configstore.Store
}

func (t transcriptRecorder) Begin(ctx context.Context, channelID uint32, peerCall string, peerSSID uint8, viaPath string) (uint32, error) {
	sess := &configstore.AX25TranscriptSession{
		ChannelID: channelID,
		PeerCall:  strings.ToUpper(strings.TrimSpace(peerCall)),
		PeerSSID:  peerSSID,
		ViaPath:   viaPath,
	}
	if err := t.store.CreateAX25TranscriptSession(ctx, sess); err != nil {
		return 0, err
	}
	return sess.ID, nil
}

func (t transcriptRecorder) Append(ctx context.Context, sessionID uint32, ts time.Time, direction, kind string, payload []byte) error {
	return t.store.AppendAX25TranscriptEntry(ctx, &configstore.AX25TranscriptEntry{
		SessionID: sessionID,
		TS:        ts,
		Direction: direction,
		Kind:      kind,
		Payload:   payload,
	})
}

func (t transcriptRecorder) End(ctx context.Context, sessionID uint32, reason string, bytes, frames uint64) error {
	return t.store.EndAX25TranscriptSession(ctx, sessionID, reason, bytes, frames)
}

// recordRecentAX25Connection upserts a recent AX25SessionProfile after
// a WebSocket bridge first reaches CONNECTED. Failures are logged and
// swallowed: a bookkeeping miss on the recents list is never worth
// killing a healthy LAPB session over. Plan §3d.2.
func (s *Server) recordRecentAX25Connection(args ax25termws.ConnectArgs) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	via := strings.Join(args.Via, ",")
	var channelID *uint32
	if args.ChannelID != 0 {
		v := args.ChannelID
		channelID = &v
	}
	row := configstore.AX25SessionProfile{
		LocalCall: strings.ToUpper(strings.TrimSpace(args.LocalCall)),
		LocalSSID: args.LocalSSID,
		DestCall:  strings.ToUpper(strings.TrimSpace(args.DestCall)),
		DestSSID:  args.DestSSID,
		ViaPath:   via,
		Mod128:    args.Mod128,
		Paclen:    uint32Of(args.Paclen),
		Maxframe:  uint32Of(args.Maxframe),
		T1MS:      uint32Of(args.T1MS),
		T2MS:      uint32Of(args.T2MS),
		T3MS:      uint32Of(args.T3MS),
		N2:        uint32Of(args.N2),
		ChannelID: channelID,
	}
	if err := s.store.UpsertRecentAX25SessionProfile(ctx, &row, maxRecentAX25Profiles); err != nil {
		s.logger.Warn("ax25 recents: upsert failed",
			"err", err,
			"peer", row.DestCall, "ssid", row.DestSSID,
		)
	}
}

func uint32Of(n int) uint32 {
	if n <= 0 {
		return 0
	}
	return uint32(n)
}

// isExpectedClose reports whether err signals a clean WebSocket
// shutdown so the read loop logs at debug instead of warning.
func isExpectedClose(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway:
		return true
	}
	return false
}
