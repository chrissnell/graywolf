package ax25conn

import "context"

// onAwaitingConnection — state 1, "SABM sent". Behavior matches
// ax25_std_state1_machine in net/ax25/ax25_std_in.c:39-96 and
// ax25_std_t1timer_expiry in net/ax25/ax25_std_timer.c:120-141 of
// Linux v6.12.
//
// Kernel deviation from spec: inbound SABM does not transition us to
// state 3 — we emit UA and keep waiting for our own SABM's UA
// (ax25_std_in.c:46-66, "Connection collision").
func (s *Session) onAwaitingConnection(_ context.Context, ev Event) bool {
	switch ev.Kind {
	case EventFrameRX:
		f := ev.Frame
		if f == nil {
			return true
		}
		switch f.Control.Kind {
		case FrameSABM:
			if !f.IsCommand {
				return true
			}
			// Peer sent SABM (collision or simultaneous-open). Kernel:
			// emit UA, set modulus=8, stay in state 1.
			s.cfg.Mod128 = false
			s.cfg.Window = DefaultWindowMod8
			s.sendUA(f.Control.PF)
		case FrameSABME:
			if !f.IsCommand {
				return true
			}
			s.cfg.Mod128 = true
			s.cfg.Window = DefaultWindowMod128
			s.sendUA(f.Control.PF)
		case FrameDISC:
			if !f.IsCommand {
				return true
			}
			s.sendDM(f.Control.PF)
		case FrameUA:
			if !f.Control.PF {
				return true
			}
			s.t1.stop()
			s.calcRTT()
			s.v.N2Count = 0
			s.v.VS, s.v.VR, s.v.VA = 0, 0, 0
			s.t3.reset()
			s.setState(StateConnected)
		case FrameDM:
			if !f.Control.PF {
				return true
			}
			if s.cfg.Mod128 {
				// Kernel ax25_std_in.c:84-87: downgrade to mod-8, keep
				// T1 running, retry as SABM on next T1 expiry.
				s.cfg.Mod128 = false
				s.cfg.Window = DefaultWindowMod8
				return true
			}
			s.t1.stop()
			s.emit(OutEvent{Kind: OutError, ErrCode: "peer-rejected",
				ErrMsg: "DM during link setup"})
			s.setState(StateDisconnected)
		}
	case EventT1Expiry:
		if s.v.N2Count >= s.cfg.N2 {
			if s.cfg.Mod128 {
				// Kernel ax25_std_timer.c:128-133: auto-fall-back to
				// mod-8 with a fresh N2 budget.
				s.cfg.Mod128 = false
				s.cfg.Window = DefaultWindowMod8
				s.v.N2Count = 0
				s.sendSABM(false)
				s.resetT1()
				return true
			}
			s.emit(OutEvent{Kind: OutError, ErrCode: "link-setup-timeout",
				ErrMsg: "no response to SABM after N2 retries"})
			s.setState(StateDisconnected)
			return true
		}
		s.v.N2Count++
		s.sendSABM(s.cfg.Mod128)
		s.resetT1()
	case EventDisconnect, EventAbort:
		// Kernel af_ax25.c:1010-1018 — close in state 1 sends DISC and
		// transitions through ax25_disconnect to state 0. We model the
		// intermediate AWAITING_RELEASE state for symmetry.
		s.sendDISC()
		s.v.N2Count = 0
		s.resetT1()
		s.setState(StateAwaitingRelease)
	case EventShutdown:
		return false
	}
	return true
}
