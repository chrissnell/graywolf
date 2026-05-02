package ax25conn

import "time"

// Default tuning constants. Values inherited from the Linux kernel
// net/ax25/ defines (AX25_DEF_T1, AX25_DEF_T2, AX25_DEF_T3,
// AX25_DEF_N2, AX25_DEF_PACLEN, AX25_DEF_WINDOW, AX25_DEF_EWINDOW,
// AX25_DEF_BACKOFF, AX25_DEF_IDLE) at include/net/ax25.h:148-160 in
// v6.12. Operators override per-session via the saved
// AX25SessionProfile or the connect-time advanced panel.
const (
	// DefaultT1 — outstanding-I-frame ack timer. Kernel AX25_DEF_T1=10s.
	DefaultT1 = 10 * time.Second
	// DefaultT2 — response delay / piggyback timer. Kernel AX25_DEF_T2=3s.
	DefaultT2 = 3 * time.Second
	// DefaultT3 — link-inactivity probe. Kernel AX25_DEF_T3=300s (5min).
	DefaultT3 = 300 * time.Second
	// DefaultIdle — optional auto-disconnect. Kernel AX25_DEF_IDLE=0
	// (disabled). Graywolf v1 inherits the disabled default; advanced
	// panel may surface as a future feature.
	DefaultIdle time.Duration = 0
	// DefaultHeartbeat — housekeeping cadence (clears OWN_RX_BUSY,
	// emits RR(F=0,rsp) when own buffer drains). Kernel hard-codes 5s
	// at ax25_timer.c:50. Not user-tunable per kernel; matched here.
	DefaultHeartbeat = 5 * time.Second
	// DefaultN2 — retry count before giving up. Kernel AX25_DEF_N2=10.
	DefaultN2 = 10
	// DefaultPaclen — max I-field byte count. Kernel AX25_DEF_PACLEN=256.
	DefaultPaclen = 256
	// DefaultWindowMod8 — k for modulo-8. Kernel AX25_DEF_WINDOW=2.
	// Note: K3NA and v2.2 spec examples often quote 4; the kernel ships
	// 2 because it's the safest "default polite" window for shared
	// channels. We match the kernel.
	DefaultWindowMod8 = 2
	// DefaultWindowMod128 — k for modulo-128. Kernel AX25_DEF_EWINDOW=32.
	DefaultWindowMod128 = 32
	// DefaultStatsTick — cadence at which an active session emits
	// OutLinkStats so the UI telemetry panel can sparkline RTT and seq
	// numbers. Fires only while CONNECTED; never blocks.
	DefaultStatsTick = time.Second
)

// Backoff selects the T1-on-retry growth strategy. Kernel default is
// linear backoff (AX25_DEF_BACKOFF=1, kernel constant naming differs).
// Value 0 is reserved for "unset" so applyDefaults can promote it to
// DefaultBackoff without mistakenly overwriting an explicit BackoffNone.
type Backoff uint8

const (
	backoffUnset       Backoff = 0
	BackoffNone        Backoff = 1 // T1 = 2 * RTT regardless of retries
	BackoffLinear      Backoff = 2 // T1 = (2 + 2*n2count) * RTT
	BackoffExponential Backoff = 3 // T1 = (2 << n2count) * RTT, capped at 8 * RTT
)

const DefaultBackoff = BackoffLinear

// RTT clamps inherited from include/net/ax25.h:20-21:
//
//	AX25_T1CLAMPLO = 1 jiffy (treat as 1ms in Go)
//	AX25_T1CLAMPHI = 30 * HZ (30 seconds)
const (
	RTTClampLo = 1 * time.Millisecond
	RTTClampHi = 30 * time.Second
)
