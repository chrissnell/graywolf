package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/callsign"
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

// OnBeaconSkipped satisfies beacon.SkipObserver. The scheduler calls
// this whenever its bounded fire worker pool is saturated and a fire is
// dropped rather than queued, so we bump graywolf_beacon_fired_total
// with the result label "skipped_<reason>" (currently always
// "skipped_busy", but the reason is passed through to leave room for
// future skip causes without a signature change).
func (o *beaconObserver) OnBeaconSkipped(beaconName string, reason string) {
	o.m.BeaconFired.WithLabelValues(beaconName, "skipped_"+reason).Inc()
}

// --- Config mapping helpers ----------------------------------------------

// beaconConfigFromStore converts a configstore.Beacon row into a
// beacon.Config suitable for handing to beacon.Scheduler. The two
// structs duplicate several fields because configstore models the
// persistence format (nullable, indexed, audited) while beacon.Config
// models the runtime shape (parsed addresses, durations, typed enums).
// Keeping the mapping explicit here surfaces parse errors per beacon
// without taking out the whole scheduler on a single bad row.
//
// Callsign resolution: b.Callsign is the per-beacon override (empty
// string means "inherit the station callsign"). stationCall is the
// resolved station callsign looked up by the caller (typically via
// store.ResolveStationCallsign). callsign.Resolve combines the two and
// returns an error for empty/N0CALL — per D6 the caller skips this
// beacon and keeps scheduling the others, so one bad override does not
// kill the beacon pipeline.
//
// SmartBeacon precedence rule: the returned cfg.SmartBeacon is non-nil
// only when BOTH b.SmartBeacon == true AND smart != nil && smart.Enabled
// == true. Either being false means cfg.SmartBeacon = nil and this
// beacon falls back to its fixed Every interval. This matches direwolf's
// semantics — the SMARTBEACON directive is all-or-nothing, so turning
// the global config off makes every per-beacon smart_beacon toggle a
// no-op. The per-beacon Sb* fields on configstore.Beacon are no longer
// consulted; the SmartBeacon curve is a global singleton
// (configstore.SmartBeaconConfig). See
// .context/2026-04-18-smart-beacon-implementation.md.
func beaconConfigFromStore(b configstore.Beacon, smart *configstore.SmartBeaconConfig, stationCall string) (beacon.Config, error) {
	resolved, err := callsign.Resolve(b.Callsign, stationCall)
	if err != nil {
		return beacon.Config{}, fmt.Errorf("resolve callsign (override %q, station %q): %w", b.Callsign, stationCall, err)
	}
	src, err := ax25.ParseAddress(resolved)
	if err != nil {
		return beacon.Config{}, fmt.Errorf("parse callsign %q: %w", resolved, err)
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
		SendToAPRSIS:   b.SendToAPRSIS,
		Enabled:        b.Enabled,
	}

	if b.SmartBeacon && smart != nil && smart.Enabled {
		cfg.SmartBeacon = &beacon.SmartBeaconConfig{
			Enabled:   true,
			FastSpeed: float64(smart.FastSpeedKt),
			SlowSpeed: float64(smart.SlowSpeedKt),
			FastRate:  time.Duration(smart.FastRateSec) * time.Second,
			SlowRate:  time.Duration(smart.SlowRateSec) * time.Second,
			TurnAngle: float64(smart.MinTurnDeg),
			TurnSlope: float64(smart.TurnSlope),
			TurnTime:  time.Duration(smart.MinTurnSec) * time.Second,
		}
	}

	return cfg, nil
}
