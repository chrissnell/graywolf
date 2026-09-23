package cot

import (
	"time"

	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/configstore"
)

// buildInfo renders a CoT target's APRS object info-field. A CoT is,
// on the wire, nothing more than an object report authored by the
// station -- so this reuses beacon's object-beacon encoder rather than
// duplicating APRS101 ch 6/11 encoding rules. live=false renders the
// APRS101 ch 11 kill status ('_') sent once on delete.
func buildInfo(t configstore.CotTarget, now time.Time, live bool) string {
	symTable := byte('/')
	if len(t.SymbolTable) > 0 {
		symTable = t.SymbolTable[0]
	}
	symCode := byte('D')
	if len(t.Symbol) > 0 {
		symCode = t.Symbol[0]
	}
	// Overlay (A-Z, 0-9) replaces the alternate-table marker on the
	// air, per APRS101, mirroring pkg/app/adapters.go's beacon mapping.
	if len(t.Overlay) > 0 && symTable == '\\' {
		c := t.Overlay[0]
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') {
			symTable = c
		}
	}
	return beacon.ObjectInfo(t.ObjectName, live, beacon.DHMZulu(now), t.Latitude, t.Longitude, 0, symTable, symCode, "", t.Comment)
}
