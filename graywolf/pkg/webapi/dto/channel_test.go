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
