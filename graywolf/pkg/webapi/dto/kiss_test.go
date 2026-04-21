package dto

import (
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

func TestKissRequest_Validate_AllowTxFromGovernor(t *testing.T) {
	tests := []struct {
		name    string
		req     KissRequest
		wantErr string
	}{
		{
			name: "allow_tx true with tnc mode is accepted",
			req: KissRequest{
				Type: "tcp", TcpPort: 8001,
				Mode: configstore.KissModeTnc, AllowTxFromGovernor: true,
			},
		},
		{
			name: "allow_tx false with modem mode is accepted",
			req: KissRequest{
				Type: "tcp", TcpPort: 8001,
				Mode: configstore.KissModeModem, AllowTxFromGovernor: false,
			},
		},
		{
			name: "allow_tx true with modem mode is rejected",
			req: KissRequest{
				Type: "tcp", TcpPort: 8001,
				Mode: configstore.KissModeModem, AllowTxFromGovernor: true,
			},
			wantErr: "allow_tx_from_governor requires mode",
		},
		{
			name: "allow_tx true with empty mode is rejected",
			req: KissRequest{
				Type: "tcp", TcpPort: 8001,
				Mode: "", AllowTxFromGovernor: true,
			},
			wantErr: "allow_tx_from_governor requires mode",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v, want contains %q", err, tc.wantErr)
			}
		})
	}
}

func TestKissRequest_ToModel_AllowTxFromGovernor(t *testing.T) {
	req := KissRequest{
		Type:                "tcp",
		TcpPort:             8001,
		Mode:                configstore.KissModeTnc,
		AllowTxFromGovernor: true,
	}
	m := req.ToModel()
	if !m.AllowTxFromGovernor {
		t.Errorf("AllowTxFromGovernor=false, want true")
	}
}

func TestKissFromModel_AllowTxFromGovernor_Roundtrip(t *testing.T) {
	m := configstore.KissInterface{
		InterfaceType:       "tcp",
		ListenAddr:          "0.0.0.0:8001",
		Mode:                configstore.KissModeTnc,
		AllowTxFromGovernor: true,
		NeedsReconfig:       true,
	}
	resp := KissFromModel(m)
	if !resp.AllowTxFromGovernor {
		t.Errorf("response AllowTxFromGovernor=false, want true")
	}
	if !resp.NeedsReconfig {
		t.Errorf("response NeedsReconfig=false, want true")
	}
}

// TestKissRequest_Validate_TcpClient exercises the tcp-client branch
// of the validator: RemoteHost + RemotePort are required, reconnect
// bounds are sanity-checked, and init <= max is enforced.
func TestKissRequest_Validate_TcpClient(t *testing.T) {
	tests := []struct {
		name    string
		req     KissRequest
		wantErr string
	}{
		{
			name: "valid tcp-client",
			req: KissRequest{
				Type:            "tcp-client",
				RemoteHost:      "lora.example.com",
				RemotePort:      8001,
				Channel:         11,
				ReconnectInitMs: 1000,
				ReconnectMaxMs:  300000,
			},
		},
		{
			name: "valid tcp-client with zero reconnect bounds (defaults applied)",
			req: KissRequest{
				Type:       "tcp-client",
				RemoteHost: "lora.example.com",
				RemotePort: 8001,
			},
		},
		{
			name: "missing remote host",
			req: KissRequest{
				Type:       "tcp-client",
				RemotePort: 8001,
			},
			wantErr: "remote_host is required",
		},
		{
			name: "missing remote port",
			req: KissRequest{
				Type:       "tcp-client",
				RemoteHost: "lora.example.com",
			},
			wantErr: "remote_port is required",
		},
		{
			name: "reconnect_init_ms below minimum",
			req: KissRequest{
				Type:            "tcp-client",
				RemoteHost:      "host",
				RemotePort:      9,
				ReconnectInitMs: 100,
			},
			wantErr: "reconnect_init_ms",
		},
		{
			name: "reconnect_max_ms above maximum",
			req: KissRequest{
				Type:           "tcp-client",
				RemoteHost:     "host",
				RemotePort:     9,
				ReconnectMaxMs: 7_000_000,
			},
			wantErr: "reconnect_max_ms",
		},
		{
			name: "init > max",
			req: KissRequest{
				Type:            "tcp-client",
				RemoteHost:      "host",
				RemotePort:      9,
				ReconnectInitMs: 100000,
				ReconnectMaxMs:  1000,
			},
			wantErr: "must be <=",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v, want contains %q", err, tc.wantErr)
			}
		})
	}
}

// TestKissRequest_ToModel_TcpClient verifies the model mapping fills
// in the tcp-client fields and constructs a sensible Name from the
// remote host/port pair.
func TestKissRequest_ToModel_TcpClient(t *testing.T) {
	req := KissRequest{
		Type:            "tcp-client",
		RemoteHost:      "lora.example.com",
		RemotePort:      8001,
		Channel:         11,
		Mode:            configstore.KissModeTnc,
		ReconnectInitMs: 2000,
		ReconnectMaxMs:  60000,
	}
	m := req.ToModel()
	if m.InterfaceType != "tcp-client" {
		t.Errorf("InterfaceType=%q, want tcp-client", m.InterfaceType)
	}
	if m.RemoteHost != "lora.example.com" || m.RemotePort != 8001 {
		t.Errorf("remote=%s:%d, want lora.example.com:8001", m.RemoteHost, m.RemotePort)
	}
	if m.ReconnectInitMs != 2000 || m.ReconnectMaxMs != 60000 {
		t.Errorf("reconnect=%d..%dms, want 2000..60000", m.ReconnectInitMs, m.ReconnectMaxMs)
	}
	if m.ListenAddr != "" {
		t.Errorf("ListenAddr should be empty for tcp-client, got %q", m.ListenAddr)
	}
	if !strings.Contains(m.Name, "lora.example.com") {
		t.Errorf("Name=%q, expected to contain remote host", m.Name)
	}
}

// TestKissFromModel_TcpClient_Roundtrip ensures response mapping
// includes the new fields.
func TestKissFromModel_TcpClient_Roundtrip(t *testing.T) {
	m := configstore.KissInterface{
		InterfaceType:   "tcp-client",
		RemoteHost:      "host.example",
		RemotePort:      1234,
		ReconnectInitMs: 500,
		ReconnectMaxMs:  60000,
	}
	resp := KissFromModel(m)
	if resp.RemoteHost != "host.example" || resp.RemotePort != 1234 {
		t.Errorf("response=%s:%d, want host.example:1234", resp.RemoteHost, resp.RemotePort)
	}
	if resp.ReconnectInitMs != 500 || resp.ReconnectMaxMs != 60000 {
		t.Errorf("reconnect in response=%d..%d", resp.ReconnectInitMs, resp.ReconnectMaxMs)
	}
}
