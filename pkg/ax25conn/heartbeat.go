package ax25conn

// heartbeatTick is the 5-second housekeeping timer per cheat sheet §2
// and net/ax25/ax25_std_timer.c:36-71. The kernel re-arms
// unconditionally; we mirror that. The kernel also has two jobs:
//
//  1. State 0/2 dead-listener cleanup. graywolf has no server side, so
//     this case is unreachable for v1.
//  2. State 3/4: clear CondOwnRxBusy if the local RX buffer drains
//     below half full, and inform the peer with an RR(F=0, rsp). Also
//     clear CondACKPending since the same RR doubles as a fresh ack.
//
// graywolf v1 has no per-session bounded RX buffer; the WebSocket
// bridge consumes incoming I-frame payloads immediately. This means
// the OWN_RX_BUSY case is unreachable in normal operation.
// rxBufferBelowHalfFull therefore always returns true, and the only
// way the body of this function executes is if a future Phase
// (transcript throttling, SDR back-pressure) explicitly sets
// CondOwnRxBusy from outside the heartbeat.
func (s *Session) heartbeatTick() {
	defer s.hb.reset()
	if s.state != StateConnected && s.state != StateTimerRecovery {
		return
	}
	if !s.v.Cond.Has(CondOwnRxBusy) {
		return
	}
	if !s.rxBufferBelowHalfFull() {
		return
	}
	s.v.Cond.Clear(CondOwnRxBusy)
	s.v.Cond.Clear(CondACKPending)
	s.sendRROrRNR(false /*P=0*/, true /*rsp*/)
}

// rxBufferBelowHalfFull returns true when our local RX buffer has
// drained enough to clear OWN_RX_BUSY. graywolf v1 streams to the
// WebSocket immediately and never asserts back-pressure, so this is
// always true. Phase 3+ may replace this with a real check.
func (s *Session) rxBufferBelowHalfFull() bool { return true }
