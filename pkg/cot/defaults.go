package cot

import (
	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/configstore"
)

// DefaultSettings returns the built-in defaults applied when no
// CotSettings row has been saved yet. Single source of truth consumed
// by both the webapi GET fallback and DTO default-population, mirroring
// beacon.DefaultSmartBeacon().
func DefaultSettings() configstore.CotSettings {
	return configstore.CotSettings{
		Type:                 "object",
		SendPath:             beacon.SendPathRF,
		Channel:              0,
		Destination:          "APGRWO",
		Path:                 "WIDE1-1,WIDE2-1",
		NumTransmits:         4,
		SecondTxDelaySeconds: 300,
		DecayFactor:          2,
	}
}
