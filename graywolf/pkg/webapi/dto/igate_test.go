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

	// Booleans and credential fields are intentionally NOT seeded: the
	// UI needs to distinguish unset from explicit-empty, and callsign
	// must come from the operator.
	if got.Enabled {
		t.Error("Enabled should stay zero-valued (false)")
	}
	if got.Callsign != "" {
		t.Errorf("Callsign = %q, want empty", got.Callsign)
	}
	if got.Passcode != "" {
		t.Errorf("Passcode = %q, want empty", got.Passcode)
	}
}

// When the model has user-set values, those win over the defaults.
func TestIGateConfigFromModel_UserValuesWin(t *testing.T) {
	m := configstore.IGateConfig{
		ID:              7,
		Enabled:         true,
		Server:          "noam.aprs2.net",
		Port:            14581,
		Callsign:        "W5XYZ-10",
		Passcode:        "54321",
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
	if got.Callsign != "W5XYZ-10" {
		t.Errorf("Callsign = %q, want W5XYZ-10", got.Callsign)
	}
	if got.RfChannel != 3 {
		t.Errorf("RfChannel = %d, want 3", got.RfChannel)
	}
	if got.SoftwareVersion != "9.9" {
		t.Errorf("SoftwareVersion = %q, want 9.9", got.SoftwareVersion)
	}
}
