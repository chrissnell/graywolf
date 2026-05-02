package ax25conn

import (
	"testing"
	"time"
)

func TestDefaultsStable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		got, want time.Duration
	}{
		{"T1", DefaultT1, 10 * time.Second},
		{"T2", DefaultT2, 3 * time.Second},
		{"T3", DefaultT3, 300 * time.Second},
		{"Heartbeat", DefaultHeartbeat, 5 * time.Second},
		{"Idle", DefaultIdle, 0},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if DefaultN2 != 10 || DefaultPaclen != 256 ||
		DefaultWindowMod8 != 2 || DefaultWindowMod128 != 32 {
		t.Fatal("default integer constants drifted; see kernel include/net/ax25.h:148-160")
	}
	if DefaultBackoff != BackoffLinear {
		t.Fatal("DefaultBackoff drifted; see AX25_DEF_BACKOFF=1")
	}
	if RTTClampLo != time.Millisecond || RTTClampHi != 30*time.Second {
		t.Fatal("RTT clamps drifted; see include/net/ax25.h:20-21")
	}
}
