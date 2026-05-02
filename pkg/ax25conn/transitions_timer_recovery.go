package ax25conn

import "context"

// onTimerRecovery — state 4. Behavior matches ax25_std_state4_machine
// in net/ax25/ax25_std_in.c:266-414 and ax25_std_t1timer_expiry in
// ax25_std_timer.c:155-165 of Linux v6.12. Cheat sheet §1 state 4.
//
// We enter state 4 from state 3 on T1 or T3 expiry; we leave back to
// state 3 only when an enquiry response confirms we have caught up
// (vs == va) — anything ack'd that does not fully drain stays in state
// 4 so kick re-fires T1 for the remaining outstanding frames.
func (s *Session) onTimerRecovery(_ context.Context, ev Event) bool {
	switch ev.Kind {
	case EventFrameRX:
		f := ev.Frame
		if f == nil {
			break
		}
		switch f.Control.Kind {
		case FrameSABM, FrameSABME:
			if !f.IsCommand {
				break
			}
			s.cfg.Mod128 = f.Control.Kind == FrameSABME
			if s.cfg.Mod128 {
				s.cfg.Window = DefaultWindowMod128
			} else {
				s.cfg.Window = DefaultWindowMod8
			}
			s.requeueAllPending()
			s.v.VS, s.v.VR, s.v.VA = 0, 0, 0
			s.v.N2Count = 0
			s.v.Cond = 0
			s.t1.stop()
			s.t2.stop()
			s.t3.reset()
			s.sendUA(f.Control.PF)
			s.setState(StateConnected)
		case FrameDISC:
			if !f.IsCommand {
				break
			}
			s.sendUA(f.Control.PF)
			s.dropQueues()
			s.emit(OutEvent{Kind: OutError, ErrCode: "peer-disconnected",
				ErrMsg: "peer sent DISC"})
			s.setState(StateDisconnected)
			return true
		case FrameDM:
			s.dropQueues()
			s.emit(OutEvent{Kind: OutError, ErrCode: "peer-reset",
				ErrMsg: "DM in TIMER_RECOVERY"})
			s.setState(StateDisconnected)
			return true
		case FrameRR, FrameRNR:
			if f.Control.Kind == FrameRNR {
				s.v.Cond.Set(CondPeerRxBusy)
			} else {
				s.v.Cond.Clear(CondPeerRxBusy)
			}
			if !s.validNR(f.Control.NR) {
				s.establishDataLink()
				return true
			}
			// Final-poll responses (rsp F=1) are the recovery signal.
			if !f.IsCommand && f.Control.PF {
				s.t1.stop()
				s.v.N2Count = 0
				s.framesAcked(f.Control.NR)
				if s.v.VS == s.v.VA {
					s.t3.reset()
					s.setState(StateConnected)
				}
				// vs!=va: stay in TIMER_RECOVERY; kick re-fires T1.
				break
			}
			if f.IsCommand && f.Control.PF {
				s.sendRROrRNR(true /*F=1*/, true /*rsp*/)
			}
			s.framesAcked(f.Control.NR)
		case FrameREJ:
			s.v.Cond.Clear(CondPeerRxBusy)
			if !s.validNR(f.Control.NR) {
				s.establishDataLink()
				return true
			}
			if !f.IsCommand && f.Control.PF {
				s.t1.stop()
				s.v.N2Count = 0
				s.framesAcked(f.Control.NR)
				s.requeueFromVA()
				if s.v.VS == s.v.VA {
					s.t3.reset()
					s.setState(StateConnected)
				}
				break
			}
			if f.IsCommand && f.Control.PF {
				s.sendRROrRNR(true /*F=1*/, true /*rsp*/)
			}
			s.framesAcked(f.Control.NR)
			s.requeueFromVA()
		case FrameI:
			if !s.validNR(f.Control.NR) {
				s.establishDataLink()
				return true
			}
			s.framesAcked(f.Control.NR)
			if s.v.Cond.Has(CondOwnRxBusy) {
				if f.Control.PF {
					s.sendRROrRNR(true, true)
				}
				break
			}
			if f.Control.NS == s.v.VR {
				s.v.VR = (s.v.VR + 1) % uint8(s.modulus())
				s.v.Cond.Clear(CondReject)
				if len(f.Info) > 0 {
					payload := append([]byte(nil), f.Info...)
					s.emit(OutEvent{Kind: OutDataRX, Data: payload})
					s.stats.BytesRX += uint64(len(payload))
				}
				s.stats.FramesRX++
				if f.Control.PF {
					s.v.Cond.Clear(CondACKPending)
					s.sendRROrRNR(true /*F=1*/, true /*rsp*/)
					s.t2.stop()
				} else if !s.v.Cond.Has(CondACKPending) {
					s.v.Cond.Set(CondACKPending)
					s.t2.reset()
				}
			} else {
				if !s.v.Cond.Has(CondReject) {
					s.v.Cond.Set(CondReject)
					s.v.Cond.Clear(CondACKPending)
					s.t2.stop()
					s.sendREJ(f.Control.PF)
				} else if f.Control.PF {
					s.sendRROrRNR(true /*F=1*/, true /*rsp*/)
				}
			}
		case FrameFRMR:
			s.establishDataLink()
			return true
		default:
			s.establishDataLink()
			return true
		}
	case EventDataTX:
		s.txBuf = append(s.txBuf, ev.Data...)
	case EventT1Expiry:
		if s.v.N2Count >= s.cfg.N2 {
			// N2 exhausted: kernel sends DM(F=1, rsp) and disconnects
			// (ax25_std_timer.c:162-165).
			f := &Frame{
				Source: s.cfg.Local, Dest: s.cfg.Peer, Path: s.cfg.Path,
				Control: Control{Kind: FrameDM, PF: true},
			}
			s.submit(f)
			s.dropQueues()
			s.emit(OutEvent{Kind: OutError, ErrCode: "ack-timeout",
				ErrMsg: "no response after N2 enquiries"})
			s.setState(StateDisconnected)
			return true
		}
		s.v.N2Count++
		s.transmitEnquiry()
		s.resetT1()
	case EventT2Expiry:
		if s.v.Cond.Has(CondACKPending) {
			s.v.Cond.Clear(CondACKPending)
			s.sendRROrRNR(false /*F=0*/, true /*rsp*/)
		}
	case EventDisconnect, EventAbort:
		s.dropQueues()
		s.v.N2Count = 0
		s.sendDISC()
		s.t2.stop()
		s.t3.stop()
		s.resetT1()
		s.setState(StateAwaitingRelease)
		return true
	case EventShutdown:
		return false
	}
	s.kick()
	return true
}
