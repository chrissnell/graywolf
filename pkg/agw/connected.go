package agw

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/ax25conn"
)

// sessionKey identifies one client's link. The callsigns must already be
// normalized -- see sessionKeyFor.
func (s *Server) sessionKey(port uint8, from, to string) string {
	return fmt.Sprintf("%d:%s:%s", port, from, to)
}

// sessionKeyFor builds the map key from a client frame, normalizing the
// callsigns through ax25.ParseAddress first. Clients are inconsistent
// about case and about spelling a zero SSID, so keying on the raw header
// bytes means a link opened as "W1AW-0" cannot be found by a later frame
// addressed to "w1aw" -- and its data would vanish. The manager already
// keys on the parsed form, so this keeps the two tables in agreement.
// Reports false if either callsign is unparseable.
func (s *Server) sessionKeyFor(h *Header) (string, bool) {
	src, err := ax25.ParseAddress(h.CallFrom)
	if err != nil {
		return "", false
	}
	dst, err := ax25.ParseAddress(h.CallTo)
	if err != nil {
		return "", false
	}
	return s.sessionKey(h.Port, src.String(), dst.String()), true
}

// normalizeCallsign renders call in the canonical form clientState.callsigns
// is keyed on, so registering "w1aw" and later connecting as "W1AW-0" still
// match. Falls back to the raw string when it doesn't parse as an AX.25
// address, so an unparseable registration is still tracked under its
// literal spelling rather than silently dropped.
func normalizeCallsign(call string) string {
	a, err := ax25.ParseAddress(call)
	if err != nil {
		return call
	}
	return a.String()
}

// refuseConnect tells the client its request failed instead of leaving
// it to hang. Report error to client by sending a 'd' (disconnect) frame
// carrying explanatory text.
//
// The text keeps the "*** DISCONNECTED" prefix that clients string-match
// on and appends the reason, which operators see in their terminal. The
// header mirrors makeObserver's disconnect frame: the remote station is
// CallFrom, the client's own callsign is CallTo.
func (s *Server) refuseConnect(cs *clientState, h *Header, reason string) error {
	s.logger.Warn("agw connect refused",
		"port", h.Port, "call_from", h.CallFrom, "call_to", h.CallTo, "reason", reason)
	return s.sendDisconnect(cs, h, reason)
}

// sendDisconnect is called from both refuseConnect and the no-session paths.
// Split out so each caller can log in its own reason while keeping the
// 'd' frame consistent.
func (s *Server) sendDisconnect(cs *clientState, h *Header, reason string) error {
	payload := []byte(fmt.Sprintf("*** DISCONNECTED With %s: %s\r\n", h.CallTo, reason))
	return s.writeFrame(cs, &Header{
		Port:     h.Port,
		DataKind: KindDisconnect,
		PID:      ax25.PIDNoLayer3,
		CallFrom: h.CallTo,
		CallTo:   h.CallFrom,
	}, payload)
}

// openFailureReason maps ax25conn.Manager.Open errors into a message for the
// operator.
func openFailureReason(err error, channel uint32) string {
	switch {
	case errors.Is(err, ax25conn.ErrChannelAPRSOnly):
		return fmt.Sprintf("channel %d is APRS-only; set its mode to packet or aprs+packet", channel)
	case errors.Is(err, ax25conn.ErrSessionExists):
		return "a link to this station is already open on this channel"
	case errors.Is(err, ax25conn.ErrMaxTotal):
		return "the station's connected-mode session limit is reached"
	case errors.Is(err, ax25conn.ErrMaxPerOperator):
		return "the AGWPE connected-mode session limit is reached"
	case errors.Is(err, ax25conn.ErrManagerClosed):
		return "the station is shutting down"
	default:
		return fmt.Sprintf("session setup failed: %v", err)
	}
}

func (s *Server) handleConnect(cs *clientState, h *Header, data []byte) error {
	src, err := ax25.ParseAddress(h.CallFrom)
	if err != nil {
		return s.refuseConnect(cs, h, fmt.Sprintf("invalid source callsign %q", h.CallFrom))
	}
	dst, err := ax25.ParseAddress(h.CallTo)
	if err != nil {
		return s.refuseConnect(cs, h, fmt.Sprintf("invalid destination callsign %q", h.CallTo))
	}

	// The AGWPE spec says CallFrom "must [have] been previously registered" with
	// an 'X' frame (https://www.on7lds.net/42/sites/default/files/AGWPEAPI.HTM),
	// but clients differ: Paracon registers the callsign it connects as, while
	// EasyTerm registers only its setup callsign. Refusing would pin EasyTerm to
	// that one callsign, and since 'X' is unauthenticated the check guards
	// nothing. In the spirit of Postel's Law ("Be conservative in what you send,
	// be liberal in what you accept"), accept and log the connection.
	var registeredCalls []string
	cs.mu.Lock()
	_, registered := cs.callsigns[normalizeCallsign(h.CallFrom)]
	if !registered {
		registeredCalls = slices.Sorted(maps.Keys(cs.callsigns))
	}
	cs.mu.Unlock()
	if !registered {
		s.logger.Warn("connection from unregistered callsign",
			"call_from", h.CallFrom, "call_to", h.CallTo, "registered", registeredCalls)
	}

	var path []ax25.Address
	if h.DataKind == KindConnectVia {
		// Per the AGWPE spec, 'v' carries the same via list as 'V': one
		// byte of digipeater count followed by that many 10-byte
		// NUL-padded callsigns. It is not a space-separated string.
		// 'v' has no trailing info field, so the remainder is ignored.
		via, _, err := parseViaPayload(data)
		if err != nil {
			return s.refuseConnect(cs, h, fmt.Sprintf("malformed digipeater list: %v", err))
		}
		for _, v := range via {
			a, err := ax25.ParseAddress(v)
			if err != nil {
				// Connecting direct instead would quietly route the link
				// somewhere the operator did not ask for.
				return s.refuseConnect(cs, h, fmt.Sprintf("invalid digipeater callsign %q", v))
			}
			path = append(path, a)
		}
	}

	channel := s.channelFor(h.Port)
	if s.cfg.AX25Manager == nil {
		return s.refuseConnect(cs, h, "connected mode is not available on this station")
	}

	// Key on the parsed callsigns, matching what the manager keys on. The
	// frames we send back still echo the client's own spelling.
	key := s.sessionKey(h.Port, src.String(), dst.String())

	scfg := ax25conn.SessionConfig{
		Local:    src,
		Peer:     dst,
		Path:     path,
		Channel:  channel,
		Logger:   s.logger.With("call_from", h.CallFrom, "call_to", h.CallTo),
		Observer: s.makeObserver(cs, h.Port, h.CallFrom, h.CallTo, key),
	}

	cs.mu.Lock()
	if _, exists := cs.sessions[key]; exists {
		cs.mu.Unlock()
		return s.refuseConnect(cs, h, "this client already has a link to that station")
	}
	// Manager.Open can also fail for reasons this client's own session
	// map cannot see -- it keys on (channel, local, peer) across every
	// client, so a second AGW client asking for the same link lands in
	// ErrSessionExists here rather than the duplicate check above.
	_, sess, err := s.cfg.AX25Manager.Open(scfg, "agw")
	if err != nil {
		cs.mu.Unlock()
		return s.refuseConnect(cs, h, openFailureReason(err, channel))
	}
	cs.sessions[key] = sess
	cs.mu.Unlock()

	sess.Submit(ax25conn.Event{Kind: ax25conn.EventConnect})
	return nil
}

// sessionFor looks up the live session a client frame refers to.
func (s *Server) sessionFor(cs *clientState, h *Header) (*ax25conn.Session, bool) {
	key, ok := s.sessionKeyFor(h)
	if !ok {
		return nil, false
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	sess, found := cs.sessions[key]
	return sess, found
}

func (s *Server) handleDisconnect(cs *clientState, h *Header) error {
	sess, ok := s.sessionFor(cs, h)
	if !ok {
		// Nothing to tear down, but the client believes a link exists. Send a
		// disconnect frame to the client to prevent the client from having to
		// timeout waiting for an expected disconnect that never arrives.
		s.logger.Debug("agw disconnect for unknown session",
			"call_from", h.CallFrom, "call_to", h.CallTo)
		return s.sendDisconnect(cs, h, "no such link")
	}
	sess.Submit(ax25conn.Event{Kind: ax25conn.EventDisconnect})
	return nil
}

func (s *Server) handleConnectedData(cs *clientState, h *Header, data []byte) error {
	sess, ok := s.sessionFor(cs, h)
	if !ok {
		// Dropping the payload silently looks to the operator like the
		// remote end swallowed their text. Tell the client the link is
		// gone so it can stop sending.
		s.logger.Warn("agw data for unknown session",
			"call_from", h.CallFrom, "call_to", h.CallTo, "len", len(data))
		return s.sendDisconnect(cs, h, "no such link")
	}
	sess.Submit(ax25conn.Event{Kind: ax25conn.EventDataTX, Data: data})
	return nil
}

// makeObserver bridges session events back to the AGW client. from and
// to are the callsigns exactly as the client spelled them, echoed on
// every frame we write so the client can match them against its own
// request; key is the normalized map key used to retire the session.
func (s *Server) makeObserver(cs *clientState, port uint8, from, to, key string) func(ax25conn.OutEvent) {
	return func(ev ax25conn.OutEvent) {
		switch ev.Kind {
		case ax25conn.OutStateChange:
			if ev.State == ax25conn.StateConnected {
				// Send C to client
				payload := []byte(fmt.Sprintf("*** CONNECTED With %s\r\n", to))
				if err := s.writeFrame(cs, &Header{
					Port:     port,
					DataKind: KindConnect,
					PID:      ax25.PIDNoLayer3,
					CallFrom: to,
					CallTo:   from,
				}, payload); err != nil {
					s.logger.Debug("agw connect notice write failed", "err", err)
				}
			} else if ev.State == ax25conn.StateDisconnected {
				// Send d to client
				payload := []byte("*** DISCONNECTED\r\n")
				if err := s.writeFrame(cs, &Header{
					Port:     port,
					DataKind: KindDisconnect,
					PID:      ax25.PIDNoLayer3,
					CallFrom: to,
					CallTo:   from,
				}, payload); err != nil {
					s.logger.Debug("agw disconnect notice write failed", "err", err)
				}

				// Remove session from map
				cs.mu.Lock()
				delete(cs.sessions, key)
				cs.mu.Unlock()
			}
		case ax25conn.OutDataRX:
			if err := s.writeFrame(cs, &Header{
				Port:     port,
				DataKind: KindConnectedData,
				PID:      ax25.PIDNoLayer3,
				CallFrom: to,
				CallTo:   from,
			}, ev.Data); err != nil {
				s.logger.Debug("agw connected data write failed", "err", err)
			}
		case ax25conn.OutError:
			// Just log
			s.logger.Warn("agw session err", "code", ev.ErrCode, "msg", ev.ErrMsg)
		}
	}
}
