package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/agw"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/metrics"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// --- Sink adapters --------------------------------------------------------
//
// Each of the three sink adapters bridges a source-specific TxSink
// interface (kiss.TxSink, agw.TxSink, beacon.TxSink) to txgovernor's
// single Submit method. They are trivial field-renaming shims that
// exist because Go requires concrete interface satisfaction rather
// than duck typing; inlining them into wiring.go would bury the
// governor call behind anonymous closures and hurt readability.

type kissSinkAdapter struct{ gov *txgovernor.Governor }

func (a *kissSinkAdapter) Submit(ctx context.Context, channel uint32, f *ax25.Frame, s kiss.SubmitSource) error {
	return a.gov.Submit(ctx, channel, f, txgovernor.SubmitSource{
		Kind: s.Kind, Detail: s.Detail, Priority: s.Priority,
	})
}

type agwSinkAdapter struct{ gov *txgovernor.Governor }

func (a *agwSinkAdapter) Submit(ctx context.Context, channel uint32, f *ax25.Frame, s agw.SubmitSource) error {
	return a.gov.Submit(ctx, channel, f, txgovernor.SubmitSource{
		Kind: s.Kind, Detail: s.Detail, Priority: s.Priority,
	})
}

type beaconSinkAdapter struct{ gov *txgovernor.Governor }

func (a *beaconSinkAdapter) Submit(ctx context.Context, channel uint32, f *ax25.Frame, s beacon.SubmitSource) error {
	return a.gov.Submit(ctx, channel, f, txgovernor.SubmitSource{
		Kind: s.Kind, Detail: s.Detail, Priority: s.Priority,
	})
}

// --- Beacon observer for metrics -----------------------------------------

type beaconObserver struct{ m *metrics.Metrics }

func (o *beaconObserver) OnBeaconSent(t beacon.Type) {
	o.m.BeaconPackets.WithLabelValues(string(t)).Inc()
}

func (o *beaconObserver) OnSmartBeaconRate(channel uint32, interval time.Duration) {
	o.m.SmartBeaconRate.WithLabelValues(strconv.FormatUint(uint64(channel), 10)).Set(interval.Seconds())
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
