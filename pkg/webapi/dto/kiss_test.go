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

// TestKissRequest_Validate_BaudRate verifies that a serial/bluetooth
// interface with baud_rate == 0 is rejected, a valid baud_rate is
// accepted, and non-serial types are unaffected by the baud_rate check.
func TestKissRequest_Validate_BaudRate(t *testing.T) {
	tests := []struct {
		name    string
		req     KissRequest
		wantErr string
	}{
		{
			name: "serial with zero baud_rate is rejected",
			req: KissRequest{
				Type: configstore.KissTypeSerial, SerialDevice: "/dev/ttyUSB0", BaudRate: 0,
			},
			wantErr: "baud_rate is required for serial/bluetooth interfaces",
		},
		{
			name: "serial with non-zero baud_rate is accepted",
			req: KissRequest{
				Type: configstore.KissTypeSerial, SerialDevice: "/dev/ttyUSB0", BaudRate: 9600,
			},
		},
		{
			name: "tcp with zero baud_rate is accepted (baud_rate irrelevant for tcp)",
			req: KissRequest{
				Type: configstore.KissTypeTCP, TcpPort: 8001, BaudRate: 0,
			},
		},
		{
			name: "tcp-client with zero baud_rate is accepted (baud_rate irrelevant for tcp-client)",
			req: KissRequest{
				Type: configstore.KissTypeTCPClient, RemoteHost: "tnc.example", RemotePort: 8001, BaudRate: 0,
			},
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

// TestKissRequest_ToModel_TcpClientModeDefault verifies the Phase 4
// contract: a tcp-client with no explicit Mode defaults to a TX-capable
// TNC link (mode=tnc + allow_tx_from_governor), while every other
// interface type keeps the historical modem default and an explicit
// Mode is always preserved as-is. This is the API-boundary half of the
// issue #128 fix; normalizeKissInterface is the store-side backstop.
func TestKissRequest_ToModel_TcpClientModeDefault(t *testing.T) {
	t.Run("tcp-client empty mode defaults to tnc + governor TX", func(t *testing.T) {
		m := KissRequest{Type: "tcp-client", RemoteHost: "tnc.example", RemotePort: 8001}.ToModel()
		if m.Mode != configstore.KissModeTnc {
			t.Errorf("Mode=%q, want %q", m.Mode, configstore.KissModeTnc)
		}
		if !m.AllowTxFromGovernor {
			t.Errorf("AllowTxFromGovernor=false, want true for a defaulted tcp-client")
		}
	})
	t.Run("tcp server empty mode keeps modem default", func(t *testing.T) {
		m := KissRequest{Type: "tcp", TcpPort: 8001}.ToModel()
		if m.Mode != configstore.KissModeModem {
			t.Errorf("Mode=%q, want %q", m.Mode, configstore.KissModeModem)
		}
		if m.AllowTxFromGovernor {
			t.Errorf("AllowTxFromGovernor=true, want false for a modem-default tcp server")
		}
	})
	t.Run("explicit modem mode on tcp-client is preserved", func(t *testing.T) {
		m := KissRequest{
			Type: "tcp-client", RemoteHost: "tnc.example", RemotePort: 8001,
			Mode: configstore.KissModeModem,
		}.ToModel()
		if m.Mode != configstore.KissModeModem {
			t.Errorf("Mode=%q, want explicit %q preserved", m.Mode, configstore.KissModeModem)
		}
		if m.AllowTxFromGovernor {
			t.Errorf("AllowTxFromGovernor=true, want false when caller pinned modem mode")
		}
	})
}

// TestKissRequest_ToUpdate_TcpClientFullResourceReplace pins the
// update-path contract: KISS PUT is full-resource replace (ToUpdate
// feeds the same ToModel that create uses, and Store.UpdateKissInterface
// does db.Save), so a PUT that OMITS mode re-applies the tcp-client TX
// default exactly as create does — it does not merge against the
// persisted row. An explicitly supplied mode is still honored verbatim.
// This is intentional and consistent with every other KISS field
// default (reconnect bounds, ingress rates) on PUT; the only hazardous
// outcome (tnc+governor TX on a modem-backed channel) is independently
// rejected by validateKissInterface, not by this layer.
func TestKissRequest_ToUpdate_TcpClientFullResourceReplace(t *testing.T) {
	t.Run("PUT omitting mode re-applies the tcp-client TX default", func(t *testing.T) {
		m := KissRequest{Type: "tcp-client", RemoteHost: "tnc.example", RemotePort: 8001}.ToUpdate(7)
		if m.ID != 7 {
			t.Errorf("ID=%d, want 7", m.ID)
		}
		if m.Mode != configstore.KissModeTnc || !m.AllowTxFromGovernor {
			t.Errorf("Mode=%q Allow=%v, want tnc+true (default re-applied on update)",
				m.Mode, m.AllowTxFromGovernor)
		}
	})
	t.Run("PUT with explicit modem keeps the interface passive", func(t *testing.T) {
		m := KissRequest{
			Type: "tcp-client", RemoteHost: "tnc.example", RemotePort: 8001,
			Mode: configstore.KissModeModem,
		}.ToUpdate(7)
		if m.Mode != configstore.KissModeModem || m.AllowTxFromGovernor {
			t.Errorf("Mode=%q Allow=%v, want explicit modem preserved (passive)",
				m.Mode, m.AllowTxFromGovernor)
		}
	})
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
