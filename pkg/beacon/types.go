package beacon

import (
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

// Type enumerates the supported beacon kinds.
type Type string

const (
	TypePosition Type = "position"
	TypeObject   Type = "object"
	TypeTracker  Type = "tracker"
	TypeCustom   Type = "custom"
	TypeIGate    Type = "igate"
)

// Config describes one beacon entry from the beacons table. Fields match
// the SQL schema in .context/graywolf-implementation-plan.md §beacons.
type Config struct {
	ID          uint32
	Type        Type
	Channel     uint32 // send_to parsed as channel number (IG/APP handled by caller)
	Source      ax25.Address
	Dest        ax25.Address
	Path        []ax25.Address
	Delay       time.Duration // initial delay
	Every       time.Duration // periodic interval
	Slot        int           // seconds past the hour; -1 means unset
	UseGps      bool          // if true, source lat/lon/alt from the GPS cache instead of Lat/Lon/AltFt
	Lat, Lon    float64       // fixed position
	AltFt       float64
	SymbolTable byte
	SymbolCode  byte
	Comment     string
	CommentCmd  []string // already-split argv; empty = static comment
	Compress    bool     // use 13-byte base-91 compressed position format
	Messaging   bool
	ObjectName  string             // for TypeObject
	CustomInfo  string             // for TypeCustom (raw info field override)
	SmartBeacon *SmartBeaconConfig // non-nil + .Enabled → use for tracker
	// PHG radio-capability extension (APRS101 ch 7) for fixed-station
	// position, igate, and object beacons. Emitted only when PHGPower
	// > 0. Not valid for trackers (CSE/SPD occupies the same slot).
	PHGPower       int // watts
	PHGHeightFt    int // feet above average terrain
	PHGGainDB      int // dBi
	PHGDirectivity int // 0 = omni, 1..8 = 45° × d compass direction
	Enabled      bool
	SendToAPRSIS bool // also send this beacon to APRS-IS (default off)
}

// ISSink is an optional destination for sending beacons to APRS-IS.
// When non-nil and Config.SendToAPRSIS is true, the scheduler sends
// a TNC-2 formatted copy of the beacon to APRS-IS after RF submission.
type ISSink interface {
	SendLine(line string) error
}

// Observer is an optional hook for metrics. Scheduler calls these on
// beacon send; nil methods are skipped.
type Observer interface {
	OnBeaconSent(beaconType Type)
	OnSmartBeaconRate(channel uint32, interval time.Duration)
}

// ErrorObserver is an optional interface the Observer may implement to
// receive beacon failure notifications. The scheduler performs a type
// assertion at call time, so existing Observer implementations (which
// do not know about encode/submit errors) keep working unmodified.
//
// OnEncodeError fires once per beacon whose AX.25 encoding step
// returned a non-nil error — typically a misconfigured source or
// destination address. The beacon is dropped on the floor; the
// scheduler will retry at the next tick but nothing changes in the
// configuration, so a sustained non-zero count is an operator signal.
//
// OnSubmitError fires once per Submit call that returned an error.
// reason is one of "queue_full", "timeout", or "other" — see
// classifySubmitError.
type ErrorObserver interface {
	OnEncodeError(beaconName string)
	OnSubmitError(beaconName string, reason string)
}

// SkipObserver is an optional interface for observers that want to know
// when the scheduler skipped a fire because the worker pool was saturated.
// reason is currently always "busy"; the argument exists so future skip
// causes (e.g. shutdown-in-progress) get their own bucket without a
// signature change.
type SkipObserver interface {
	OnBeaconSkipped(beaconName string, reason string)
}

// Clock abstracts time for deterministic tests.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
