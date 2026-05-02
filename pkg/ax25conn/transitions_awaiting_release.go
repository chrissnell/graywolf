package ax25conn

import "context"

// onAwaitingRelease — state 2, "DISC sent". Behavior matches
// ax25_std_state2_machine in net/ax25/ax25_std_in.c:103-134 and
// ax25_std_t1timer_expiry in ax25_std_timer.c:143-148 of Linux v6.12.
// Cheat sheet §1 state 2.
func (s *Session) onAwaitingRelease(_ context.Context, ev Event) bool {
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
			// Refuse to reconnect during release.
			s.sendDM(f.Control.PF)
		case FrameDISC:
			if !f.IsCommand {
				break
			}
			// Peer also disconnecting.
			s.sendUA(f.Control.PF)
			s.t1.stop()
			s.setState(StateDisconnected)
			return true
		case FrameUA:
			if !f.Control.PF {
				break
			}
			// Ack of our DISC.
			s.t1.stop()
			s.setState(StateDisconnected)
			return true
		case FrameDM:
			if !f.Control.PF {
				break
			}
			s.t1.stop()
			s.setState(StateDisconnected)
			return true
		case FrameI, FrameRR, FrameRNR, FrameREJ:
			// Discourage further data with DM(F=1, rsp).
			if f.IsCommand && f.Control.PF {
				s.sendDM(true)
			}
			// P=0 silently dropped.
		}
	case EventT1Expiry:
		if s.v.N2Count >= s.cfg.N2 {
			// One last DISC and give up (kernel ax25_std_timer.c:144-148).
			s.sendDISC()
			s.emit(OutEvent{Kind: OutError, ErrCode: "disc-timeout",
				ErrMsg: "no response to DISC after N2 retries"})
			s.setState(StateDisconnected)
			return true
		}
		s.v.N2Count++
		s.sendDISC()
		s.resetT1()
	case EventShutdown:
		return false
	}
	return true
}
