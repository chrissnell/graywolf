package dto

import "testing"

func TestFixedPointRequestValidate(t *testing.T) {
	cases := []struct {
		name    string
		req     FixedPointRequest
		wantErr bool
	}{
		{"ok", FixedPointRequest{Name: "X", Latitude: 1, Longitude: 2}, false},
		{"empty name", FixedPointRequest{Name: "  ", Latitude: 1, Longitude: 2}, true},
		{"lat too high", FixedPointRequest{Name: "X", Latitude: 91, Longitude: 2}, true},
		{"lat too low", FixedPointRequest{Name: "X", Latitude: -91, Longitude: 2}, true},
		{"lon too high", FixedPointRequest{Name: "X", Latitude: 1, Longitude: 181}, true},
		{"lon too low", FixedPointRequest{Name: "X", Latitude: 1, Longitude: -181}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.req.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() err=%v, wantErr=%v", err, c.wantErr)
			}
		})
	}
}

func TestFixedPointToModelAndBack(t *testing.T) {
	req := FixedPointRequest{Name: "Aid 3", SymbolTable: "/", Symbol: "a", Overlay: "", Latitude: 37.5, Longitude: -122.0}
	m := req.ToModel()
	if m.Name != "Aid 3" || m.Latitude != 37.5 || m.SymbolTable != "/" {
		t.Fatalf("ToModel mismatch: %+v", m)
	}
	m.ID = 7
	resp := FixedPointFromModel(m)
	if resp.ID != 7 || resp.Name != "Aid 3" || resp.Longitude != -122.0 {
		t.Fatalf("FromModel mismatch: %+v", resp)
	}
}
