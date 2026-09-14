package agw

import (
	"context"
	"fmt"
	"strings"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/ax25conn"
)

func (s *Server) sessionKey(port uint8, from, to string) string {
	return fmt.Sprintf("%d:%s:%s", port, from, to)
}

func (s *Server) handleConnect(ctx context.Context, cs *clientState, h *Header, data []byte) error {
	src, err := ax25.ParseAddress(h.CallFrom)
	if err != nil {
		s.logger.Warn("agw connect invalid from", "call", h.CallFrom, "err", err)
		return nil
	}
	dst, err := ax25.ParseAddress(h.CallTo)
	if err != nil {
		s.logger.Warn("agw connect invalid to", "call", h.CallTo, "err", err)
		return nil
	}
	var path []ax25.Address
	if h.DataKind == KindConnectVia {
		viaStr := string(data)
		for _, v := range strings.Split(viaStr, " ") {
			if v == "" {
				continue
			}
			a, err := ax25.ParseAddress(v)
			if err != nil {
				s.logger.Warn("agw via parse", "addr", v, "err", err)
				continue
			}
			path = append(path, a)
		}
	}

	channel := s.channelFor(h.Port)
	if s.cfg.AX25Manager == nil {
		s.logger.Warn("agw connect dropped; no AX25Manager configured")
		return nil
	}

	scfg := ax25conn.SessionConfig{
		Local:    src,
		Peer:     dst,
		Path:     path,
		Channel:  channel,
		Logger:   s.logger.With("call_from", h.CallFrom, "call_to", h.CallTo),
		Observer: s.makeObserver(cs, h.Port, h.CallFrom, h.CallTo),
	}

	key := s.sessionKey(h.Port, h.CallFrom, h.CallTo)
	cs.mu.Lock()
	if _, exists := cs.sessions[key]; exists {
		cs.mu.Unlock()
		s.logger.Warn("agw connect dropped; session already exists", "key", key)
		return nil
	}
	_, sess, err := s.cfg.AX25Manager.Open(scfg, "agw")
	if err != nil {
		cs.mu.Unlock()
		s.logger.Warn("agw manager open failed", "err", err)
		return nil
	}
	cs.sessions[key] = sess
	cs.mu.Unlock()

	sess.Submit(ax25conn.Event{Kind: ax25conn.EventConnect})
	return nil
}

func (s *Server) handleDisconnect(cs *clientState, h *Header) error {
	key := s.sessionKey(h.Port, h.CallFrom, h.CallTo)
	cs.mu.Lock()
	sess, ok := cs.sessions[key]
	cs.mu.Unlock()
	if ok {
		sess.Submit(ax25conn.Event{Kind: ax25conn.EventDisconnect})
	}
	return nil
}

func (s *Server) handleConnectedData(cs *clientState, h *Header, data []byte) error {
	key := s.sessionKey(h.Port, h.CallFrom, h.CallTo)
	cs.mu.Lock()
	sess, ok := cs.sessions[key]
	cs.mu.Unlock()
	if ok {
		sess.Submit(ax25conn.Event{Kind: ax25conn.EventDataTX, Data: data})
	}
	return nil
}

func (s *Server) makeObserver(cs *clientState, port uint8, from, to string) func(ax25conn.OutEvent) {
	return func(ev ax25conn.OutEvent) {
		switch ev.Kind {
		case ax25conn.OutStateChange:
			if ev.State == ax25conn.StateConnected {
				// Send C to client
				payload := []byte(fmt.Sprintf("*** CONNECTED With %s\r\n", to))
				_ = s.writeFrame(cs, &Header{
					Port:     port,
					DataKind: KindConnect,
					PID:      ax25.PIDNoLayer3,
					CallFrom: to,
					CallTo:   from,
				}, payload)
			} else if ev.State == ax25conn.StateDisconnected {
				// Send d to client
				payload := []byte("*** DISCONNECTED\r\n")
				_ = s.writeFrame(cs, &Header{
					Port:     port,
					DataKind: KindDisconnect,
					PID:      ax25.PIDNoLayer3,
					CallFrom: to,
					CallTo:   from,
				}, payload)

				// Remove session from map
				key := s.sessionKey(port, from, to)
				cs.mu.Lock()
				delete(cs.sessions, key)
				cs.mu.Unlock()
			}
		case ax25conn.OutDataRX:
			_ = s.writeFrame(cs, &Header{
				Port:     port,
				DataKind: KindConnectedData,
				PID:      ax25.PIDNoLayer3,
				CallFrom: to,
				CallTo:   from,
			}, ev.Data)
		case ax25conn.OutError:
			// Just log
			s.logger.Warn("agw session err", "code", ev.ErrCode, "msg", ev.ErrMsg)
		}
	}
}
