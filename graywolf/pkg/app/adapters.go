package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/metrics"
)

// --- Beacon observer for metrics -----------------------------------------

type beaconObserver struct{ m *metrics.Metrics }

func (o *beaconObserver) OnBeaconSent(t beacon.Type) {
	o.m.BeaconPackets.WithLabelValues(string(t)).Inc()
}

func (o *beaconObserver) OnSmartBeaconRate(channel uint32, interval time.Duration) {
	o.m.SmartBeaconRate.WithLabelValues(strconv.FormatUint(uint64(channel), 10)).Set(interval.Seconds())
}

// OnEncodeError satisfies beacon.ErrorObserver and routes to the
// shared metrics registry. Kept in the adapter package (rather than
// pkg/beacon) so pkg/beacon does not need to import pkg/metrics.
func (o *beaconObserver) OnEncodeError(beaconName string) {
	o.m.BeaconEncodeErrors.WithLabelValues(beaconName).Inc()
}

// OnSubmitError satisfies beacon.ErrorObserver with the same rule.
// reason is one of "queue_full", "timeout", or "other"; the beacon
// scheduler classifies at the call site so this label stays stable
// across governor sentinel changes.
func (o *beaconObserver) OnSubmitError(beaconName string, reason string) {
	o.m.BeaconSubmitErrors.WithLabelValues(beaconName, reason).Inc()
}

// --- Config mapping helpers ----------------------------------------------

// beaconConfigFromStore converts a configstore.Beacon row into a
// beacon.Config suitable for handing to beacon.Scheduler. The two
// structs duplicate several fields because configstore models the
// persistence format (nullable, indexed, audited) while beacon.Config
// models the runtime shape (parsed addresses, durations, typed enums).
// Keeping the mapping explicit here surfaces parse errors per beacon
// without taking out the whole scheduler on a single bad row.
func beaconConfigFromStore(b configstore.Beacon) (beacon.Config, error) {
	src, err := ax25.ParseAddress(b.Callsign)
	if err != nil {
		return beacon.Config{}, fmt.Errorf("parse callsign %q: %w", b.Callsign, err)
	}
	dest, err := ax25.ParseAddress(b.Destination)
	if err != nil {
		return beacon.Config{}, fmt.Errorf("parse destination %q: %w", b.Destination, err)
	}
	var path []ax25.Address
	for _, p := range strings.Split(b.Path, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		a, err := ax25.ParseAddress(p)
		if err != nil {
			return beacon.Config{}, fmt.Errorf("parse path %q: %w", p, err)
		}
		path = append(path, a)
	}

	var commentCmd []string
	if b.CommentCmd != "" {
		argv, err := beacon.SplitArgv(b.CommentCmd)
		if err != nil {
			return beacon.Config{}, fmt.Errorf("split comment_cmd: %w", err)
		}
		commentCmd = argv
	}

	symTable := byte('/')
	if len(b.SymbolTable) > 0 {
		symTable = b.SymbolTable[0]
	}
	symCode := byte('-')
	if len(b.Symbol) > 0 {
		symCode = b.Symbol[0]
	}
	// Overlay (A-Z, 0-9) replaces the alternate-table marker on the air,
	// per APRS101: an alphanumeric byte at the table position signals an
	// alternate-table symbol with that overlay character.
	if len(b.Overlay) > 0 && symTable == '\\' {
		c := b.Overlay[0]
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') {
			symTable = c
		}
	}

	cfg := beacon.Config{
		ID:             b.ID,
		Type:           beacon.Type(b.Type),
		Channel:        b.Channel,
		Source:         src,
		Dest:           dest,
		Path:           path,
		Delay:          time.Duration(b.DelaySeconds) * time.Second,
		Every:          time.Duration(b.EverySeconds) * time.Second,
		Slot:           int(b.SlotSeconds),
		UseGps:         b.UseGps,
		Lat:            b.Latitude,
		Lon:            b.Longitude,
		AltFt:          b.AltFt,
		SymbolTable:    symTable,
		SymbolCode:     symCode,
		Comment:        b.Comment,
		CommentCmd:     commentCmd,
		Compress:       b.Compress,
		Messaging:      b.Messaging,
		ObjectName:     b.ObjectName,
		CustomInfo:     b.CustomInfo,
		PHGPower:       int(b.Power),
		PHGHeightFt:    int(b.Height),
		PHGGainDB:      int(b.Gain),
		PHGDirectivity: int(b.Dir),
		Enabled:        b.Enabled,
	}

	if b.SmartBeacon {
		cfg.SmartBeacon = &beacon.SmartBeaconConfig{
			Enabled:   true,
			FastSpeed: float64(b.SbFastSpeed),
			SlowSpeed: float64(b.SbSlowSpeed),
			FastRate:  time.Duration(b.SbFastRate) * time.Second,
			SlowRate:  time.Duration(b.SbSlowRate) * time.Second,
			TurnAngle: float64(b.SbTurnAngle),
			TurnSlope: float64(b.SbTurnSlope),
			TurnTime:  time.Duration(b.SbMinTurnTime) * time.Second,
		}
	}

	return cfg, nil
}
