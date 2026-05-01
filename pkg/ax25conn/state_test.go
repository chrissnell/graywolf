package ax25conn

import "testing"

func TestStateString(t *testing.T) {
	cases := []struct {
		s    State
		want string
	}{
		{StateDisconnected, "DISCONNECTED"},
		{StateAwaitingConnection, "AWAITING_CONNECTION"},
		{StateConnected, "CONNECTED"},
		{StateTimerRecovery, "TIMER_RECOVERY"},
		{StateAwaitingRelease, "AWAITING_RELEASE"},
		{State(99), "UNKNOWN"},
	}
	for _, c := range cases {
		if c.s.String() != c.want {
			t.Errorf("%d.String()=%q want %q", c.s, c.s.String(), c.want)
		}
	}
}

func TestConditionBitfield(t *testing.T) {
	var c Condition
	c.Set(CondACKPending)
	c.Set(CondPeerRxBusy)
	if !c.Has(CondACKPending) || !c.Has(CondPeerRxBusy) {
		t.Fatalf("set/has drift: %08b", c)
	}
	if c.Has(CondReject) || c.Has(CondOwnRxBusy) {
		t.Fatalf("unexpected bit: %08b", c)
	}
	c.Clear(CondACKPending)
	if c.Has(CondACKPending) {
		t.Fatalf("clear failed: %08b", c)
	}
	if !c.Has(CondPeerRxBusy) {
		t.Fatalf("clear nuked wrong bit: %08b", c)
	}
	// Zero-wholesale per link-establish semantics.
	c = 0
	if c != 0 || c.Has(CondPeerRxBusy) {
		t.Fatalf("zero failed")
	}
}
