package aprs

import "testing"

func TestCountHops(t *testing.T) {
	tests := []struct {
		name string
		path []string
		want int
	}{
		{"direct, no path", nil, 0},
		{"requested but unused", []string{"WIDE1-1", "WIDE2-1"}, 0},
		{"single real digi", []string{"N0CALL*"}, 1},
		{"issue #222 example", []string{"SHEPRD*", "WIDE1*", "ELY*", "WIDE2*"}, 2},
		{"used aliases only", []string{"WIDE1*", "RELAY*"}, 0},
		{"real digi with consumed alias", []string{"N0DIGI*", "WIDE1*"}, 1},
		{"mixed used/unused", []string{"WIDE1-1", "N0CALL*", "WIDE2-1", "N1CALL*", "N2CALL*"}, 3},
		{"trace alias used", []string{"TRACE3-2*", "K7ABC*"}, 1},
		{"q-construct ignored", []string{"qAR*", "K7XYZ*"}, 1},
		{"is-originated TCPIP", []string{"TCPIP*"}, 0},
		{"third-party gated", []string{"TCPIP*", "qAC", "T2USA"}, 0},
		{"tcpxx marker", []string{"TCPXX*", "K7ABC*"}, 1},
	}
	for _, tt := range tests {
		if got := CountHops(tt.path); got != tt.want {
			t.Errorf("%s: CountHops(%v) = %d, want %d", tt.name, tt.path, got, tt.want)
		}
	}
}

func TestIsGenericPathAlias(t *testing.T) {
	aliases := []string{"WIDE1-1", "WIDE2*", "RELAY", "TRACE3-3", "qAR", "qAC*", "TCPIP*", "TCPXX"}
	for _, a := range aliases {
		if !IsGenericPathAlias(a) {
			t.Errorf("IsGenericPathAlias(%q) = false, want true", a)
		}
	}
	reals := []string{"SHEPRD", "ELY*", "K7ABC-1", "N0CALL"}
	for _, r := range reals {
		if IsGenericPathAlias(r) {
			t.Errorf("IsGenericPathAlias(%q) = true, want false", r)
		}
	}
}
