package ingress

import "testing"

func TestModemConstructor(t *testing.T) {
	s := Modem()
	if s.Kind != KindModem {
		t.Fatalf("Modem().Kind = %d, want %d", s.Kind, KindModem)
	}
	if s.ID != 0 {
		t.Fatalf("Modem().ID = %d, want 0 (modem has no interface ID)", s.ID)
	}
}

func TestKissTncConstructor(t *testing.T) {
	cases := []uint32{0, 1, 7, 42, 1 << 31}
	for _, id := range cases {
		s := KissTnc(id)
		if s.Kind != KindKissTnc {
			t.Errorf("KissTnc(%d).Kind = %d, want %d", id, s.Kind, KindKissTnc)
		}
		if s.ID != id {
			t.Errorf("KissTnc(%d).ID = %d, want %d", id, s.ID, id)
		}
	}
}

func TestKindsDistinct(t *testing.T) {
	// Zero-value Kind must not collide with either real value; this
	// catches accidental "forgot to construct via Modem()/KissTnc()".
	var zero Kind
	if zero == KindModem || zero == KindKissTnc {
		t.Fatalf("zero-value Kind %d collides with real Kind constants", zero)
	}
}

func TestIsKissTnc(t *testing.T) {
	tests := []struct {
		name  string
		src   Source
		query uint32
		want  bool
	}{
		{"modem source, any id", Modem(), 1, false},
		{"modem source, zero id", Modem(), 0, false},
		{"kiss-tnc matching id", KissTnc(5), 5, true},
		{"kiss-tnc non-matching id", KissTnc(5), 6, false},
		{"kiss-tnc zero vs zero", KissTnc(0), 0, true},
		{"kiss-tnc zero vs nonzero", KissTnc(0), 1, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.src.IsKissTnc(tc.query); got != tc.want {
				t.Fatalf("IsKissTnc(%d) on %+v = %v, want %v", tc.query, tc.src, got, tc.want)
			}
		})
	}
}
