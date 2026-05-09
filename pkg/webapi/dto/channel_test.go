package dto

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// TestChannelRequestValidate_Matrix spans the rules introduced by
// Phase 2 (nullable InputDeviceID, D3 exclusivity deferred to Phase 3).
// Kept as a table so new rules slot in without reshuffling existing
// cases.
func TestChannelRequestValidate_Matrix(t *testing.T) {
	u := configstore.U32Ptr
	cases := []struct {
		name    string
		req     ChannelRequest
		wantErr string // "" means no error; otherwise substring match
	}{
		{
			name:    "modem-backed: valid",
			req:     ChannelRequest{Name: "vhf", InputDeviceID: u(1), ModemType: "afsk"},
			wantErr: "",
		},
		{
			name:    "modem-backed: with output device, valid",
			req:     ChannelRequest{Name: "vhf", InputDeviceID: u(1), OutputDeviceID: 2, ModemType: "afsk"},
			wantErr: "",
		},
		{
			name:    "kiss-only: nil input, no output — valid",
			req:     ChannelRequest{Name: "kiss", InputDeviceID: nil, ModemType: "afsk"},
			wantErr: "",
		},
		{
			name:    "kiss-only: nil input + non-zero output — rejected",
			req:     ChannelRequest{Name: "kiss", InputDeviceID: nil, OutputDeviceID: 4, ModemType: "afsk"},
			wantErr: "output_device_id must be 0",
		},
		{
			name:    "missing name rejected",
			req:     ChannelRequest{Name: "", InputDeviceID: u(1), ModemType: "afsk"},
			wantErr: "name is required",
		},
		{
			name:    "missing modem_type rejected",
			req:     ChannelRequest{Name: "x", InputDeviceID: u(1), ModemType: ""},
			wantErr: "modem_type is required",
		},
		{
			name:    "explicit input_device_id=0 rejected",
			req:     ChannelRequest{Name: "x", InputDeviceID: u(0), ModemType: "afsk"},
			wantErr: "must be null or a valid device id",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.req.Validate()
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case c.wantErr != "" && err == nil:
				t.Fatalf("expected error containing %q, got nil", c.wantErr)
			case c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr):
				t.Fatalf("error %q missing substring %q", err.Error(), c.wantErr)
			}
		})
	}
}

// TestChannelRequest_JSONRoundTrip_Nullable verifies that JSON null
// in input_device_id decodes to a nil pointer (not 0) and that a
// non-null value round-trips identically.
func TestChannelRequest_JSONRoundTrip_Nullable(t *testing.T) {
	t.Run("null decodes to nil", func(t *testing.T) {
		var req ChannelRequest
		if err := json.Unmarshal([]byte(`{"name":"k","input_device_id":null,"modem_type":"afsk"}`), &req); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if req.InputDeviceID != nil {
			t.Fatalf("expected nil InputDeviceID, got %v", req.InputDeviceID)
		}
	})
	t.Run("omitted decodes to nil", func(t *testing.T) {
		var req ChannelRequest
		if err := json.Unmarshal([]byte(`{"name":"k","modem_type":"afsk"}`), &req); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if req.InputDeviceID != nil {
			t.Fatalf("expected nil InputDeviceID, got %v", req.InputDeviceID)
		}
	})
	t.Run("value decodes to pointer", func(t *testing.T) {
		var req ChannelRequest
		if err := json.Unmarshal([]byte(`{"name":"v","input_device_id":7,"modem_type":"afsk"}`), &req); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if req.InputDeviceID == nil || *req.InputDeviceID != 7 {
			t.Fatalf("expected *7, got %v", req.InputDeviceID)
		}
	})
	t.Run("nil encodes to null", func(t *testing.T) {
		req := ChannelRequest{Name: "k", ModemType: "afsk"}
		buf, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(buf), `"input_device_id":null`) {
			t.Fatalf("expected input_device_id:null in %s", string(buf))
		}
	})
}

// TestChannelFromModel_NilInput asserts that a nil InputDeviceID on
// the storage model is preserved through the DTO mapping.
func TestChannelFromModel_NilInput(t *testing.T) {
	m := configstore.Channel{ID: 11, Name: "kiss", InputDeviceID: nil, ModemType: "afsk"}
	resp := ChannelFromModel(m)
	if resp.InputDeviceID != nil {
		t.Fatalf("expected nil, got %v", resp.InputDeviceID)
	}
}

// TestChannelRequestToModel_NilInput asserts the same invariant in
// reverse: a nil DTO InputDeviceID maps to a nil model pointer.
func TestChannelRequestToModel_NilInput(t *testing.T) {
	req := ChannelRequest{Name: "kiss", InputDeviceID: nil, ModemType: "afsk"}
	m := req.ToModel()
	if m.InputDeviceID != nil {
		t.Fatalf("expected nil, got %v", m.InputDeviceID)
	}
}

// TestChannelPttFromModel covers the four detail-rendering branches of
// the PTT summary used by the Channels page (issue #112). The Detail
// strings are wire format consumed by web/src/lib/channelPtt.js — keep
// them in lockstep with that file.
func TestChannelPttFromModel(t *testing.T) {
	tests := []struct {
		name       string
		in         configstore.PttConfig
		method     string
		configured bool
		detail     string
	}{
		{
			name:       "none method maps to !configured",
			in:         configstore.PttConfig{Method: "none"},
			method:     "none",
			configured: false,
			detail:     "",
		},
		{
			name:       "empty method coerces to none",
			in:         configstore.PttConfig{},
			method:     "none",
			configured: false,
			detail:     "",
		},
		{
			name:       "cm108 surfaces gpio pin",
			in:         configstore.PttConfig{Method: "cm108", GpioPin: 3, Device: "/dev/hidraw0"},
			method:     "cm108",
			configured: true,
			detail:     "GPIO 3 · /dev/hidraw0",
		},
		{
			name:       "cm108 zero pin defaults to 3",
			in:         configstore.PttConfig{Method: "cm108"},
			method:     "cm108",
			configured: true,
			detail:     "GPIO 3",
		},
		{
			name:       "unknown method renders without trailing separator",
			in:         configstore.PttConfig{Method: "futureproof"},
			method:     "futureproof",
			configured: true,
			detail:     "",
		},
		{
			name:       "gpio surfaces line offset",
			in:         configstore.PttConfig{Method: "gpio", GpioLine: 17, Device: "/dev/gpiochip0"},
			method:     "gpio",
			configured: true,
			detail:     "line 17 · /dev/gpiochip0",
		},
		{
			name:       "serial_rts surfaces device path",
			in:         configstore.PttConfig{Method: "serial_rts", Device: "/dev/ttyUSB0"},
			method:     "serial_rts",
			configured: true,
			detail:     "/dev/ttyUSB0",
		},
		{
			name:       "rigctld surfaces host:port",
			in:         configstore.PttConfig{Method: "rigctld", Device: "127.0.0.1:4532"},
			method:     "rigctld",
			configured: true,
			detail:     "127.0.0.1:4532",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ChannelPttFromModel(tc.in)
			if got.Method != tc.method {
				t.Errorf("Method = %q, want %q", got.Method, tc.method)
			}
			if got.Configured != tc.configured {
				t.Errorf("Configured = %v, want %v", got.Configured, tc.configured)
			}
			if got.Detail != tc.detail {
				t.Errorf("Detail = %q, want %q", got.Detail, tc.detail)
			}
		})
	}
}
