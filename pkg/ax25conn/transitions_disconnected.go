package ax25conn

import (
	"context"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// onDisconnected is the state-0 handler. The kernel has no input
// handler for state 0; ax25_rcv() decides directly
// (net/ax25/ax25_in.c:317-427).
func (s *Session) onDisconnected(_ context.Context, ev Event) bool {
	switch ev.Kind {
	case EventConnect:
		// Equivalent of ax25_std_establish_data_link
		// (ax25_std_subr.c:35-50): zero condition, n2count=0, TX
		// SABM(P=1, cmd), start T1, start heartbeat, seed RTT.
		s.v = vars{}
		s.mutateStats(func(st *LinkStats) { *st = LinkStats{} })
		s.sendSABM(s.cfg.Mod128)
		s.resetT1()
		s.hb.reset()
		s.setState(StateAwaitingConnection)
		return true
	case EventFrameRX:
		// Outbound-only client (brainstorm Q4). Reject inbound SABM/SABME
		// with DM(F=p, rsp). Silently drop DM (kernel "Never reply to a
		// DM" — ax25_in.c:323-326). Drop everything else silently.
		if ev.Frame == nil {
			return true
		}
		switch ev.Frame.Control.Kind {
		case FrameSABM, FrameSABME:
			if ev.Frame.IsCommand {
				s.sendDM(ev.Frame.Control.PF)
			}
		}
		return true
	case EventShutdown:
		return false
	}
	return true
}

// sendSABM emits SABM (mod-8) or SABME (mod-128) with P=1, cmd. The
// kernel chooses by ax25->modulus in ax25_std_subr.c:35-50. We accept
// the modulus arg explicitly so the state-1 N2-exhaustion fall-back
// (ax25_std_timer.c:128-133) can downgrade in-place.
func (s *Session) sendSABM(mod128 bool) {
	kind := FrameSABM
	if mod128 {
		kind = FrameSABME
	}
	f := &Frame{
		Source: s.cfg.Local, Dest: s.cfg.Peer, Path: s.cfg.Path,
		Control:   Control{Kind: kind, PF: true},
		IsCommand: true, Mod128: false, // wire control byte is 1 byte for SABM/SABME (the SABME is just a U-frame)
	}
	s.submit(f)
}

func (s *Session) sendDM(pf bool) {
	f := &Frame{
		Source: s.cfg.Local, Dest: s.cfg.Peer, Path: s.cfg.Path,
		Control:   Control{Kind: FrameDM, PF: pf},
		IsCommand: false,
	}
	s.submit(f)
}

func (s *Session) sendDISC() {
	f := &Frame{
		Source: s.cfg.Local, Dest: s.cfg.Peer, Path: s.cfg.Path,
		Control:   Control{Kind: FrameDISC, PF: true},
		IsCommand: true,
	}
	s.submit(f)
}

func (s *Session) sendUA(pf bool) {
	f := &Frame{
		Source: s.cfg.Local, Dest: s.cfg.Peer, Path: s.cfg.Path,
		Control:   Control{Kind: FrameUA, PF: pf},
		IsCommand: false,
	}
	s.submit(f)
}

// sendRROrRNR emits RR (or RNR if CondOwnRxBusy) with the current N(R).
// asResponse selects response polarity (rsp); poll selects the P/F bit.
func (s *Session) sendRROrRNR(poll, asResponse bool) {
	kind := FrameRR
	if s.v.Cond.Has(CondOwnRxBusy) {
		kind = FrameRNR
	}
	f := &Frame{
		Source: s.cfg.Local, Dest: s.cfg.Peer, Path: s.cfg.Path,
		Control:   Control{Kind: kind, NR: s.v.VR % uint8(s.modulus()), PF: poll},
		IsCommand: !asResponse,
	}
	s.submit(f)
}

// submit hands a connected-mode frame to the TX governor with our
// priority. SkipDedup is always true: identical bytes legitimately
// retransmit on T1 expiry, and the governor's payload-keyed dedup
// would collapse RR/RNR/REJ frames that share a (dest, source) but
// differ in N(R).
func (s *Session) submit(f *Frame) {
	wrapper, err := f.ToAX25Frame()
	if err != nil {
		s.cfg.Logger.Warn("ax25conn: encode failed", "err", err, "kind", f.Control.Kind)
		return
	}
	src := txgovernor.SubmitSource{
		Kind:      "ax25conn",
		Detail:    f.Control.Kind.String(),
		Priority:  ax25.PriorityAX25Conn,
		SkipDedup: true,
	}
	if err := s.cfg.TxSink.Submit(context.Background(), s.cfg.Channel, wrapper, src); err != nil {
		s.cfg.Logger.Warn("ax25conn: tx submit failed", "err", err)
		return
	}
	s.mutateStats(func(st *LinkStats) {
		st.FramesTX++
		if f.hasInfo() {
			st.BytesTX += uint64(len(f.Info))
		}
	})
}
