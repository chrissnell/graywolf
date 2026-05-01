package ax25conn

import "time"

// State enumerates the LAPB data-link states (K3NA 1988, AX.25 v2.2 §6).
type State uint8

const (
	StateDisconnected State = iota
	StateAwaitingConnection
	StateConnected
	StateTimerRecovery
	StateAwaitingRelease
)

func (s State) String() string {
	switch s {
	case StateDisconnected:
		return "DISCONNECTED"
	case StateAwaitingConnection:
		return "AWAITING_CONNECTION"
	case StateConnected:
		return "CONNECTED"
	case StateTimerRecovery:
		return "TIMER_RECOVERY"
	case StateAwaitingRelease:
		return "AWAITING_RELEASE"
	}
	return "UNKNOWN"
}

// vars holds per-session sequence and flag state. Mutated only from
// the session goroutine; readers go through atomic snapshot getters
// when used outside that goroutine.
//
// The Cond bitfield mirrors the kernel's `ax25->condition` byte
// (include/net/ax25.h:60-64). The kernel zeroes the whole byte at
// link-establish time (ax25_std_subr.c:37); replicate via
// `s.v.Cond = 0` in establish-data-link, not via per-bit clears.
type vars struct {
	VS, VR, VA uint8         // send / receive / ack sequence numbers
	N2Count    int           // retry count (cmp against cfg.N2)
	Cond       Condition     // bitfield, see below
	RTT        time.Duration // EWMA, seeded at T1/2 on link establish
	T1Started  time.Time     // wall time of last T1 reset; used by RTT calc
}

// Condition is a bitfield mirroring the Linux kernel's ax25->condition
// byte. Zeroed wholesale at link-establish per ax25_std_subr.c:37.
type Condition uint8

const (
	// CondACKPending — we received an in-order I-frame without P=1
	// and have not yet emitted an RR/RNR; T2 will fire if no
	// piggyback opportunity arrives. Kernel AX25_COND_ACK_PENDING.
	CondACKPending Condition = 1 << iota
	// CondReject — we sent REJ for an out-of-order I-frame and are
	// waiting for the missing N(S). Don't send another REJ until
	// CondReject clears. Kernel AX25_COND_REJECT.
	CondReject
	// CondPeerRxBusy — peer sent RNR; suspend our I-frame TX.
	// Kernel AX25_COND_PEER_RX_BUSY.
	CondPeerRxBusy
	// CondOwnRxBusy — our receive buffer is full; we're sending RNR
	// in lieu of RR. Cleared by the heartbeat once buffer drains.
	// Kernel AX25_COND_OWN_RX_BUSY.
	CondOwnRxBusy
)

func (c Condition) Has(b Condition) bool { return c&b != 0 }
func (c *Condition) Set(b Condition)     { *c |= b }
func (c *Condition) Clear(b Condition)   { *c &^= b }
