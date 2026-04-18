package dto

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// An empty configstore.AgwConfig (fresh install) must round-trip with
// UI-ready defaults so /agw renders a usable form on first load.
func TestAgwFromModel_EmptyModelSeedsDefaults(t *testing.T) {
	got := AgwFromModel(configstore.AgwConfig{})

	if got.ListenAddr != DefaultAgwListenAddr {
		t.Errorf("ListenAddr = %q, want %q", got.ListenAddr, DefaultAgwListenAddr)
	}
	if got.Callsigns != DefaultAgwCallsigns {
		t.Errorf("Callsigns = %q, want %q", got.Callsigns, DefaultAgwCallsigns)
	}
	if got.Enabled {
		t.Error("Enabled should stay zero-valued (false)")
	}
}

func TestAgwFromModel_UserValuesWin(t *testing.T) {
	m := configstore.AgwConfig{
		ID:         3,
		ListenAddr: "127.0.0.1:9000",
		Callsigns:  "W5ABC,W5DEF",
		Enabled:    true,
	}
	got := AgwFromModel(m)

	if got.ID != 3 {
		t.Errorf("ID = %d, want 3", got.ID)
	}
	if got.ListenAddr != "127.0.0.1:9000" {
		t.Errorf("ListenAddr = %q, want 127.0.0.1:9000", got.ListenAddr)
	}
	if got.Callsigns != "W5ABC,W5DEF" {
		t.Errorf("Callsigns = %q, want W5ABC,W5DEF", got.Callsigns)
	}
	if !got.Enabled {
		t.Error("Enabled = false, want true")
	}
}
