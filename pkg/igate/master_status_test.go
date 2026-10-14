package igate

import "testing"

func TestStatusTracksRFToISMaster(t *testing.T) {
	ig := mustIgate(t)
	for _, enabled := range []bool{true, false, true} {
		ig.SetRFToISEnabled(enabled)
		if ig.Status().RFToISEnabled != enabled {
			t.Fatalf("status does not reflect live switch %v", enabled)
		}
	}
}
