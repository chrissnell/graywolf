package dto

import (
	"strings"
	"testing"
)

// TestPttRequest_Validate_Android covers the gpio_pin gate added for the
// android method (spec Appendix B, C1 final-review sweep).
func TestPttRequest_Validate_Android(t *testing.T) {
	// Each valid pin must pass.
	for _, pin := range []uint32{1, 2, 3, 4} {
		r := PttRequest{ChannelID: 1, Method: "android", GpioPin: pin}
		if err := r.Validate(); err != nil {
			t.Errorf("gpio_pin=%d: expected no error, got %v", pin, err)
		}
	}

	// gpio_pin 0 must be rejected.
	r0 := PttRequest{ChannelID: 1, Method: "android", GpioPin: 0}
	if err := r0.Validate(); err == nil {
		t.Error("gpio_pin=0: expected error, got nil")
	} else if !strings.Contains(err.Error(), "android ptt") {
		t.Errorf("gpio_pin=0: error %q does not contain \"android ptt\"", err.Error())
	}

	// gpio_pin 99 must be rejected.
	r99 := PttRequest{ChannelID: 1, Method: "android", GpioPin: 99}
	if err := r99.Validate(); err == nil {
		t.Error("gpio_pin=99: expected error, got nil")
	} else if !strings.Contains(err.Error(), "android ptt") {
		t.Errorf("gpio_pin=99: error %q does not contain \"android ptt\"", err.Error())
	}
}

// TestPttRequest_Validate_NonAndroid confirms the android gpio_pin gate
// does not interfere with other methods (they may carry any gpio_pin value).
func TestPttRequest_Validate_NonAndroid(t *testing.T) {
	for _, method := range []string{"serial_rts", "cm108_hid", "gpio", "none"} {
		r := PttRequest{ChannelID: 1, Method: method, GpioPin: 0}
		if err := r.Validate(); err != nil {
			t.Errorf("method=%s gpio_pin=0: unexpected error: %v", method, err)
		}
	}
}
