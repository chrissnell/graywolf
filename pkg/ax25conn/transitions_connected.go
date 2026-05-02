package ax25conn

import "context"

// onConnected — state 3, "Established". Behavior matches
// ax25_std_state3_machine in net/ax25/ax25_std_in.c:141-259 of Linux
// v6.12. See cheat sheet §1 state 3 for the full table.
//
// Per cheat sheet §10, every dispatch path that ends without exiting
// must call s.kick() so newly available window slots actually drain.
func (s *Session) onConnected(_ context.Context, ev Event) bool {
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
			// Graceful rebind in place: full reset of seq vars, requeue
			// pending I-frames so kick re-sends them under fresh
			// numbers. ax25_std_in.c:146-165.
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
				ErrMsg: "DM in CONNECTED"})
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
			// Cmd P=1 → enquiry response.
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
			if f.IsCommand && f.Control.PF {
				s.sendRROrRNR(true /*F=1*/, true /*rsp*/)
			}
			s.framesAcked(f.Control.NR)
			s.requeueFromVA()
			s.t1.stop()
			s.calcRTT()
			s.t3.reset()
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
				// In-sequence delivery.
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
				// Out-of-sequence I-frame.
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
			// Kernel: collapse to re-establish (§4).
			s.establishDataLink()
			return true
		default:
			// Undecodable / unexpected control byte — same as FRMR.
			s.establishDataLink()
			return true
		}
	case EventDataTX:
		s.txBuf = append(s.txBuf, ev.Data...)
	case EventT1Expiry:
		s.v.N2Count = 1
		s.transmitEnquiry()
		s.resetT1()
		s.setState(StateTimerRecovery)
		return true
	case EventT3Expiry:
		s.v.N2Count = 0
		s.transmitEnquiry()
		s.resetT1()
		s.setState(StateTimerRecovery)
		return true
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

// kick drains s.txBuf into I-frames while window has space and the peer
// is not RNR-busy. Mirrors net/ax25/ax25_out.c:286-324 (ax25_kick).
func (s *Session) kick() {
	if s.state != StateConnected && s.state != StateTimerRecovery {
		return
	}
	if s.v.Cond.Has(CondPeerRxBusy) {
		return
	}
	mod := uint8(s.modulus())
	sent := 0
	for len(s.txBuf) > 0 {
		// Window check: outstanding frames are [VA, VS) modulo.
		outstanding := int((s.v.VS - s.v.VA + mod) % mod)
		if outstanding >= s.cfg.Window {
			break
		}
		end := s.cfg.Paclen
		if end > len(s.txBuf) {
			end = len(s.txBuf)
		}
		payload := append([]byte(nil), s.txBuf[:end]...)
		s.txBuf = s.txBuf[end:]
		ns := s.v.VS
		f := &Frame{
			Source: s.cfg.Local, Dest: s.cfg.Peer, Path: s.cfg.Path,
			Control:   Control{Kind: FrameI, NS: ns, NR: s.v.VR, PF: false},
			PID:       0xF0,
			Info:      payload,
			IsCommand: true,
		}
		s.pending[ns] = f
		s.submit(f)
		s.v.VS = (s.v.VS + 1) % mod
		sent++
	}
	if sent > 0 {
		// Each I-frame piggybacks our latest N(R), so any pending ack
		// is now satisfied (kernel ax25_kick: ack-pending cleared once
		// I-frame goes out — ax25_out.c:319-323).
		if s.v.Cond.Has(CondACKPending) {
			s.v.Cond.Clear(CondACKPending)
			s.t2.stop()
		}
	}
	if s.v.VS != s.v.VA {
		// Outstanding I-frames → T1 must be running, T3 suspended.
		if !s.t1.running() {
			s.resetT1()
		}
		s.t3.stop()
	}
}

// validNR reports whether nr falls in the inclusive range [VA, VS] of
// the modulus. Mirrors ax25_validate_nr (net/ax25/ax25_subr.c:158-181).
func (s *Session) validNR(nr uint8) bool {
	mod := uint8(s.modulus())
	span := (s.v.VS - s.v.VA + mod) % mod
	delta := (nr - s.v.VA + mod) % mod
	return delta <= span
}

// framesAcked frees pending slots [VA, nr-1], advances VA, and updates
// T1/T3 per ax25_check_iframes_acked (net/ax25/ax25_out.c:369-386).
func (s *Session) framesAcked(nr uint8) {
	mod := uint8(s.modulus())
	for s.v.VA != nr {
		s.pending[s.v.VA] = nil
		s.v.VA = (s.v.VA + 1) % mod
	}
	if s.v.VS == s.v.VA {
		s.t1.stop()
		s.calcRTT()
		s.t3.reset()
	} else {
		s.resetT1()
	}
}

// requeueFromVA prepends pending[VA..VS-1] back onto txBuf so kick
// re-sends them with fresh sequence numbers. Used by REJ recovery and
// SABM rebind (cheat sheet §1 state 3 + §5).
func (s *Session) requeueFromVA() {
	mod := uint8(s.modulus())
	var buf []byte
	for i := s.v.VA; i != s.v.VS; i = (i + 1) % mod {
		if f := s.pending[i]; f != nil {
			buf = append(buf, f.Info...)
			s.pending[i] = nil
		}
	}
	s.txBuf = append(buf, s.txBuf...)
	s.v.VS = s.v.VA
}

// requeueAllPending is requeueFromVA with the same semantics, kept as a
// distinct name for the SABM-rebind call site so the intent is clear
// in transition logs.
func (s *Session) requeueAllPending() { s.requeueFromVA() }

// dropQueues clears both pending and txBuf; used on hard
// disconnect/abort paths.
func (s *Session) dropQueues() {
	for i := range s.pending {
		s.pending[i] = nil
	}
	s.txBuf = nil
}

// establishDataLink mirrors ax25_std_establish_data_link
// (ax25_std_subr.c:35-50): zero condition, n2count=0, retransmit SABM,
// restart T1, transition to AWAITING_CONNECTION.
func (s *Session) establishDataLink() {
	s.v.Cond = 0
	s.v.N2Count = 0
	s.t1.stop()
	s.t2.stop()
	s.t3.stop()
	s.sendSABM(s.cfg.Mod128)
	s.resetT1()
	s.setState(StateAwaitingConnection)
}

// transmitEnquiry sends RR or RNR (cmd, P=1) and serves as both the
// T1 and T3 expiry response in CONNECTED/TIMER_RECOVERY. Mirrors
// ax25_std_transmit_enquiry (ax25_std_subr.c:52-63).
func (s *Session) transmitEnquiry() {
	s.sendRROrRNR(true /*P=1*/, false /*cmd*/)
	s.v.Cond.Clear(CondACKPending)
	s.t2.stop()
}

// sendREJ emits REJ(F=p, rsp) with our current N(R).
func (s *Session) sendREJ(pf bool) {
	f := &Frame{
		Source: s.cfg.Local, Dest: s.cfg.Peer, Path: s.cfg.Path,
		Control:   Control{Kind: FrameREJ, NR: s.v.VR, PF: pf},
		IsCommand: false,
	}
	s.submit(f)
}
