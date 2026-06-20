package dto

import (
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// TestPttRequest_Validate_Android covers the ptt_method gate for the
// android method (spec Appendix B, C1 final-review sweep).
func TestPttRequest_Validate_Android(t *testing.T) {
	// Each valid ptt_method must pass (5 = Digirig Lite tone PTT).
	for _, pin := range []uint32{1, 2, 3, 4, 5} {
		r := PttRequest{ChannelID: 1, Method: "android", PttMethod: pin}
		if err := r.Validate(); err != nil {
			t.Errorf("ptt_method=%d: expected no error, got %v", pin, err)
		}
	}

	// ptt_method 0 must be rejected.
	r0 := PttRequest{ChannelID: 1, Method: "android", PttMethod: 0}
	if err := r0.Validate(); err == nil {
		t.Error("ptt_method=0: expected error, got nil")
	} else if !strings.Contains(err.Error(), "android ptt") {
		t.Errorf("ptt_method=0: error %q does not contain \"android ptt\"", err.Error())
	}

	// ptt_method 99 must be rejected.
	r99 := PttRequest{ChannelID: 1, Method: "android", PttMethod: 99}
	if err := r99.Validate(); err == nil {
		t.Error("ptt_method=99: expected error, got nil")
	} else if !strings.Contains(err.Error(), "android ptt") {
		t.Errorf("ptt_method=99: error %q does not contain \"android ptt\"", err.Error())
	}
}

func TestPttRequest_AndroidRequiresPttMethod(t *testing.T) {
	bad := PttRequest{ChannelID: 1, Method: "android", PttMethod: 0}
	if bad.Validate() == nil {
		t.Fatal("android with ptt_method=0 must be rejected")
	}
	ok := PttRequest{ChannelID: 1, Method: "android", PttMethod: 3}
	if err := ok.Validate(); err != nil {
		t.Fatalf("android ptt_method=3 must be valid, got %v", err)
	}
	if m := ok.ToModel(); m.PttMethod != 3 {
		t.Fatalf("ToModel ptt_method: got %d want 3", m.PttMethod)
	}
}

// TestPttRequest_Validate_NonAndroid confirms the android ptt_method gate
// does not interfere with other methods (they may carry any gpio_pin value).
func TestPttRequest_Validate_NonAndroid(t *testing.T) {
	for _, method := range []string{"serial_rts", "cm108_hid", "gpio", "vox", "digirig_tone", "none"} {
		r := PttRequest{ChannelID: 1, Method: method, GpioPin: 0}
		if err := r.Validate(); err != nil {
			t.Errorf("method=%s gpio_pin=0: unexpected error: %v", method, err)
		}
	}
}

// TestPttFromModel_PttMethod verifies that PttFromModel propagates the
// Android transport (PttMethod) into the PTT-specific API response -- the
// modal restoring an android channel via GET /api/ptt/{channel} (and the
// 201/200 body of POST/PUT /api/ptt) depends on this round-trip.
func TestPttFromModel_PttMethod(t *testing.T) {
	in := configstore.PttConfig{ID: 1, ChannelID: 7, Method: "android", PttMethod: 3}
	got := PttFromModel(in)
	if got.Method != "android" {
		t.Fatalf("method: got %q want android", got.Method)
	}
	if got.PttMethod != 3 {
		t.Fatalf("ptt_method: got %d want 3 (AIOC must round-trip through PttFromModel)", got.PttMethod)
	}
}
