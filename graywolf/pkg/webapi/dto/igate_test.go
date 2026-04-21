package dto

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// An empty configstore.IGateConfig (fresh install, no row yet) must
// round-trip through the DTO with UI-ready defaults seeded so the web
// form on /igate renders sensible values instead of blanks.
func TestIGateConfigFromModel_EmptyModelSeedsDefaults(t *testing.T) {
	got := IGateConfigFromModel(configstore.IGateConfig{})

	if got.Server != DefaultIGateServer {
		t.Errorf("Server = %q, want %q", got.Server, DefaultIGateServer)
	}
	if got.Port != DefaultIGatePort {
		t.Errorf("Port = %d, want %d", got.Port, DefaultIGatePort)
	}
	if got.RfChannel != DefaultIGateRfChannel {
		t.Errorf("RfChannel = %d, want %d", got.RfChannel, DefaultIGateRfChannel)
	}
	if got.MaxMsgHops != DefaultIGateMaxMsgHops {
		t.Errorf("MaxMsgHops = %d, want %d", got.MaxMsgHops, DefaultIGateMaxMsgHops)
	}
	if got.SoftwareName != DefaultIGateSoftwareName {
		t.Errorf("SoftwareName = %q, want %q", got.SoftwareName, DefaultIGateSoftwareName)
	}
	if got.SoftwareVersion != DefaultIGateSoftwareVersion {
		t.Errorf("SoftwareVersion = %q, want %q", got.SoftwareVersion, DefaultIGateSoftwareVersion)
	}
	if got.TxChannel != DefaultIGateTxChannel {
		t.Errorf("TxChannel = %d, want %d", got.TxChannel, DefaultIGateTxChannel)
	}

	// Booleans intentionally stay zero-valued: the UI needs to
	// distinguish unset from explicit-empty. Callsign and Passcode
	// fields no longer exist on the DTO — the station callsign lives
	// in StationConfig and the passcode is computed internally.
	if got.Enabled {
		t.Error("Enabled should stay zero-valued (false)")
	}
}

// TestIGateRfFilterRequestValidate covers the wildcard-safety rules
// enforced on POST /api/igate/filters and PUT /api/igate/filters/{id}.
// Both paths route through IGateRfFilterRequest.Validate() via the
// generic handleCreate / handleUpdate helpers in pkg/webapi, so one
// table drives both endpoints.
func TestIGateRfFilterRequestValidate(t *testing.T) {
	const (
		errMissingType  = "type is required"
		errMissingPat   = "pattern is required"
		errBareWildcard = "pattern must not be empty or a bare wildcard"
		errWildcardType = "`*` wildcard is only supported for message_dest and object types"
		errTrailingOnly = "`*` is only supported as a trailing wildcard"
	)

	tests := []struct {
		name    string
		req     IGateRfFilterRequest
		wantErr string // empty = expect nil
	}{
		// --- baseline required-field errors (regression) -----------------
		{
			name:    "missing_type",
			req:     IGateRfFilterRequest{Type: "", Pattern: "NW5W"},
			wantErr: errMissingType,
		},
		{
			name:    "missing_pattern",
			req:     IGateRfFilterRequest{Type: "callsign", Pattern: ""},
			wantErr: errMissingPat,
		},

		// --- accept paths: one per valid rule type, plus wildcard forms --
		{
			name: "accept_callsign_exact",
			req:  IGateRfFilterRequest{Type: "callsign", Pattern: "NW5W-7"},
		},
		{
			name: "accept_prefix_literal",
			req:  IGateRfFilterRequest{Type: "prefix", Pattern: "W5"},
		},
		{
			name: "accept_message_dest_exact",
			req:  IGateRfFilterRequest{Type: "message_dest", Pattern: "BLN1"},
		},
		{
			name: "accept_message_dest_trailing_wildcard",
			req:  IGateRfFilterRequest{Type: "message_dest", Pattern: "NW5W-*"},
		},
		{
			name: "accept_object_exact",
			req:  IGateRfFilterRequest{Type: "object", Pattern: "WX-001"},
		},
		{
			name: "accept_object_trailing_wildcard",
			req:  IGateRfFilterRequest{Type: "object", Pattern: "WX-*"},
		},
		{
			name: "accept_trailing_wildcard_with_surrounding_whitespace",
			req:  IGateRfFilterRequest{Type: "message_dest", Pattern: "  NW5W-*  "},
		},

		// --- bare-wildcard / empty-after-trim (flooding guard) -----------
		{
			name:    "reject_whitespace_only_pattern",
			req:     IGateRfFilterRequest{Type: "message_dest", Pattern: "   "},
			wantErr: errBareWildcard,
		},
		{
			name:    "reject_bare_star",
			req:     IGateRfFilterRequest{Type: "message_dest", Pattern: "*"},
			wantErr: errBareWildcard,
		},
		{
			name:    "reject_padded_bare_star",
			req:     IGateRfFilterRequest{Type: "object", Pattern: " * "},
			wantErr: errBareWildcard,
		},

		// --- wildcard in wrong type --------------------------------------
		{
			name:    "reject_callsign_with_trailing_star",
			req:     IGateRfFilterRequest{Type: "callsign", Pattern: "NW5W*"},
			wantErr: errWildcardType,
		},
		{
			name:    "reject_callsign_with_internal_star",
			req:     IGateRfFilterRequest{Type: "callsign", Pattern: "N*W"},
			wantErr: errWildcardType,
		},
		{
			name:    "reject_prefix_with_trailing_star",
			req:     IGateRfFilterRequest{Type: "prefix", Pattern: "W5*"},
			wantErr: errWildcardType,
		},

		// --- internal / leading `*` on types that accept trailing `*` ----
		{
			name:    "reject_message_dest_leading_star",
			req:     IGateRfFilterRequest{Type: "message_dest", Pattern: "*NW5W"},
			wantErr: errTrailingOnly,
		},
		{
			name:    "reject_message_dest_internal_star",
			req:     IGateRfFilterRequest{Type: "message_dest", Pattern: "NW*W-7"},
			wantErr: errTrailingOnly,
		},
		{
			name:    "reject_object_double_star",
			req:     IGateRfFilterRequest{Type: "object", Pattern: "WX-**"},
			wantErr: errTrailingOnly,
		},
		{
			name:    "reject_object_star_with_trailing_whitespace_internal",
			req:     IGateRfFilterRequest{Type: "object", Pattern: "WX* X"},
			wantErr: errTrailingOnly,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() returned unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error %q", tc.wantErr)
			}
			if err.Error() != tc.wantErr {
				t.Errorf("Validate() error = %q, want %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// When the model has user-set values, those win over the defaults.
func TestIGateConfigFromModel_UserValuesWin(t *testing.T) {
	m := configstore.IGateConfig{
		ID:              7,
		Enabled:         true,
		Server:          "noam.aprs2.net",
		Port:            14581,
		ServerFilter:    "r/35/-106/100",
		RfChannel:       3,
		MaxMsgHops:      4,
		SoftwareName:    "custom",
		SoftwareVersion: "9.9",
		TxChannel:       2,
	}
	got := IGateConfigFromModel(m)

	if got.ID != 7 {
		t.Errorf("ID = %d, want 7", got.ID)
	}
	if got.Server != "noam.aprs2.net" {
		t.Errorf("Server = %q, want noam.aprs2.net", got.Server)
	}
	if got.Port != 14581 {
		t.Errorf("Port = %d, want 14581", got.Port)
	}
	if got.RfChannel != 3 {
		t.Errorf("RfChannel = %d, want 3", got.RfChannel)
	}
	if got.SoftwareVersion != "9.9" {
		t.Errorf("SoftwareVersion = %q, want 9.9", got.SoftwareVersion)
	}
}
